package resolver

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/yoonhyunwoo/terraform-price/internal/funcs"
)

type Resolver struct {
	vars        map[string]cty.Value
	locals      map[string]cty.Value
	resources   map[string]map[string]cty.Value
	pendingLocs map[string]hcl.Expression
	resScope    map[string]cty.Value // resource TYPE → ObjectVal{name → attrs}
}

// NewResolver builds a resolver over a Terraform directory.
func NewResolver(dir string) *Resolver {
	return NewResolverWithVars(dir, nil)
}

// NewResolverWithVars builds a resolver with externally supplied variable
// values (module inputs) layered on top of the directory's own tfvars —
// module instantiation. Inputs win over tfvars, and the child's variable
// defaults still backstop anything unset.
func NewResolverWithVars(dir string, inputs map[string]cty.Value) *Resolver {
	r := &Resolver{vars: map[string]cty.Value{}, locals: map[string]cty.Value{}}
	parser := hclparse.NewParser()

	// tfvars: terraform.tfvars first, then *.auto.tfvars (later wins).
	for _, pattern := range []string{"terraform.tfvars", "*.auto.tfvars"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, path := range matches {
			fv, diags := hclparse.NewParser().ParseHCLFile(path)
			if diags.HasErrors() || fv == nil {
				continue
			}
			if attrs, d := fv.Body.JustAttributes(); !d.HasErrors() {
				for name, attr := range attrs {
					if val, d := attr.Expr.Value(&hcl.EvalContext{}); !d.HasErrors() {
						r.vars[name] = val
					}
				}
			}
		}
	}

	// Module inputs layer on top of the directory's tfvars — a module
	// instance's caller-supplied values win for that instantiation.
	for k, v := range inputs {
		r.vars[k] = v
	}

	// Variables with defaults first — locals reference them.
	r.loadVarDefaults(dir)
	// Collect locals blocks from ALL .tf files (not just locals.tf) —
	// real repos spread locals across many files.
	r.pendingLocs = map[string]hcl.Expression{}
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
				continue
			}
			fv, diags := parser.ParseHCLFile(filepath.Join(dir, e.Name()))
			if diags.HasErrors() || fv == nil {
				continue
			}
			content, _, d := fv.Body.PartialContent(&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{{Type: "locals"}},
			})
			if d.HasErrors() {
				continue
			}
			for _, blk := range content.Blocks {
				if attrs, d := blk.Body.JustAttributes(); !d.HasErrors() {
					for name, attr := range attrs {
						r.pendingLocs[name] = attr.Expr
					}
				}
			}
		}
	}
	// Iterative fixpoint: locals can reference other locals across files.
	// Locals referencing resources stay pending until SetResources +
	// RetryLocals (analyze iterates both to a fixpoint).
	for r.RetryLocals() {
		// iterate: a pass may unlock locals that referenced other locals
		// resolved later in the same pass (map order is random)
	}
	return r
}

// RetryLocals re-attempts pending locals (e.g. those referencing resources
// after SetResources). Returns true if any new local resolved.
func (r *Resolver) RetryLocals() bool {
	changed := false
	for name, expr := range r.pendingLocs {
		if val, d := expr.Value(r.scope()); !d.HasErrors() {
			r.locals[name] = val
			delete(r.pendingLocs, name)
			changed = true
		}
	}
	return changed
}

// loadVarDefaults reads variable blocks' default values so unset vars
// fall back to their declared defaults (common in real repos).
func (r *Resolver) loadVarDefaults(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		fv, diags := hclparse.NewParser().ParseHCLFile(filepath.Join(dir, e.Name()))
		if diags.HasErrors() || fv == nil {
			continue
		}
		content, _, d := fv.Body.PartialContent(&hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{{Type: "variable", LabelNames: []string{"name"}}},
		})
		if d.HasErrors() {
			continue
		}
		for _, blk := range content.Blocks {
			name := blk.Labels[0]
			if _, set := r.vars[name]; set {
				continue
			}
			// PartialContent (not JustAttributes): variable blocks may
			// contain nested validation blocks which JustAttributes rejects.
			vc, _, d := blk.Body.PartialContent(&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{{Name: "default"}},
			})
			if d.HasErrors() {
				continue
			}
			def, ok := vc.Attributes["default"]
			if !ok {
				continue
			}
			if val, d := def.Expr.Value(&hcl.EvalContext{}); !d.HasErrors() {
				r.vars[name] = val
			}
		}
	}
}

