package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"terraform-price/internal/mapper"
	"terraform-price/internal/output"
	"terraform-price/internal/parser"
	"terraform-price/internal/price"
	"terraform-price/internal/resolver"
)

func main() {
	profileFlag := flag.String("profile", "", "AWS profile (default: tfvars account_alias)")
	noCacheFlag := flag.Bool("no-cache", false, "AWS Price List API 결과 캐시 사용 안 함")
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
		fmt.Fprintln(os.Stderr, "AWS 프로파일을 tfvars(account_alias)에서 찾을 수 없습니다. --profile <이름> 지정.")
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
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: "단가 조회 실패: " + err.Error()})
			continue
		}
		items = append(items, output.CostItem{
			Kind: output.Fixed, Addr: addr, Type: r.Type, Spec: spec.Label,
			UnitPrice: p, Unit: unit, Monthly: p * spec.UsageQty * float64(spec.Count),
		})
	}

	gaps := 0
	for _, it := range items {
		if it.Kind == output.Unsupported {
			gaps++
		}
	}
	if gaps > 0 {
		fmt.Fprintf(os.Stderr, "⚠️ 미지원 과금 리소스 %d건이 단가 매핑에서 누락되어 추정에서 제외됨 — 보고서 '미지원 과금 리소스' 섹션 확인\n", gaps)
	}
	output.WriteMarkdown(os.Stdout, service, region, items)

	if cacher != nil {
		if err := cacher.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
		}
	}
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
