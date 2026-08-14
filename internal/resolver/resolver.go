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
	vars      map[string]cty.Value
	locals    map[string]cty.Value
	resources map[string]map[string]cty.Value
}

func NewResolver(dir string) *Resolver {
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

	// Variables with defaults first — locals reference them.
	r.loadVarDefaults(dir)
	// Collect locals blocks from ALL .tf files (not just locals.tf) —
	// real repos spread locals across many files.
	type localAttr struct {
		expr hcl.Expression
	}
	pending := map[string]localAttr{}
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
						pending[name] = localAttr{expr: attr.Expr}
					}
				}
			}
		}
	}
	// Iterative fixpoint: locals can reference other locals across files.
	for changed := true; changed && len(pending) > 0; {
		changed = false
		for name, la := range pending {
			if val, d := la.expr.Value(r.scope()); !d.HasErrors() {
				r.locals[name] = val
				delete(pending, name)
				changed = true
			}
		}
	}
	return r
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
			attrs, d := blk.Body.JustAttributes()
			if d.HasErrors() {
				continue
			}
			def, ok := attrs["default"]
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
	return funcs.Scope(r.vars, r.locals)
}

// ResolveExpr evaluates an expression with the full scope: var/local
// references, indexing, and function calls.
func (r *Resolver) ResolveExpr(expr hcl.Expression) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		return r.resolveTraversal(hcl.Traversal(ste.Traversal))
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
	obj := cty.ObjectVal(attrs)
	return navigate(rest, obj)
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


// SetResources registers parsed resources for cross-resource reference
// resolution (e.g. aws_instance.a.instance_type). Call after ParseDir.
func (r *Resolver) SetResources(resources map[string]map[string]cty.Value) {
	r.resources = resources
}

// ResolveResourceAttr resolves an attribute of another resource.
// Returns false if the resource or attribute is not resolvable.
func (r *Resolver) ResolveResourceAttr(typeName, name, attr string) (cty.Value, bool) {
	attrs, ok := r.resources[typeName+"."+name]
	if !ok {
		return cty.NilVal, false
	}
	v, ok := attrs[attr]
	if !ok || !v.IsKnown() || v.IsNull() {
		return cty.NilVal, false
	}
	return v, true
}

// ResolveLocal resolves a local value by name.
func (r *Resolver) ResolveLocal(name string) (cty.Value, bool) {
	v, ok := r.locals[name]
	if !ok || !v.IsKnown() || v.IsNull() {
		return cty.NilVal, false
	}
	return v, true
}