func (r *Resolver) VarString(name string) (string, bool) {
	v, ok := r.vars[name]
	if !ok || !v.IsKnown() || v.IsNull() || v.Type() != cty.String {
		return "", false
	}
	return v.AsString(), true
}

// scope returns the evaluation context with var/local objects and the
// standard function table (toset, length, concat, ...).
func (r *Resolver) scope() *hcl.EvalContext {
	ctx := funcs.Scope(r.vars, r.locals)
	if r.resScope != nil {
		for t, v := range r.resScope {
			ctx.Variables[t] = v
		}
	}
	return ctx
}

// ResolveExpr evaluates an expression with the full scope: var/local
// references, indexing, and function calls.
func (r *Resolver) ResolveExpr(expr hcl.Expression) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		return r.resolveTraversal(hcl.Traversal(ste.Traversal))
	}
	if sx, ok := expr.(hclsyntax.Expression); ok {
		expr = rewriteEachValue(sx)
	}
	if val, diags := expr.Value(r.scope()); !diags.HasErrors() {
		return val, true
	}
	return cty.NilVal, false
}

func (r *Resolver) resolveTraversal(t hcl.Traversal) (cty.Value, bool) {
	if len(t) == 0 {
		return cty.NilVal, false
	}
	root, ok := rootName(t[0])
	if !ok {
		return cty.NilVal, false
	}
	switch root {
	case "var":
		return navigate(t[1:], cty.ObjectVal(r.vars))
	case "local":
		return navigate(t[1:], cty.ObjectVal(r.locals))
	// Known-unknowns, unresolved by design and reported as such — never guessed:
	// provider-computed attrs (id/arn/latest_version), data.*, module.*, each.key.
	case "data", "module", "terraform", "path", "cwd", "each", "count":
		return cty.NilVal, false
	}
	// Resource reference: aws_instance.a.instance_type
	if len(t) >= 2 {
		if name, ok := t[1].(hcl.TraverseAttr); ok {
			return r.resolveResourceTraversal(root, name.Name, t[2:])
		}
	}
	return cty.NilVal, false
}

func (r *Resolver) resolveResourceTraversal(root string, name string, rest []hcl.Traverser) (cty.Value, bool) {
	if r.resources == nil {
		return cty.NilVal, false
	}
	attrs, ok := r.resources[root+"."+name]
	if !ok {
		return cty.NilVal, false
	}
	// Parser stores nested-block attrs as flat dotted keys
	// (root_block_device.volume_type). Explode into a nested object so
	// .root_block_device[0].volume_type navigates naturally.
	obj := cty.ObjectVal(explodeAttrs(attrs))
	// Drop numeric index steps: resource[0] on a count-based resource (and
	// single-occurrence nested blocks) carry the same config attrs at any
	// index — count semantics, not value lookup.
	filtered := rest[:0]
	for _, s := range rest {
		if idx, ok := s.(hcl.TraverseIndex); ok && idx.Key.Type() == cty.Number {
			continue
		}
		filtered = append(filtered, s)
	}
	return navigate(filtered, obj)
}

// explodeAttrs converts {"a.b": v} into {"a": {"b": v}} recursively.
func explodeAttrs(flat map[string]cty.Value) map[string]cty.Value {
	out := map[string]cty.Value{}
	for k, v := range flat {
		parts := strings.SplitN(k, ".", 2)
		if len(parts) == 1 {
			out[k] = v
			continue
		}
		if sub, ok := out[parts[0]]; ok && sub.Type().IsObjectType() {
			// merge into existing nested object
			merged := sub.AsValueMap()
			nested := explodeNested(parts[1], v)
			for nk, nv := range nested {
				if existing, exists := merged[nk]; exists && existing.Type().IsObjectType() && nv.Type().IsObjectType() {
					for k2, v2 := range nv.AsValueMap() {
						merged[nk] = setAttr(merged[nk], k2, v2)
					}
				} else {
					merged[nk] = nv
				}
			}
			out[parts[0]] = cty.ObjectVal(merged)
		} else {
			out[parts[0]] = cty.ObjectVal(explodeNested(parts[1], v))
		}
	}
	return out
}

