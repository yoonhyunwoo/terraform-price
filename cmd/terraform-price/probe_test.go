package main

import (
	"context"
	"os"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func TestProbeEKSNodeGroup(t *testing.T) {
	dir := os.Getenv("PROBE_DIR")
	if dir == "" {
		t.Skip("PROBE_DIR unset")
	}
	items, err := analyze(context.Background(), fakePricer{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Addr != "" && (it.Unresolved != "" || it.Note != "") {
			t.Logf("ITEM %s unresolved=%q note=%q", it.Addr, it.Unresolved, it.Note)
		}
	}
	_ = mapper.MapResource
	_ = parser.ParseDir
	_ = resolver.NewResolver
}
