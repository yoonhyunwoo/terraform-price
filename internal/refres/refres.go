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

	"github.com/yoonhyunwoo/terraform-price/internal/funcs"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

const nonResourceRoot = "var local data module terraform path cwd each count self"

func isNonResourceRoot(name string) bool {
	for _, n := range strings.Fields(nonResourceRoot) {
		if name == n {
			return true
		}
	}
	return false
}

type RefResolver struct {
	resources map[string]*parser.Resource
	res       *resolver.Resolver

	// resolved caches attribute values: "aws_instance.a" -> {"instance_type": cty.StringVal("t3.micro")}
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
				cycle := append(append([]string{}, path...), addr)
				return fmt.Errorf("reference cycle: %s", strings.Join(cycle, " -> "))
			}
		}
		if visited[addr] {
			return nil
		}
		visited[addr] = true
		for _, dep := range r.deps(addr) {
			// copy: siblings must not share the appended backing array
			next := append(append([]string{}, path...), addr)
			if err := check(dep, next); err != nil {
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
// Uses expr.Variables() so refs nested in try()/coalesce()/conditionals/
// templates count as edges — a cycle hidden behind a try() fallback would
// otherwise price both sides with the fallback value (silent wrong answer).
func (r *RefResolver) deps(addr string) []string {
	res, ok := r.resources[addr]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, expr := range res.Exprs {
		for _, addr := range addrsInExpr(expr) {
			if !seen[addr] {
				seen[addr] = true
				out = append(out, addr)
			}
		}
	}
	return out
}

func addrsInExpr(expr hcl.Expression) []string {
	var out []string
	for _, t := range expr.Variables() {
		if len(t) < 2 {
			continue
		}
		root, ok := t[0].(hcl.TraverseRoot)
		if !ok {
			continue
		}
		switch root.Name {
		case "var", "local", "data", "module", "terraform", "path", "cwd", "each", "count", "self":
			continue
		}
		name, ok := t[1].(hcl.TraverseAttr)
		if !ok {
			continue
		}
		out = append(out, root.Name+"."+name.Name)
	}
	return out
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

// resolveExpr evaluates an expression. Direct scope traversals to other
// resources are resolved recursively; any other expression (function call,
// ternary, template, splat) is evaluated against a scope that includes the
// resource objects it references.
func (r *RefResolver) resolveExpr(expr hcl.Expression, fromAddr string) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		t := hcl.Traversal(ste.Traversal)
		if len(t) >= 2 {
			if root, ok := t[0].(hcl.TraverseRoot); ok && !isNonResourceRoot(root.Name) {
				if name, ok := t[1].(hcl.TraverseAttr); ok {
					targetAddr := root.Name + "." + name.Name
					if targetAddr == fromAddr {
						return cty.NilVal, false // self-reference
					}
					targetAttrs := r.ResolveResource(targetAddr)
					if targetAttrs == nil {
						return cty.NilVal, false
					}
					return navigateAttrs(targetAttrs, t[2:])
				}
			}
		}
	}
	// Non-traversal expression: build a scope with var/local values plus
	// on-demand resolved resource objects, then let HCL evaluate it.
	vars, locals := r.res.ValueMaps()
	ctx := funcs.Scope(vars, locals)
	need := map[string]map[string]cty.Value{}
	for _, addr := range addrsInExpr(expr) {
		if addr == fromAddr {
			continue // self-reference inside a nested expression
		}
		attrs := r.ResolveResource(addr)
		if attrs == nil {
			continue // unresolvable dep: leave its root out of scope
		}
		typ, name, _ := strings.Cut(addr, ".")
		if need[typ] == nil {
			need[typ] = map[string]cty.Value{}
		}
		need[typ][name] = cty.ObjectVal(attrs)
	}
	for typ, m := range need {
		ctx.Variables[typ] = cty.ObjectVal(m)
	}
	if v, diags := expr.Value(ctx); !diags.HasErrors() {
		return v, true
	}
	return cty.NilVal, false
}

// navigateAttrs walks the remaining traversal steps over a resolved attrs map.
// A numeric [0] on the object itself is count=1 semantics and resolves to the
// whole object.
func navigateAttrs(attrs map[string]cty.Value, steps []hcl.Traverser) (cty.Value, bool) {
	var val cty.Value
	started := false
	for i, step := range steps {
		switch s := step.(type) {
		case hcl.TraverseAttr:
			if i == 0 {
				v, ok := attrs[s.Name]
				if !ok || !v.IsKnown() {
					return cty.NilVal, false
				}
				val = v
				started = true
			} else {
				if !val.Type().IsObjectType() || !val.Type().HasAttribute(s.Name) {
					return cty.NilVal, false
				}
				val = val.GetAttr(s.Name)
			}
		case hcl.TraverseIndex:
			idx := s.Key
			if i == 0 {
				// x[0].attr over a single resource object
				if idx.Type() != cty.Number {
					return cty.NilVal, false
				}
				if n, _ := idx.AsBigFloat().Int64(); n != 0 {
					return cty.NilVal, false
				}
				val = cty.ObjectVal(attrs)
				started = true
				continue
			}
			switch {
			case idx.Type() == cty.String && val.Type().IsObjectType():
				if !val.Type().HasAttribute(idx.AsString()) {
					return cty.NilVal, false
				}
				val = val.GetAttr(idx.AsString())
			case idx.Type() == cty.Number && (val.Type().IsListType() || val.Type().IsTupleType() || val.Type().IsSetType()):
				i64, _ := idx.AsBigFloat().Int64()
				elems := val.AsValueSlice()
				if int(i64) < 0 || int(i64) >= len(elems) {
					return cty.NilVal, false
				}
				val = elems[i64]
			case idx.Type() == cty.Number && val.Type().IsObjectType():
				if n, _ := idx.AsBigFloat().Int64(); n != 0 {
					return cty.NilVal, false
				}
			default:
				return cty.NilVal, false
			}
		default:
			return cty.NilVal, false
		}
		if started && !val.IsKnown() {
			return cty.NilVal, false
		}
	}
	if !started {
		return cty.NilVal, false
	}
	return val, true
}

// AllResolved eagerly resolves every resource and returns the memo table.
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
