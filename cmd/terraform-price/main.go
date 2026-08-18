package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yoonhyunwoo/terraform-price/internal/delta"
	"github.com/yoonhyunwoo/terraform-price/internal/i18n"
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
	langFlag := flag.String("lang", "", "report language: "+strings.Join(i18n.Languages, ", ")+" (default en; falls back to TFPRICE_LANG, LC_ALL, LC_MESSAGES, LANG)")
	flag.Parse()
	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}

	prefs := languagePrefs(*langFlag)
	l := i18n.New(prefs...)

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
			output.WriteCompact(os.Stdout, l, service, region, items)
		} else {
			output.WriteMarkdown(os.Stdout, l, service, region, items)
		}
	}
	if *baselineFlag != "" {
		baseItems, err := analyze(ctx, pricer, *baselineFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "baseline parse:", err)
			os.Exit(1)
		}
		rows, totals := delta.Compute(l, baseItems, items)
		if *formatFlag == "compact" {
			delta.WriteCompact(os.Stdout, l, rows, totals)
		} else {
			delta.WriteMarkdown(os.Stdout, l, *baselineFlag, rows, totals)
		}
	}

	for _, c := range cachers {
		if err := c.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
		}
	}
}

// languagePrefs resolves --lang > TFPRICE_LANG > LC_ALL > LC_MESSAGES > LANG
// (kubectl's env order); every entry is a negotiation hint, so an unsupported
// value lands on English inside i18n.New rather than erroring.
func languagePrefs(flagVal string) []string {
	if flagVal != "" {
		return []string{flagVal}
	}
	var prefs []string
	for _, k := range []string{"TFPRICE_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			prefs = append(prefs, v)
		}
	}
	return prefs
}
