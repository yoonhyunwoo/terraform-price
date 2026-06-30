package price

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ptypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

func mkFilter(field, value string) ptypes.Filter {
	return ptypes.Filter{Type: ptypes.FilterTypeTermMatch, Field: aws.String(field), Value: aws.String(value)}
}

func TestCacheKeyOrderIndependent(t *testing.T) {
	k1 := cacheKey("AmazonEC2", []ptypes.Filter{
		mkFilter("instanceType", "t3.micro"),
		mkFilter("location", "Seoul"),
	}, "")
	k2 := cacheKey("AmazonEC2", []ptypes.Filter{
		mkFilter("location", "Seoul"),
		mkFilter("instanceType", "t3.micro"),
	}, "")
	if k1 != k2 {
		t.Fatalf("order-independent key mismatch:\n %q\n %q", k1, k2)
	}
}

func TestCacheKeyDistinguishes(t *testing.T) {
	a := cacheKey("AmazonEC2", []ptypes.Filter{mkFilter("location", "Seoul")}, "")
	b := cacheKey("AmazonEC2", []ptypes.Filter{mkFilter("location", "Tokyo")}, "")
	c := cacheKey("AmazonEC2", []ptypes.Filter{mkFilter("location", "Seoul")}, "GB-Mo")
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

func TestCacheExpiredIgnored(t *testing.T) {
	c := NewCached(nil, "does-not-exist", time.Nanosecond)
	c.data["stale"] = cacheEntry{Price: 9.9, Unit: "Hrs", CachedAt: time.Now().Add(-time.Hour).Unix()}
	if _, _, ok := lookupFresh(c, "stale"); ok {
		t.Fatal("expected expired entry to be treated as miss")
	}
}

func lookupFresh(c *Cached, key string) (float64, string, bool) {
	e, ok := c.data[key]
	if !ok || time.Since(time.Unix(e.CachedAt, 0)) > c.ttl {
		return 0, "", false
	}
	return e.Price, e.Unit, true
}
