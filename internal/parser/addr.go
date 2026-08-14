package parser

import "strings"

// Addr returns the Terraform address "type.name".
func (r *Resource) Addr() string { return r.Type + "." + r.Name }

// Index maps resources by address ("aws_instance.a" -> resource).
func Index(resources []*Resource) map[string]*Resource {
	idx := make(map[string]*Resource, len(resources))
	for _, r := range resources {
		idx[r.Addr()] = r
	}
	return idx
}

// SplitAddr splits a resource address "type.name" into its parts.
// ok=false when the address has no "." separator.
func SplitAddr(addr string) (typ, name string, ok bool) {
	typ, name, found := strings.Cut(addr, ".")
	return typ, name, found
}
