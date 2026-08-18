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
	"github.com/yoonhyunwoo/terraform-price/internal/tf/resolver"
)

const priceCacheTTL = 7 * 24 * time.Hour

func main() {
	noCacheFlag := flag.Bool("no-cache", false, "bypass the AWS Price List API price cache")
	priceFileFlag := flag.String("price-file", "", "JSON price file to seed lookups (same format as the cache, never expires; misses fall through to the network and successful lookups are written back)")
	formatFlag := flag.String("format", "full", "report format: full (all tables) or compact (CI summary)")
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
		region = defaultRegion
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
	var cachers []*provider.Cached
	if !*noCacheFlag {
		home, err := os.UserHomeDir()
		if err == nil {
			cacheDir := filepath.Join(home, ".cache", "terraform-price")
			bulk := awsprice.NewBulk(client, filepath.Join(cacheDir, "bulk"), priceCacheTTL)
			inner := provider.Fallback{Primary: bulk, Secondary: client}
			c := provider.NewCached(inner, filepath.Join(cacheDir, "prices.json"), priceCacheTTL)
			cachers = append(cachers, c)
			pricer = c
		}
	}
	if *priceFileFlag != "" {
		c := provider.NewCached(pricer, *priceFileFlag, 0)
		cachers = append(cachers, c)
		pricer = c
	}
	pricer = awsprice.NewComposer(pricer)

	items, err := analyze(ctx, pricer, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	if *formatFlag != "compact" || *baselineFlag == "" {
		if *formatFlag == "compact" {
			output.WriteCompact(os.Stdout, service, region, items)
		} else {
			output.WriteMarkdown(os.Stdout, service, region, items)
		}
	}
	if *baselineFlag != "" {
		baseItems, err := analyze(ctx, pricer, *baselineFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "baseline parse:", err)
			os.Exit(1)
		}
		rows, totals := delta.Compute(baseItems, items)
		if *formatFlag == "compact" {
			delta.WriteCompact(os.Stdout, rows, totals)
		} else {
			delta.WriteMarkdown(os.Stdout, *baselineFlag, rows, totals)
		}
	}

	for _, c := range cachers {
		if err := c.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
		}
	}
}
