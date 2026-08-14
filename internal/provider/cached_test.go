package provider

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func mkFilter(field, value string) Filter {
	return Filter{Field: field, Value: value}
}

func TestCacheKeyOrderIndependent(t *testing.T) {
	k1 := cacheKey(Query{Service: "AmazonEC2", Filters: []Filter{
		mkFilter("instanceType", "t3.micro"),
		mkFilter("location", "Seoul"),
	}})
	k2 := cacheKey(Query{Service: "AmazonEC2", Filters: []Filter{
		mkFilter("location", "Seoul"),
		mkFilter("instanceType", "t3.micro"),
	}})
	if k1 != k2 {
		t.Fatalf("order-independent key mismatch:\n %q\n %q", k1, k2)
	}
}

func TestCacheKeyFormatStable(t *testing.T) {
	k := cacheKey(Query{Service: "AmazonEC2", Filters: []Filter{
		mkFilter("instanceType", "t3.micro"),
		mkFilter("location", "Seoul"),
	}, PreferUnit: "Hrs"})
	want := "AmazonEC2|instanceType=t3.micro,location=Seoul|Hrs"
	if k != want {
		t.Fatalf("key format drifted (existing cache files would be invalidated):\n got  %q\n want %q", k, want)
	}
}

func TestCacheKeyDistinguishes(t *testing.T) {
	a := cacheKey(Query{Service: "AmazonEC2", Filters: []Filter{mkFilter("location", "Seoul")}})
	b := cacheKey(Query{Service: "AmazonEC2", Filters: []Filter{mkFilter("location", "Tokyo")}})
	c := cacheKey(Query{Service: "AmazonEC2", Filters: []Filter{mkFilter("location", "Seoul")}, PreferUnit: "GB-Mo"})
	if a == b || a == c {
		t.Fatalf("expected distinct keys, got a=%q b=%q c=%q", a, b, c)
	}
}

func TestCacheSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.json")
	c1 := NewCached(nil, path, time.Hour)
	c1.data["k"] = cacheEntry{Price: 0.123, Unit: "Hrs", CachedAt: time.Now().Unix()}
	c1.dirty = true
	if err := c1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	c2 := NewCached(nil, path, time.Hour)
	e, ok := c2.data["k"]
	if !ok {
		t.Fatal("entry not loaded after save")
	}
	if e.Price != 0.123 || e.Unit != "Hrs" {
		t.Fatalf("roundtrip mismatch: %+v", e)
	}
}

type stubPricer struct {
	calls int
}

func (s *stubPricer) UnitPrice(ctx context.Context, q Query) (float64, string, error) {
	s.calls++
	return 0.05, "Hrs", nil
}

func TestCacheHitSkipsInner(t *testing.T) {
	stub := &stubPricer{}
	c := NewCached(stub, filepath.Join(t.TempDir(), "prices.json"), time.Hour)
	q := Query{Service: "AmazonEC2", Filters: []Filter{mkFilter("location", "Seoul")}}
	if _, _, err := c.UnitPrice(nil, q); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.UnitPrice(nil, q); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("want 1 inner call (second must hit cache), got %d", stub.calls)
	}
}

func TestCacheExpiredIgnored(t *testing.T) {
	c := NewCached(nil, "does-not-exist", time.Nanosecond)
	c.data["stale"] = cacheEntry{Price: 9.9, Unit: "Hrs", CachedAt: time.Now().Add(-time.Hour).Unix()}
	stub := &stubPricer{}
	c.inner = stub
	q := Query{Service: "AmazonEC2"}
	p, unit, err := c.UnitPrice(nil, q)
	if err != nil || p != 0.05 || unit != "Hrs" {
		t.Fatalf("expired entry should miss and re-fetch, got %v %q %v", p, unit, err)
	}
	if stub.calls != 1 {
		t.Fatalf("inner should be called for expired entry, got %d calls", stub.calls)
	}
}
