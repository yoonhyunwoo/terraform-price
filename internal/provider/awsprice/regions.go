package awsprice

import (
	"context"
	"fmt"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

var regionToLocation = map[string]string{
	"ap-northeast-2": "Asia Pacific (Seoul)",
	"ap-northeast-1": "Asia Pacific (Tokyo)",
	"ap-northeast-3": "Asia Pacific (Osaka)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
	"ap-southeast-2": "Asia Pacific (Sydney)",
	"us-east-1":      "US East (N. Virginia)",
	"us-west-2":      "US West (Oregon)",
	"eu-west-1":      "Europe (Ireland)",
	"eu-central-1":   "Europe (Frankfurt)",
}

var regionToUsagePrefix = map[string]string{
	"ap-northeast-2": "APN2",
	"ap-northeast-1": "APN1",
	"ap-northeast-3": "APN3",
	"ap-southeast-1": "APS1",
	"ap-southeast-2": "APS2",
	"us-east-1":      "USE1",
	"us-west-2":      "USW2",
	"eu-west-1":      "EU",
	"eu-central-1":   "EUC1",
}

// compose must run before provider.Cached: the location filter it injects is
// part of the cache key, so regions never collide on one key.
func compose(q provider.Query) (provider.Query, error) {
	loc, ok := regionToLocation[q.Region]
	if !ok {
		return provider.Query{}, fmt.Errorf("unknown region %q (%d regions supported)", q.Region, len(regionToLocation))
	}
	prefix := regionToUsagePrefix[q.Region]
	filters := make([]provider.Filter, 0, len(q.Filters)+1)
	for _, f := range q.Filters {
		if f.Field == "usagetype" {
			f.Value = prefix + "-" + f.Value
		}
		filters = append(filters, f)
	}
	filters = append(filters, provider.Filter{Field: "location", Value: loc})
	return provider.Query{Service: q.Service, Region: q.Region, Filters: filters, PreferUnit: q.PreferUnit}, nil
}

type Composer struct {
	inner provider.Pricer
}

func NewComposer(inner provider.Pricer) *Composer {
	return &Composer{inner: inner}
}

func (c *Composer) UnitPrice(ctx context.Context, q provider.Query) (float64, string, error) {
	q, err := compose(q)
	if err != nil {
		return 0, "", err
	}
	return c.inner.UnitPrice(ctx, q)
}
