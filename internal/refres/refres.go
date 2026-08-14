// SPDX-License-Identifier: Apache-2.0
// Package refres resolves cross-resource references (aws_instance.a.instance_type)
// using the verify-before-resolve pattern: cycle detection at registration,
// lazy memoized resolution at lookup.
package refres

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

type RefResolver struct {
	resources map[string]*parser.Resource
	res       *resolver.Resolver

	// resolved caches attribute values: "aws_instance.a" → {"instance_type": cty.StringVal("t3.micro")}
	resolved map[string]map[string]cty.Value
	// resolving tracks in-flight addresses for cycle detection
	resolving map[string]bool
}

func New(resources []*parser.Resource, res *resolver.Resolver) *RefResolver {
	idx := make(map[string]*parser.Resource, len(resources))
	for _, r := range resources {
		idx[r.Type+"."+r.Name] = r
	}
	return &RefResolver{
		resources: idx,
		res:       res,
		resolved:  make(map[string]map[string]cty.Value),
		resolving: make(map[string]bool),
	}
}

// Verify checks the reference graph is acyclic. Returns an error with the
// full cycle path (e.g. "aws_a.x -> aws_b.y -> aws_a.x").
func (r *RefResolver) Verify() error {
	visited := make(map[string]bool)
	var check func(addr string, path []string) error
	check = func(addr string, path []string) error {
		for _, p := range path {
			if p == addr {
				return fmt.Errorf("reference cycle: %s", strings.Join(append(path, addr), " -> "))
			}
		}
		if visited[addr] {
			return nil
		}
		visited[addr] = true
		for _, dep := range r.deps(addr) {
			if err := check(dep, append(path, addr)); err != nil {
				return err
			}
		}
		return nil
	}
	for addr := range r.resources {
		if err := check(addr, nil); err != nil {
			return err
		}
	}
	return nil
}

// deps returns addresses referenced by the resource's expressions.
func (r *RefResolver) deps(addr string) []string {
	res, ok := r.resources[addr]
	if !ok {
		return nil
	}
	var out []string
	for _, expr := range res.Exprs {
		out = append(out, exprDeps(expr)...)
	}
	return out
}

func exprDeps(expr hcl.Expression) []string {
	ste, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok {
		return nil
	}
	t := hcl.Traversal(ste.Traversal)
	if len(t) < 2 {
		return nil
	}
	root, ok := t[0].(hcl.TraverseRoot)
	if !ok {
		return nil
	}
	if root.Name == "var" || root.Name == "local" || root.Name == "data" || root.Name == "module" {
		return nil
	}
	name, ok := t[1].(hcl.TraverseAttr)
	if !ok {
		return nil
	}
	return []string{root.Name + "." + name.Name}
}

// ResolveAttr resolves an attribute of a resource, following references.
// addr is "type.name", attr is the attribute key (e.g. "instance_type").
func (r *RefResolver) ResolveAttr(addr, attr string) (cty.Value, bool) {
	attrs := r.ResolveResource(addr)
	if attrs == nil {
		return cty.NilVal, false
	}
	v, ok := attrs[attr]
	if !ok || !v.IsKnown() || v.IsNull() {
		return cty.NilVal, false
	}
	return v, true
}

// ResolveResource resolves all attributes of a resource, following
// references recursively. Results are memoized.
func (r *RefResolver) ResolveResource(addr string) map[string]cty.Value {
	if attrs, ok := r.resolved[addr]; ok {
		return attrs
	}
	if r.resolving[addr] {
		return nil // cycle — should have been caught by Verify
	}
	res, ok := r.resources[addr]
	if !ok {
		return nil
	}
	r.resolving[addr] = true
	defer delete(r.resolving, addr)

	attrs := make(map[string]cty.Value, len(res.Exprs))
	for key, expr := range res.Exprs {
		if v, ok := r.resolveExpr(expr, addr); ok {
			attrs[key] = v
		}
	}
	r.resolved[addr] = attrs
	return attrs
}

// resolveExpr evaluates an expression. Scope traversals to other resources
// are resolved recursively; everything else goes to the base resolver.
func (r *RefResolver) resolveExpr(expr hcl.Expression, fromAddr string) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		t := hcl.Traversal(ste.Traversal)
		if len(t) >= 2 {
			if root, ok := t[0].(hcl.TraverseRoot); ok {
				if root.Name != "var" && root.Name != "local" && root.Name != "data" && root.Name != "module" {
					if name, ok := t[1].(hcl.TraverseAttr); ok {
						targetAddr := root.Name + "." + name.Name
						if targetAddr == fromAddr {
							return cty.NilVal, false // self-reference
						}
						// Resolve the target resource
						targetAttrs := r.ResolveResource(targetAddr)
						if targetAttrs == nil {
							return cty.NilVal, false
						}
						// Navigate remaining path (e.g. .instance_type)
						return navigateAttrs(targetAttrs, t[2:])
					}
				}
			}
		}
	}
	// Non-resource reference: delegate to base resolver (var/local/functions)
	return r.res.ResolveExpr(expr)
}

func navigateAttrs(attrs map[string]cty.Value, steps []hcl.Traverser) (cty.Value, bool) {
	var val cty.Value
	var ok bool
	for i, step := range steps {
		switch s := step.(type) {
		case hcl.TraverseAttr:
			key := s.Name
			if i == 0 {
				val, ok = attrs[key]
				if !ok {
					return cty.NilVal, false
				}
			} else {
				if !val.Type().IsObjectType() || !val.Type().HasAttribute(key) {
					return cty.NilVal, false
				}
				val = val.GetAttr(key)
			}
		case hcl.TraverseIndex:
			idx := s.Key
			switch {
			case idx.Type() == cty.String && val.Type().IsObjectType():
				if !val.Type().HasAttribute(idx.AsString()) {
					return cty.NilVal, false
				}
				val = val.GetAttr(idx.AsString())
			case idx.Type() == cty.Number:
				i64, _ := idx.AsBigFloat().Int64()
				var elems []cty.Value
				if val.Type().IsListType() || val.Type().IsTupleType() || val.Type().IsSetType() {
					elems = val.AsValueSlice()
				}
				if int(i64) < 0 || int(i64) >= len(elems) {
					return cty.NilVal, false
				}
				val = elems[i64]
			default:
				return cty.NilVal, false
			}
		default:
			return cty.NilVal, false
		}
		if !val.IsKnown() {
			return cty.NilVal, false
		}
	}
	return val, true
}

// AllResolved returns all resolved resource attributes.
// Call after ResolveResource for each resource.
func (r *RefResolver) AllResolved() map[string]map[string]cty.Value {
	for addr := range r.resources {
		r.ResolveResource(addr)
	}
	return r.resolved
}

// Reset clears the resolution cache so resources re-resolve with the
// resolver's updated scope (e.g. after RetryLocals).
func (r *RefResolver) Reset() {
	r.resolved = make(map[string]map[string]cty.Value)
}
