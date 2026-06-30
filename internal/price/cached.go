package price

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ptypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

const CacheTTL = 7 * 24 * time.Hour

type cacheEntry struct {
	Price    float64 `json:"price"`
	Unit     string  `json:"unit"`
	CachedAt int64   `json:"cached_at"`
}

type Cached struct {
	inner Pricer
	path  string
	ttl   time.Duration
	data  map[string]cacheEntry
	dirty bool
}

func NewCached(inner Pricer, path string, ttl time.Duration) *Cached {
	c := &Cached{inner: inner, path: path, ttl: ttl, data: map[string]cacheEntry{}}
	c.load()
	return c
}

func (c *Cached) UnitPrice(ctx context.Context, serviceCode string, filters []ptypes.Filter, preferUnit string) (float64, string, error) {
	key := cacheKey(serviceCode, filters, preferUnit)
	if e, ok := c.data[key]; ok && time.Since(time.Unix(e.CachedAt, 0)) <= c.ttl {
		return e.Price, e.Unit, nil
	}
	p, unit, err := c.inner.UnitPrice(ctx, serviceCode, filters, preferUnit)
	if err != nil {
		return 0, "", err
	}
	c.data[key] = cacheEntry{Price: p, Unit: unit, CachedAt: time.Now().Unix()}
	c.dirty = true
	return p, unit, nil
}

func (c *Cached) Save() error {
	if !c.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o644)
}

func (c *Cached) load() {
	b, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	json.Unmarshal(b, &c.data)
}

func cacheKey(serviceCode string, filters []ptypes.Filter, preferUnit string) string {
	parts := make([]string, 0, len(filters))
	for _, f := range filters {
		parts = append(parts, aws.ToString(f.Field)+"="+aws.ToString(f.Value))
	}
	sort.Strings(parts)
	return serviceCode + "|" + strings.Join(parts, ",") + "|" + preferUnit
}
