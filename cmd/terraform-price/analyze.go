package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/refres"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
	"github.com/zclconf/go-cty/cty"
)

// analyze parses the Terraform directory and prices every resource,
// producing the cost items for the report. The region comes from the
// directory's own tfvars (aws_region, default ap-northeast-2).
func analyze(ctx context.Context, pricer provider.Pricer, dir string) ([]output.CostItem, error) {
	res := resolver.NewResolver(dir)
	region, _ := res.VarString("aws_region")
	if region == "" {
		region = "ap-northeast-2"
	}
	resources, err := parser.ParseDir(dir)
	if err != nil {
		return nil, err
	}
	idx := make(map[string]*parser.Resource, len(resources))
	for _, r := range resources {
		idx[r.Type+"."+r.Name] = r
	}

	// Resolve cross-resource references and inject into the resolver.
	// Iterate: resource refs feed locals, locals feed resource attrs —
	// loop both until stable so locals like
	//   launch_template_block = { id = one(aws_launch_template.x[*].id) }
	// and attrs referencing them both converge.
	rr := refres.New(resources, res)
	if err := rr.Verify(); err != nil {
		return nil, fmt.Errorf("reference cycle: %w", err)
	}
	res.SetResources(rr.AllResolved())
	for res.RetryLocals() {
		rr.Reset()
		res.SetResources(rr.AllResolved())
	}

	var items []output.CostItem
	for _, r := range resources {
		addr := r.Type + "." + r.Name
		kind, spec, note := mapper.MapResource(r, res, idx, region)
		if kind == mapper.KindVariable {
			items = append(items, variableItem(ctx, pricer, addr, r.Type, note, spec))
			continue
		}
		if kind != mapper.KindFixed {
			items = append(items, classifyItem(kind, addr, r.Type, note))
			continue
		}
		if spec == nil {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: note})
			continue
		}
		n, metaNote := metaCount(r, res)
		if n == 0 {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: "count = 0 — resource not created"})
			continue
		}
		spec.Count *= n
		if n > 1 {
			spec.Label = fmt.Sprintf("%s × %d", spec.Label, n)
		}
		if metaNote != "" {
			spec.Label += " (" + metaNote + ")"
		}
		p, unit, err := pricer.UnitPrice(ctx, provider.Query{Service: spec.ServiceCode, Region: spec.Region, Filters: spec.Filters, PreferUnit: spec.PreferUnit})
		if err != nil {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: "price lookup failed: " + err.Error()})
			continue
		}
		items = append(items, output.CostItem{
			Kind: output.Fixed, Addr: addr, Type: r.Type, Spec: spec.Label,
			UnitPrice: p, Unit: unit, Monthly: p * spec.UsageQty * float64(spec.Count),
		})
	}
	return items, nil
}

func variableItem(ctx context.Context, pricer provider.Pricer, addr, typ, note string, spec *mapper.Spec) output.CostItem {
	item := output.CostItem{Kind: output.Variable, Addr: addr, Type: typ, Note: note}
	if spec != nil && spec.Note != "" {
		item.Note = spec.Note
	}
	if spec == nil {
		return item
	}
	var lastErr error
	for _, rt := range spec.Rates {
		p, unit, err := pricer.UnitPrice(ctx, provider.Query{Service: rt.ServiceCode, Region: rt.Region, Filters: rt.Filters, PreferUnit: rt.PreferUnit})
		if err != nil {
			lastErr = err
			continue
		}
		if rt.DisplayMult > 0 {
			p *= rt.DisplayMult
		}
		if rt.DisplayUnit != "" {
			unit = rt.DisplayUnit
		}
		item.Rates = append(item.Rates, output.RateLine{Label: rt.Label, UnitPrice: p, Unit: unit})
	}
	if lastErr != nil && len(item.Rates) == 0 {
		if item.Note != "" {
			item.Note += " — "
		}
		item.Note += "price lookup failed: " + lastErr.Error()
	}
	return item
}

func classifyItem(kind mapper.Kind, addr, typ, note string) output.CostItem {
	switch kind {
	case mapper.KindVariable:
		return output.CostItem{Kind: output.Variable, Addr: addr, Type: typ, Note: note}
	case mapper.KindFree:
		return output.CostItem{Kind: output.Free, Addr: addr, Type: typ, Note: note}
	default:
		return output.CostItem{Kind: output.Unsupported, Addr: addr, Type: typ, Note: note}
	}
}

func metaCount(r *parser.Resource, res *resolver.Resolver) (int, string) {
	if expr, ok := r.Exprs["count"]; ok {
		if v, ok := res.ResolveExpr(expr); ok && v.IsKnown() && !v.IsNull() && v.Type() == cty.Number {
			if n, acc := v.AsBigFloat().Int64(); acc == big.Exact && n >= 0 {
				return int(n), ""
			}
		}
		return 1, "count unresolved — priced as 1"
	}
	if expr, ok := r.Exprs["for_each"]; ok {
		if v, ok := res.ResolveExpr(expr); ok && v.IsKnown() && !v.IsNull() {
			t := v.Type()
			if t.IsListType() || t.IsSetType() || t.IsTupleType() || t.IsMapType() || t.IsObjectType() {
				return v.LengthInt(), ""
			}
		}
		return 1, "for_each unresolved — priced as 1"
	}
	return 1, ""
}
