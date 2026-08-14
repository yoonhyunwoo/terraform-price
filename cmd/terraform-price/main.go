package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/provider/awsprice"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func buildPricer(client provider.Pricer, noCache bool, cachePath string) (provider.Pricer, *provider.Cached) {
	var inner provider.Pricer = client
	var cacher *provider.Cached
	if cachePath != "" && !noCache {
		cacher = provider.NewCached(client, cachePath, provider.CacheTTL)
		inner = cacher
	}
	return awsprice.NewComposer(inner), cacher
}

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

	client, err := awsprice.NewClient(ctx, profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aws config:", err)
		os.Exit(1)
	}

	cachePath := ""
	if !*noCacheFlag {
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			cachePath = filepath.Join(cacheDir, "terraform-price", "prices.json")
		}
	}
	pricer, cacher := buildPricer(client, *noCacheFlag, cachePath)

	items, err := analyze(ctx, pricer, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	output.WriteMarkdown(os.Stdout, service, region, items)

	if cacher != nil {
		if err := cacher.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
		}
	}
}

