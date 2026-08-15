package provider

import "context"

// Fallback delegates to Secondary whenever Primary errors, so a flaky bulk
// source degrades to the per-query API without callers knowing.
type Fallback struct {
	Primary   Pricer
	Secondary Pricer
}

func (f Fallback) UnitPrice(ctx context.Context, q Query) (float64, string, error) {
	p, unit, err := f.Primary.UnitPrice(ctx, q)
	if err == nil {
		return p, unit, nil
	}
	return f.Secondary.UnitPrice(ctx, q)
}
