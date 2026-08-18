package provider

import (
	"context"
	"errors"
	"testing"
)

func testQ(service string, field, value string) Query {
	return Query{Service: service, Filters: []Filter{{Field: field, Value: value}}, PreferUnit: "Hrs"}
}

func TestEmbeddedHitAndFallback(t *testing.T) {
	catalog := `{"AmazonEC2|instanceType=t3.medium|Hrs": {"price": 0.0416, "unit": "Hrs", "cached_at": 0}}`
	var innerErr = innerErrStub
	inner := failPricer{}
	e, err := LoadEmbedded([]byte(catalog), inner)
	if err != nil {
		t.Fatal(err)
	}
	p, unit, err := e.UnitPrice(context.Background(), testQ("AmazonEC2", "instanceType", "t3.medium"))
	if err != nil || p != 0.0416 || unit != "Hrs" {
		t.Fatalf("hit: %v %v %v", p, unit, err)
	}
	if e.Hits() != 1 {
		t.Fatalf("hits = %d, want 1", e.Hits())
	}
	// Miss falls through to the live client; a client error surfaces unchanged.
	if _, _, err := e.UnitPrice(context.Background(), testQ("AmazonEC2", "instanceType", "t3.nano")); !errors.Is(err, innerErr) {
		t.Fatalf("miss should reach inner, got %v", err)
	}
	if e.Hits() != 1 {
		t.Fatalf("miss must not count as a hit: %d", e.Hits())
	}
}

func TestEmbeddedKeyIncludesRegionFilter(t *testing.T) {
	// The cache key embeds the location filter the Composer injects, so the
	// catalog naturally distinguishes regions.
	q := testQ("AmazonEC2", "instanceType", "t3.medium")
	q.Filters = append(q.Filters, Filter{Field: "location", Value: "Asia Pacific (Seoul)"})
	key := cacheKey(q)
	if key != "AmazonEC2|instanceType=t3.medium,location=Asia Pacific (Seoul)|Hrs" {
		t.Fatalf("unexpected key %q", key)
	}
}

type failPricer struct{}

func (failPricer) UnitPrice(context.Context, Query) (float64, string, error) {
	return 0, "", innerErrStub
}

var innerErrStub = errors.New("inner called")
