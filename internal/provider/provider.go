package provider

import "context"

type Filter struct {
	Field string
	Value string
}

type Query struct {
	Service    string
	Filters    []Filter
	PreferUnit string
}

type Pricer interface {
	UnitPrice(ctx context.Context, q Query) (float64, string, error)
}
