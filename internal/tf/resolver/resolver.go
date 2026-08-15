package resolver

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"

	"github.com/yoonhyunwoo/terraform-price/internal/tf/funcs"
	"github.com/yoonhyunwoo/terraform-price/internal/tf/parser"
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

// ForceModule settles a module's outputs now; the analyzer forces in
// registration order so later modules see earlier ones' outputs in scope().
func (r *Resolver) ForceModule(name string) {
	r.moduleOutputs(name)
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
		// nil (mid-build re-entry or unfetchable) stays retryable — the
		// build in flight may still produce outputs.
		e.done = e.vals != nil
	}
	return e.vals, e.vals != nil
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

	decls := collectVarDecls(dir)

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
						r.vars[name] = r.enterVar(name, val, decls)
					}
				}
			}
		}
	}

	for k, v := range inputs {
		r.vars[k] = r.enterVar(k, v, decls)
	}

	for name, d := range decls {
		if _, set := r.vars[name]; set || d.defaultExpr == nil {
			continue
		}
		if val, dg := d.defaultExpr.Value(&hcl.EvalContext{}); !dg.HasErrors() {
			r.vars[name] = r.enterVar(name, val, decls)
		}
	}
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
		if val, ok := r.resolveConditional(expr); ok {
			r.locals[name] = val
			delete(r.pendingLocs, name)
			changed = true
			continue
		}
		if val, d := expr.Value(r.scope()); !d.HasErrors() {
			r.locals[name] = val
			delete(r.pendingLocs, name)
			changed = true
		}
	}
	return changed
}

// normalizeVarVal coerces tuple values to lists, mirroring what a
// variable type constraint does in Terraform: conditionals like
// `var.groups != null ? var.groups : []` fail to unify tuples of
// different lengths but accept list vs empty tuple.
func normalizeVarVal(v cty.Value) cty.Value {
	if !v.IsKnown() || v.IsNull() {
		return v
	}
	if v.Type().IsTupleType() {
		if lv, err := convert.Convert(v, cty.List(cty.DynamicPseudoType)); err == nil {
			return lv
		}
	}
	return v
}

type varDecl struct {
	typeExpr    hcl.Expression
	defaultExpr hcl.Expression
}

func collectVarDecls(dir string) map[string]varDecl {
	decls := map[string]varDecl{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return decls
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
			// PartialContent (not JustAttributes): variable blocks may
			// contain nested validation blocks which JustAttributes rejects.
			vc, _, d := blk.Body.PartialContent(&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{{Name: "type"}, {Name: "default"}},
			})
			if d.HasErrors() {
				continue
			}
			dl := varDecl{}
			if a, ok := vc.Attributes["type"]; ok {
				dl.typeExpr = a.Expr
			}
			if a, ok := vc.Attributes["default"]; ok {
				dl.defaultExpr = a.Expr
			}
			decls[blk.Labels[0]] = dl
		}
	}
	return decls
}

// enterVar runs a value through the coercion gate; undeclared variables
// keep the legacy tuple→list normalization.
func (r *Resolver) enterVar(name string, v cty.Value, decls map[string]varDecl) cty.Value {
	if d, ok := decls[name]; ok {
		return coerceToDeclaredType(v, d.typeExpr)
	}
	return normalizeVarVal(v)
}

func (r *Resolver) VarString(name string) (string, bool) {
	return Str(r.vars[name])
}

func (r *Resolver) scope() *hcl.EvalContext {
	ctx := funcs.EvalScope(r.vars, r.locals)
	if r.resScope != nil {
		for t, v := range r.resScope {
			ctx.Variables[t] = v
		}
	}
	// A `module` object makes template-nested refs ("${module.m.out}-x") work.
	// Only settled modules are included — forcing here could build a sibling
	// mid-build and silently drop its inputs; the analyzer forces modules
	// explicitly instead.
	mods := make(map[string]cty.Value, len(r.modules))
	for name, e := range r.modules {
		if e.done && e.vals != nil {
			mods[name] = cty.ObjectVal(e.vals)
		}
	}
	if len(mods) > 0 {
		ctx.Variables["module"] = cty.ObjectVal(mods)
	}
	return ctx
}

