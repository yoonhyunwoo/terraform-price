package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/provider/awsprice"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func TestClassifyItemWiring(t *testing.T) {
	cases := []struct {
		kind mapper.Kind
		want output.Kind
	}{
		{mapper.KindVariable, output.Variable},
		{mapper.KindFree, output.Free},
		{mapper.KindUnsupported, output.Unsupported},
		{mapper.Kind(99), output.Unsupported},
	}
	for _, c := range cases {
		got := classifyItem(c.kind, "addr", "aws_x", "note")
		if got.Kind != c.want {
			t.Errorf("kind %v: want output %v, got %v", c.kind, c.want, got.Kind)
		}
		if got.Addr != "addr" || got.Type != "aws_x" || got.Note != "note" {
			t.Errorf("kind %v: fields not propagated: %+v", c.kind, got)
		}
	}
}

func TestMetaCount(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(
		"n = 3\nnames = { a = 1, b = 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
resource "aws_instance" "literal"  { count = 3 }
resource "aws_instance" "via_var"  { count = var.n }
resource "aws_instance" "missing"  { count = var.missing }
resource "aws_instance" "for_each" { for_each = var.names }
resource "aws_instance" "toset"    { for_each = toset(["x"]) }
resource "aws_instance" "none"     {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := parser.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := resolver.NewResolver(dir)
	want := map[string]struct {
		n    int
		note bool
	}{
		"literal":  {3, false},
		"via_var":  {3, false},
		"missing":  {1, true},
		"for_each": {2, false},
		"toset":    {1, false},
		"none":     {1, false},
	}
	for _, r := range rs {
		w, ok := want[r.Name]
		if !ok {
			t.Fatalf("unexpected resource %q", r.Name)
		}
		n, note := metaCount(r, res)
		if n != w.n || (note != "") != w.note {
			t.Errorf("%s: metaCount = %d, %q; want %d note-present=%v", r.Name, n, note, w.n, w.note)
		}
	}
}

type capturePricer struct {
	calls int
	last  provider.Query
}

func (c *capturePricer) UnitPrice(_ context.Context, q provider.Query) (float64, string, error) {
	c.calls++
	c.last = q
	return 0.5, "Hrs", nil
}

// Regression: the Composer must sit ABOVE the cache so cache keys are computed
// from the composed query (location filter present). Wired the other way,
// different regions collide on one key and Seoul prices get served for
// other regions.
func TestBuildPricerComposesBeforeCache(t *testing.T) {
	stub := &capturePricer{}
	path := filepath.Join(t.TempDir(), "prices.json")
	var p provider.Pricer = stub
	var cacher *provider.Cached
	if path != "" {
		cacher = provider.NewCached(stub, path, time.Hour)
		p = cacher
	}
	p = awsprice.NewComposer(p)
	q := provider.Query{Service: "AmazonEC2", Region: "ap-northeast-2",
		Filters: []provider.Filter{{Field: "instanceType", Value: "t3.micro"}}}
	if _, _, err := p.UnitPrice(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.UnitPrice(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("second identical call must hit the cache, got %d upstream calls", stub.calls)
	}
	hasLoc := false
	for _, f := range stub.last.Filters {
		if f.Field == "location" && f.Value == "Asia Pacific (Seoul)" {
			hasLoc = true
		}
	}
	if !hasLoc {
		t.Fatalf("upstream must receive the composed query with a location filter, got %+v", stub.last.Filters)
	}
	if err := cacher.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Asia Pacific (Seoul)") {
		t.Fatal("cache key lost the location value — regions would collide on one key")
	}
}
