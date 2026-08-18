package provider

import (
	"context"
	"encoding/json"
	"sync/atomic"
)

// Embedded serves prices from a read-only catalog bundled at build time.
// It sits at the bottom of the Pricer chain: any fresher source (price file,
// disk cache, bulk data, live API) wins; only their misses land here, so
// credentials-free runs still price common resources. Hits are counted so the
// report can disclose catalog provenance.
type Embedded struct {
	data  map[string]cacheEntry
	inner Pricer
	hits  atomic.Int64
}

// LoadEmbedded parses catalog bytes in the price-file format (map from cache
// key to {price, unit, cached_at}).
func LoadEmbedded(b []byte, inner Pricer) (*Embedded, error) {
	data := map[string]cacheEntry{}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	return &Embedded{data: data, inner: inner}, nil
}

func (e *Embedded) UnitPrice(ctx context.Context, q Query) (float64, string, error) {
	if entry, ok := e.data[cacheKey(q)]; ok {
		e.hits.Add(1)
		return entry.Price, entry.Unit, nil
	}
	return e.inner.UnitPrice(ctx, q)
}

// Hits reports how many lookups the catalog answered.
func (e *Embedded) Hits() int64 { return e.hits.Load() }
