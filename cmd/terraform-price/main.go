package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yoonhyunwoo/terraform-price/internal/delta"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/provider/awsprice"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func buildPricer(client provider.Pricer, noCache bool, cachePath string) (provider.Pricer, *provider.Cached) {
	var inner provider.Pricer = client
	var cacher *provider.Cached
	if cachePath != "" && !noCache {
		cacher = provider.NewCached(cachePath, inner)
		inner = cacher
	}
	return awsprice.NewComposer(inner), cacher
}

func main() {
	profileFlag := flag.String("profile", "", "AWS profile (default: tfvars account_alias)")
	noCacheFlag := flag.Bool("no-cache", false, "bypass the AWS Price List API price cache")
	baselineFlag := flag.String("baseline", "", "baseline directory to diff against (e.g. a checkout of the merge-target branch)")
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
		profile = "default"
	}
	service, _ := res.VarString("origin_service_name")
	if service == "" {
		service = dir
	}

	client, err := awsprice.NewClient(ctx, profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aws:", err)
		os.Exit(1)
	}

	cachePath := ""
	if !*noCacheFlag {
		home, err := os.UserHomeDir()
		if err == nil {
			cachePath = filepath.Join(home, ".cache", "terraform-price", "prices.json")
		}
	}
	pricer, cacher := buildPricer(client, *noCacheFlag, cachePath)

	items, err := analyze(ctx, pricer, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	output.WriteMarkdown(os.Stdout, service, region, items)

	if *baselineFlag != "" {
		baseItems, err := analyze(ctx, pricer, *baselineFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "baseline parse:", err)
			os.Exit(1)
		}
		rows, totals := delta.Compute(baseItems, items)
		delta.WriteMarkdown(os.Stdout, *baselineFlag, rows, totals)
	}

	if cacher != nil {
		if err := cacher.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
		}
	}
}
