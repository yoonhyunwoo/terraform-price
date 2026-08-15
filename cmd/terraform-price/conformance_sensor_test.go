package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/provider/awsprice"
)

// The conformance sensor: prices the vendored infracost fixtures with LIVE
// prices and reports dollar divergence from their goldens. Usage-default and
// price-snapshot drift make this advisory — it logs, never fails. Skipped
// unless run explicitly:
//
//	TF_CONFORMANCE_SENSOR=1 go test -run TestConformanceSensor -count=1 -v
func TestConformanceSensor(t *testing.T) {
	if os.Getenv("TF_CONFORMANCE_SENSOR") == "" {
		t.Skip("set TF_CONFORMANCE_SENSOR=1 to run the live-price sensor")
	}
	ctx := context.Background()
	client, err := awsprice.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pricer provider.Pricer = client
	if home, err := os.UserHomeDir(); err == nil {
		cacheDir := filepath.Join(home, ".cache", "terraform-price")
		bulk := awsprice.NewBulk(client, filepath.Join(cacheDir, "bulk"), priceCacheTTL)
		pricer = provider.Fallback{Primary: bulk, Secondary: client}
		pricer = provider.NewCached(pricer, filepath.Join(cacheDir, "prices.json"), priceCacheTTL)
	}
	pricer = awsprice.NewComposer(pricer)

	root := filepath.Join("..", "..", "testdata", "conformance", "aws")
	dirs, _ := filepath.Glob(filepath.Join(root, "*_test"))
	sort.Strings(dirs)
	if len(dirs) == 0 {
		t.Fatal("fixtures missing")
	}
	match, mismatch, skipped := 0, 0, 0
	for _, d := range dirs {
		items, err := analyze(ctx, pricer, d)
		if err != nil {
			t.Errorf("%s: analyze: %v", filepath.Base(d), err)
			continue
		}
		ours := map[string]float64{}
		for _, it := range items {
			if it.Kind == output.Fixed && it.UnitPrice > 0 {
				ours[it.Addr] = it.Monthly // only rows we actually price
			}
		}
		for _, g := range goldenCosts(d) {
			om, ok := ours[g.addr]
			if !ok || len(g.comps) != 1 {
				skipped++
				continue
			}
			gc := g.comps[0]
			switch {
			case gc == 0 && om == 0,
				gc > 0 && math.Abs(om-gc)/gc <= 0.02:
				match++
			default:
				mismatch++
				delta := 0.0
				if gc != 0 {
					delta = (om - gc) / gc * 100
				}
				t.Logf("MISMATCH %s %s: golden $%.2f ours $%.2f (%+.0f%%)",
					filepath.Base(d), g.addr, gc, om, delta)
			}
		}
	}
	if match+mismatch > 0 {
		t.Logf("sensor: match=%d mismatch=%d skipped=%d rate=%.1f%%",
			match, mismatch, skipped, 100*float64(match)/float64(match+mismatch))
	}
}

type goldenCost struct {
	addr  string
	comps []float64
}

var sensorCompLine = regexp.MustCompile(`^[ │├└─]*(.+?)\s{2,}([\d,.]+)\s+[\w/-]+\s+\$([\d,.]+)\s*$`)
var sensorTopLine = regexp.MustCompile(`^ ([a-z][\w.\[\]"-]+)\s*$`)

func goldenCosts(dir string) []goldenCost {
	goldens, _ := filepath.Glob(filepath.Join(dir, "*.golden"))
	if len(goldens) == 0 {
		return nil
	}
	b, err := os.ReadFile(goldens[0])
	if err != nil {
		return nil
	}
	var out []goldenCost
	var cur *goldenCost
	for _, line := range strings.Split(string(b), "\n") {
		if m := sensorTopLine.FindStringSubmatch(line); m != nil {
			out = append(out, goldenCost{addr: m[1]})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if m := sensorCompLine.FindStringSubmatch(line); m != nil {
			if c, err := strconv.ParseFloat(strings.ReplaceAll(m[3], ",", ""), 64); err == nil {
				cur.comps = append(cur.comps, c)
			}
		}
	}
	return out
}