func explodeNested(rest string, v cty.Value) map[string]cty.Value {
	parts := strings.SplitN(rest, ".", 2)
	if len(parts) == 1 {
		return map[string]cty.Value{parts[0]: v}
	}
	return map[string]cty.Value{parts[0]: cty.ObjectVal(explodeNested(parts[1], v))}
}

func setAttr(obj cty.Value, key string, v cty.Value) cty.Value {
	m := obj.AsValueMap()
	m[key] = v
	return cty.ObjectVal(m)
}
func rootName(tr hcl.Traverser) (string, bool) {
	if root, ok := tr.(hcl.TraverseRoot); ok {
		return root.Name, true
	}
	return "", false
}

func navigate(steps []hcl.Traverser, val cty.Value) (cty.Value, bool) {
	for _, step := range steps {
		switch s := step.(type) {
		case hcl.TraverseAttr:
			if !val.Type().IsObjectType() || !val.Type().HasAttribute(s.Name) {
				return cty.NilVal, false
			}
			attr := val.GetAttr(s.Name)
			if !attr.IsKnown() {
				return cty.NilVal, false
			}
			val = attr
		case hcl.TraverseIndex:
			idx := s.Key
			switch {
			case idx.Type() == cty.String && val.Type().IsObjectType():
				if !val.Type().HasAttribute(idx.AsString()) {
					return cty.NilVal, false
				}
				attr := val.GetAttr(idx.AsString())
				if !attr.IsKnown() {
					return cty.NilVal, false
				}
				val = attr
			case idx.Type() == cty.Number && val.Type().IsListType():
				i, _ := idx.AsBigFloat().Int64()
				list := val.AsValueSlice()
				if i < 0 || int(i) >= len(list) {
					return cty.NilVal, false
				}
				val = list[i]
			case idx.Type() == cty.Number && val.Type().IsTupleType():
				i, _ := idx.AsBigFloat().Int64()
				list := val.AsValueSlice()
				if i < 0 || int(i) >= len(list) {
					return cty.NilVal, false
				}
				val = list[i]
			default:
				return cty.NilVal, false
			}
		default:
			return cty.NilVal, false
		}
	}
	if !val.IsKnown() {
		return cty.NilVal, false
	}
	return val, true
}

// SetResources registers resolved resource attributes and builds the
// resource TYPE-rooted evaluation scope (aws_launch_template = {this = {...}})
// so expressions like one(aws_launch_template.default[*].id) evaluate natively.
func (r *Resolver) SetResources(resources map[string]map[string]cty.Value, countBased map[string]bool) {
	r.resources = resources
	byType := map[string]map[string]cty.Value{}
	for addr, attrs := range resources {
		parts := strings.SplitN(addr, ".", 2)
		if len(parts) != 2 {
			continue
		}
		nameObjs, ok := byType[parts[0]]
		if !ok {
			nameObjs = map[string]cty.Value{}
			byType[parts[0]] = nameObjs
		}
		obj := cty.ObjectVal(explodeAttrs(attrs))
		if countBased[addr] {
			// count/for_each resource: ref as a collection (a[*].attr, a[0])
			obj = cty.TupleVal([]cty.Value{obj})
		}
		nameObjs[parts[1]] = obj
	}
	scope := make(map[string]cty.Value, len(byType))
	for t, names := range byType {
		scope[t] = cty.ObjectVal(names)
	}
	r.resScope = scope
}

// ResolveLocal resolves a local value by name.
func (r *Resolver) ResolveLocal(name string) (cty.Value, bool) {
	v, ok := r.locals[name]
	if !ok || !v.IsKnown() || v.IsNull() {
		return cty.NilVal, false
	}
	return v, true
}

// ValueMaps exposes the resolved var and local values so callers building
// their own EvalContext (e.g. refres resource-expression evaluation) can
// seed it with the same base scope.
func (r *Resolver) ValueMaps() (vars, locals map[string]cty.Value) {
	return r.vars, r.locals
}

// LocalExpr returns the unevaluated expression of a local that has not
// resolved (typically because it references computed resource attrs).
// Used for transitive resource discovery without value resolution.
func (r *Resolver) LocalExpr(name string) (hcl.Expression, bool) {
	e, ok := r.pendingLocs[name]
	return e, ok
}
