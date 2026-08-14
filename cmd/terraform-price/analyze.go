package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/refres"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
	"github.com/zclconf/go-cty/cty"
)

// defaultRegion is the single owner of the fallback region used when the
// directory's tfvars do not set aws_region.
const defaultRegion = "ap-northeast-2"

// gatedResourceNote marks a resource whose count/for_each resolved to 0:
// it is not created, so nothing is priced.
const gatedResourceNote = "count = 0 — resource not created"

// analyze parses the Terraform directory and prices every resource,
// recursively expanding local-path module blocks with their input
// values. The region comes from the root directory's tfvars
// (aws_region, default ap-northeast-2) and is inherited by module
// instances.
func analyze(ctx context.Context, pricer provider.Pricer, dir string) ([]output.CostItem, error) {
	region, _ := resolver.NewResolver(dir).VarString("aws_region")
	if region == "" {
		region = defaultRegion
	}
	return analyzeDir(ctx, pricer, dir, region, "", nil, 0)
}

// maxModuleDepth bounds module recursion (defensive against symlink
// loops); real module stacks stay far shallower.
const maxModuleDepth = 5

// analyzeDir prices one directory: plain resources here plus every
// local-path module instance nested below it. prefix is the Terraform
// address prefix ("" at the root, "module.name." inside an instance);
// inputs are the module-call argument values for this instance.
func analyzeDir(ctx context.Context, pricer provider.Pricer, dir, region, prefix string, inputs map[string]cty.Value, depth int) ([]output.CostItem, error) {
	res := resolver.NewResolverWithVars(dir, inputs)
	resources, err := parser.ParseDir(dir)
	if err != nil {
		return nil, err
	}
	var plain, mods []*parser.Resource
	for _, r := range resources {
		if r.Type == "module" {
			mods = append(mods, r)
			continue
		}
		plain = append(plain, r)
	}
	idx := make(map[string]*parser.Resource, len(plain))
	for _, r := range plain {
		idx[r.Type+"."+r.Name] = r
	}

	// Resolve cross-resource references and inject into the resolver.
	// Iterate: resource refs feed locals, locals feed resource attrs —
	// loop both until stable so locals like
	//   launch_template_block = { id = one(aws_launch_template.x[*].id) }
	// and attrs referencing them both converge.
	rr := refres.New(plain, res)
	if err := rr.Verify(); err != nil {
		if depth > 0 {
			// a broken nested module must not kill the whole report
			return []output.CostItem{{Kind: output.Unsupported, Addr: prefix[:len(prefix)-1], Type: "module", Note: "module analysis failed: " + err.Error()}}, nil
		}
		return nil, err
	}
	countBased := make(map[string]bool, len(plain))
	for _, r := range plain {
		if _, ok := r.Exprs["count"]; ok {
			countBased[r.Type+"."+r.Name] = true
		}
		if _, ok := r.Exprs["for_each"]; ok {
			countBased[r.Type+"."+r.Name] = true
		}
	}
	res.SetResources(rr.AllResolved(), countBased)
	for res.RetryLocals() {
		rr.Reset()
		res.SetResources(rr.AllResolved(), countBased)
	}

	items, err := priceResources(ctx, pricer, plain, res, idx, region, prefix)
	if err != nil {
		return nil, err
	}
	for _, m := range mods {
		mitems, err := analyzeModule(ctx, pricer, dir, region, prefix, res, m, depth)
		if err != nil {
			return nil, err
		}
		items = append(items, mitems...)
	}
	return items, nil
}

// moduleMetaAttrs are module-block arguments that are not inputs.
var moduleMetaAttrs = map[string]bool{
	"source": true, "count": true, "for_each": true, "providers": true, "depends_on": true,
}

// analyzeModule prices one module block. Local-path sources are
// expanded recursively with the block's input values layered over the
// module's own variable defaults; public-registry sources are fetched
// from the registry tarball (cached) and expanded the same way.
// Anything unfetchable keeps the generic "not parsed" info row.
func analyzeModule(ctx context.Context, pricer provider.Pricer, dir, region, prefix string, parent *resolver.Resolver, m *parser.Resource, depth int) ([]output.CostItem, error) {
	addr := prefix + "module." + m.Name
	// Gate on the module block's own count/for_each: 0 creates nothing.
	if n, note := metaCount(m, parent); n == 0 && note == "" {
		return []output.CostItem{{Kind: output.Fixed, Addr: addr, Unresolved: "count = 0 — module not created"}}, nil
	}

	src := ""
	if e, ok := m.Exprs["source"]; ok {
		if v, ok := parent.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() && v.Type() == cty.String {
			src = v.AsString()
		}
	}
	modDir := localModuleDir(dir, src)
	if modDir == "" {
		modDir = registryModuleDir(m, parent, src)
	}
	if modDir == "" || depth >= maxModuleDepth {
		kind, _, note := mapper.MapResource(m, parent, nil, region)
		return []output.CostItem{classifyItem(kind, addr, m.Type, note)}, nil
	}

	inputs := map[string]cty.Value{}
	for k, e := range m.Exprs {
		if moduleMetaAttrs[k] {
			continue
		}
		if v, ok := parent.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() {
			inputs[k] = v
		}
	}
	return analyzeDir(ctx, pricer, modDir, region, addr+".", inputs, depth+1)
}

// localModuleDir resolves a "./" / "../" source to a directory, or ""
// when the source is remote/registry-shaped.
func localModuleDir(dir, src string) string {
	if src == "" || strings.Contains(src, "://") || strings.HasPrefix(src, "git::") {
		return ""
	}
	if parseRegistrySource(src) != nil {
		return ""
	}
	modDir := filepath.Clean(filepath.Join(dir, src))
	if st, err := os.Stat(modDir); err != nil || !st.IsDir() {
		return ""
	}
	return modDir
}

// registryModuleDir fetches a public-registry module (exact or ~>-pinned
// version, else latest) into the cache and returns its directory; "" on
// any failure. A `version` attr of the module block is the pin.
func registryModuleDir(m *parser.Resource, parent *resolver.Resolver, src string) string {
	rs := parseRegistrySource(src)
	if rs == nil {
		return ""
	}
	pin := ""
	if e, ok := m.Exprs["version"]; ok {
		if v, ok := parent.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() && v.Type() == cty.String {
			pin = v.AsString()
		}
	}
	version, err := resolveVersion(rs, pin)
	if err != nil {
		return ""
	}
	dir, ok := fetchRegistryModule(rs, version)
	if !ok {
		return ""
	}
	return dir
}

// priceResources maps and prices a directory's direct resources.
func priceResources(ctx context.Context, pricer provider.Pricer, resources []*parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource, region, prefix string) ([]output.CostItem, error) {
	var items []output.CostItem
	for _, r := range resources {
		addr := prefix + r.Type + "." + r.Name
		// count/for_each = 0 gates everything: the resource is not created,
		// so resolution failures below are moot. metaCount returns a note
		// whenever it falls back to 1, so n == 0 always means a resolved 0.
		n, metaNote := metaCount(r, res)
		if n == 0 {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: gatedResourceNote})
			continue
		}
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
