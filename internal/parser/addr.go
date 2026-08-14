package parser

import "strings"

// Addr returns the Terraform address "type.name".
func (r *Resource) Addr() string { return r.Type + "." + r.Name }

// Index maps resources by address.
func Index(resources []*Resource) map[string]*Resource {
	idx := make(map[string]*Resource, len(resources))
	for _, r := range resources {
		idx[r.Addr()] = r
	}
	return idx
}

// SplitAddr splits "type.name"; ok=false without a separator.
func SplitAddr(addr string) (typ, name string, ok bool) {
	typ, name, found := strings.Cut(addr, ".")
	return typ, name, found
}
