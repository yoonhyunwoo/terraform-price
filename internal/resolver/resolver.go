package resolver

import (
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
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
						if val, d := attr.Expr.Value(&hcl.EvalContext{}); !d.HasErrors() {
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

func (r *Resolver) ResolveExpr(expr hcl.Expression) (cty.Value, bool) {
	if ste, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		return r.resolveTraversal(hcl.Traversal(ste.Traversal))
	}
	if val, diags := expr.Value(&hcl.EvalContext{}); !diags.HasErrors() {
		return val, true
	}
	return cty.NilVal, false
}

func (r *Resolver) resolveTraversal(t hcl.Traversal) (cty.Value, bool) {
	if len(t) == 0 {
		return cty.NilVal, false
	}
	root, ok := t[0].(hcl.TraverseRoot)
	if !ok {
		return cty.NilVal, false
	}
	switch root.Name {
	case "var":
		if len(t) < 2 {
			return cty.NilVal, false
		}
		v, ok := r.vars[t[1].(hcl.TraverseAttr).Name]
		if !ok {
			return cty.NilVal, false
		}
		return navigate(t[2:], v)
	case "local":
		if len(t) < 2 {
			return cty.NilVal, false
		}
		v, ok := r.locals[t[1].(hcl.TraverseAttr).Name]
		if !ok {
			return cty.NilVal, false
		}
		return navigate(t[2:], v)
	}
	return cty.NilVal, false
}

func navigate(steps []hcl.Traverser, val cty.Value) (cty.Value, bool) {
	for _, s := range steps {
		attr, ok := s.(hcl.TraverseAttr)
		if !ok {
			return cty.NilVal, false
		}
		vm := val.AsValueMap()
		if vm == nil {
			return cty.NilVal, false
		}
		v, ok := vm[attr.Name]
		if !ok {
			return cty.NilVal, false
		}
		val = v
	}
	return val, true
}
