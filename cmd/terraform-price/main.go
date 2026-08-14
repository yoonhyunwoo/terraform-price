package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yoonhyunwoo/terraform-price/internal/delta"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/provider/awsprice"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

const priceCacheTTL = 7 * 24 * time.Hour

func main() {
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
	service, _ := res.VarString("origin_service_name")
	if service == "" {
		service = dir
	}

	client, err := awsprice.NewClient(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aws:", err)
		os.Exit(1)
	}

	var pricer provider.Pricer = client
	var cacher *provider.Cached
	if !*noCacheFlag {
		home, err := os.UserHomeDir()
		if err == nil {
			cachePath := filepath.Join(home, ".cache", "terraform-price", "prices.json")
			cacher = provider.NewCached(client, cachePath, priceCacheTTL)
			pricer = cacher
		}
	}
	pricer = awsprice.NewComposer(pricer)

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
