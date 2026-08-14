package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/price"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func main() {
	profileFlag := flag.String("profile", "", "AWS profile (default: tfvars account_alias)")
	noCacheFlag := flag.Bool("no-cache", false, "bypass the AWS Price List API price cache")
	flag.Parse()
	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}

	ctx := context.Background()
	res := resolver.NewResolver(dir)

	region, _ := res.VarString("aws_region")
	if region == "" {
		region = "ap-northeast-2"
	}
	profile := *profileFlag
	if profile == "" {
		profile, _ = res.VarString("account_alias")
	}
	if profile == "" {
		fmt.Fprintln(os.Stderr, "AWS profile not found in tfvars (account_alias); pass --profile <name>.")
		os.Exit(1)
	}
	service, _ := res.VarString("origin_service_name")
	if service == "" {
		service = dir
	}

	resources, err := parser.ParseDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	idx := make(map[string]*parser.Resource, len(resources))
	for _, r := range resources {
		idx[r.Type+"."+r.Name] = r
	}

	client, err := price.NewClient(ctx, profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aws config:", err)
		os.Exit(1)
	}

	pricer := price.Pricer(client)
	var cacher *price.Cached
	if !*noCacheFlag {
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			cacher = price.NewCached(client, filepath.Join(cacheDir, "terraform-price", "prices.json"), price.CacheTTL)
			pricer = cacher
		}
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
		p, unit, err := pricer.UnitPrice(ctx, spec.ServiceCode, spec.Filters, spec.PreferUnit)
		if err != nil {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: "price lookup failed: " + err.Error()})
			continue
		}
		items = append(items, output.CostItem{
			Kind: output.Fixed, Addr: addr, Type: r.Type, Spec: spec.Label,
			UnitPrice: p, Unit: unit, Monthly: p * spec.UsageQty * float64(spec.Count),
		})
	}

	output.WriteMarkdown(os.Stdout, service, region, items)

	if cacher != nil {
		if err := cacher.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
		}
	}
}

func variableItem(ctx context.Context, pricer price.Pricer, addr, typ, note string, spec *mapper.Spec) output.CostItem {
	item := output.CostItem{Kind: output.Variable, Addr: addr, Type: typ, Note: note}
	if spec == nil {
		return item
	}
	for _, rt := range spec.Rates {
		p, unit, err := pricer.UnitPrice(ctx, rt.ServiceCode, rt.Filters, rt.PreferUnit)
		if err != nil {
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