// ResolveExpr evaluates an expression against vars, locals, and resources.
// each.value refs are neutralized (no per-item context); use
// ResolveExprWithEach when the caller knows the for_each item.
func (r *Resolver) ResolveExpr(expr hcl.Expression) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		return r.resolveTraversal(hcl.Traversal(ste.Traversal))
	}
	if _, bound := r.resScope["each"]; !bound {
		if sx, ok := expr.(hclsyntax.Expression); ok {
			expr = rewriteEachValue(sx)
		}
	}
	if val, ok := r.resolveConditional(expr); ok {
		return val, true
	}
	if val, diags := expr.Value(r.scope()); !diags.HasErrors() {
		return val, true
	}
	return cty.NilVal, false
}

// resolveConditional evaluates a conditional by branch — cty cannot unify
// tuples of different lengths (`cond ? var.groups : []`), which Terraform
// avoids by coercing values through variable type constraints. The
// not-taken branch is irrelevant to the value, so evaluating only the
// taken one sidesteps unification; failures fall back to native eval.
func (r *Resolver) resolveConditional(expr hcl.Expression) (cty.Value, bool) {
	ce, ok := expr.(*hclsyntax.ConditionalExpr)
	if !ok {
		return cty.NilVal, false
	}
	cond, diags := ce.Condition.Value(r.scope())
	if diags.HasErrors() || !cond.IsKnown() || cond.IsNull() {
		return cty.NilVal, false
	}
	b, err := convert.Convert(cond, cty.Bool)
	if err != nil || b.IsNull() || !b.IsKnown() {
		return cty.NilVal, false
	}
	branch := ce.FalseResult
	if b.True() {
		branch = ce.TrueResult
	}
	if v, ok := r.resolveConditional(branch); ok {
		return v, true
	}
	if v, diags := branch.Value(r.scope()); !diags.HasErrors() {
		return v, true
	}
	return cty.NilVal, false
}

// SetResourceEach binds `each` while one for_each'd resource item is
// being resolved; ClearResourceEach restores the unbound state.
func (r *Resolver) SetResourceEach(key, value cty.Value) {
	if r.resScope == nil {
		r.resScope = map[string]cty.Value{}
	}
	r.resScope["each"] = cty.ObjectVal(map[string]cty.Value{"key": key, "value": value})
}

func (r *Resolver) ClearResourceEach() {
	delete(r.resScope, "each")
}

// ResolveExprWithEach evaluates an expression with `each` bound to one
// for_each item (key/value), restoring the previous binding afterwards.
func (r *Resolver) ResolveExprWithEach(expr hcl.Expression, key, value cty.Value) (cty.Value, bool) {
	if r.resScope == nil {
		r.resScope = map[string]cty.Value{}
	}
	prev, had := r.resScope["each"]
	r.resScope["each"] = cty.ObjectVal(map[string]cty.Value{"key": key, "value": value})
	v, ok := r.resolveBound(expr)
	if had {
		r.resScope["each"] = prev
	} else {
		delete(r.resScope, "each")
	}
	return v, ok
}

func (r *Resolver) resolveBound(expr hcl.Expression) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		return r.resolveTraversal(hcl.Traversal(ste.Traversal))
	}
	return r.ResolveExprRaw(expr)
}

// ResolveExprRaw evaluates without the each.value rewrite.
func (r *Resolver) ResolveExprRaw(expr hcl.Expression) (cty.Value, bool) {
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
	case "each":
		if v, ok := r.resScope["each"]; ok {
			return navigate(t[1:], v)
		}
		return cty.NilVal, false
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
