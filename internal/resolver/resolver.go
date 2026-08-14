package resolver

import (
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/yoonhyunwoo/terraform-price/internal/funcs"
)

type Resolver struct {
	vars   map[string]cty.Value
	locals map[string]cty.Value
}

func NewResolver(dir string) *Resolver {
	r := &Resolver{vars: map[string]cty.Value{}, locals: map[string]cty.Value{}}
	parser := hclparse.NewParser()

	if fv, diags := parser.ParseHCLFile(filepath.Join(dir, "terraform.tfvars")); !diags.HasErrors() && fv != nil {
		if attrs, d := fv.Body.JustAttributes(); !d.HasErrors() {
			for name, attr := range attrs {
				if val, d := attr.Expr.Value(&hcl.EvalContext{}); !d.HasErrors() {
					r.vars[name] = val
				}
			}
		}
	}

	if fv, diags := parser.ParseHCLFile(filepath.Join(dir, "locals.tf")); !diags.HasErrors() && fv != nil {
		content, _, d := fv.Body.PartialContent(&hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{{Type: "locals"}},
		})
		if !d.HasErrors() {
			for _, blk := range content.Blocks {
				if attrs, d := blk.Body.JustAttributes(); !d.HasErrors() {
					for name, attr := range attrs {
						if val, d := attr.Expr.Value(r.scope()); !d.HasErrors() {
							r.locals[name] = val
						}
					}
				}
			}
		}
	}
	return r
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
	}
	return cty.NilVal, false
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

