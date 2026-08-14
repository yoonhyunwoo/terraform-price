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
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
)

type Resolver struct {
	vars        map[string]cty.Value
	locals      map[string]cty.Value
	resources   map[string]map[string]cty.Value
	pendingLocs map[string]hcl.Expression
	resScope    map[string]cty.Value // resource TYPE → ObjectVal{name → attrs}
	modules     map[string]*moduleEntry
}

type moduleEntry struct {
	fn      func() map[string]cty.Value
	vals    map[string]cty.Value
	done    bool
	running bool
}

// RegisterModule registers a lazily evaluated output thunk for
// module.<name>.<output> references; first reference forces and memoizes it.
func (r *Resolver) RegisterModule(name string, outputs func() map[string]cty.Value) {
	if r.modules == nil {
		r.modules = map[string]*moduleEntry{}
	}
	r.modules[name] = &moduleEntry{fn: outputs}
}

func (r *Resolver) moduleOutputs(name string) (map[string]cty.Value, bool) {
	e, ok := r.modules[name]
	if !ok {
		return nil, false
	}
	if !e.done {
		if e.running {
			return nil, false
		}
		e.running = true
		e.vals = e.fn()
		e.running = false
		e.done = true
	}
	if e.vals == nil {
		return nil, false
	}
	return e.vals, true
}

// NewResolver builds a resolver over a Terraform directory.
func NewResolver(dir string) *Resolver {
	return NewResolverWithVars(dir, nil)
}

// NewResolverWithVars layers module-call input values over the directory's
// tfvars; the child's variable defaults backstop unset inputs.
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

	for k, v := range inputs {
		r.vars[k] = v
	}

	r.loadVarDefaults(dir)
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
	for r.RetryLocals() {
	}
	return r
}

// RetryLocals re-attempts pending locals; true when any new local resolved.
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
	return Str(r.vars[name])
}

func (r *Resolver) scope() *hcl.EvalContext {
	ctx := funcs.Scope(r.vars, r.locals)
	if r.resScope != nil {
		for t, v := range r.resScope {
			ctx.Variables[t] = v
		}
	}
	// A `module` object makes template-nested refs ("${module.m.out}-x") work.
	if len(r.modules) > 0 {
		mods := make(map[string]cty.Value, len(r.modules))
		for name := range r.modules {
			if outs, ok := r.moduleOutputs(name); ok {
				mods[name] = cty.ObjectVal(outs)
			}
		}
		if len(mods) > 0 {
			ctx.Variables["module"] = cty.ObjectVal(mods)
		}
	}
	return ctx
}

// ResolveExpr evaluates an expression against vars, locals, and resources.
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
	root, ok := parser.RootName(t)
	if !ok {
		return cty.NilVal, false
	}
	switch root {
	case "var":
		return navigate(t[1:], cty.ObjectVal(r.vars))
	case "local":
		return navigate(t[1:], cty.ObjectVal(r.locals))
	case "module":
		if len(t) >= 2 {
			if name, ok := t[1].(hcl.TraverseAttr); ok {
				if outs, ok := r.moduleOutputs(name.Name); ok {
					return navigate(t[2:], cty.ObjectVal(outs))
				}
			}
		}
		return cty.NilVal, false
	}
	if parser.IsScopeRoot(root) {
		// Known-unknowns by design: computed attrs, data.*, each.key, self.
		return cty.NilVal, false
	}
	if addr, rest, ok := parser.SplitRef(t); ok {
		typ, name, _ := parser.SplitAddr(addr)
		return r.resolveResourceTraversal(typ, name, rest)
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
	// Parser stores nested-block attrs flat (root_block_device.volume_type);
	// explode so indexed navigation works.
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

func explodeAttrs(flat map[string]cty.Value) map[string]cty.Value {
	out := map[string]cty.Value{}
	for k, v := range flat {
		parts := strings.SplitN(k, ".", 2)
		if len(parts) == 1 {
			out[k] = v
			continue
		}
		if sub, ok := out[parts[0]]; ok && sub.Type().IsObjectType() {
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
			case idx.Type() == cty.Number && (val.Type().IsListType() || val.Type().IsTupleType()):
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

// SetResources registers resolved attrs and builds the resource-type scope
// (aws_launch_template.default[*].id evaluates natively).
func (r *Resolver) SetResources(resources map[string]map[string]cty.Value, countBased map[string]bool) {
	r.resources = resources
	byType := map[string]map[string]cty.Value{}
	for addr, attrs := range resources {
		typ, name, ok := parser.SplitAddr(addr)
		if !ok {
			continue
		}
		nameObjs, ok := byType[typ]
		if !ok {
			nameObjs = map[string]cty.Value{}
			byType[typ] = nameObjs
		}
		obj := cty.ObjectVal(explodeAttrs(attrs))
		if countBased[addr] {
			// count/for_each resource: ref as a collection (a[*].attr, a[0])
			obj = cty.TupleVal([]cty.Value{obj})
		}
		nameObjs[name] = obj
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

// ValueMaps exposes resolved vars and locals for callers building their
// own EvalContext (e.g. refres).
func (r *Resolver) ValueMaps() (vars, locals map[string]cty.Value) {
	return r.vars, r.locals
}

// LocalExpr returns a pending local's unevaluated expression for
// transitive resource discovery.
func (r *Resolver) LocalExpr(name string) (hcl.Expression, bool) {
	e, ok := r.pendingLocs[name]
	return e, ok
}
