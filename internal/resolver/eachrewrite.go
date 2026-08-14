package resolver

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// rewriteEachValue replaces `each.value.X` traversals with null literals.
// Inside try(coalesce(each.value.X, var.X), ...) the null is skipped by
// coalesce, so homogeneous for_each maps (no per-item override) price via
// the var fallback. Per-item overrides are ignored — an acceptable
// approximation for cost estimation.
//
// Notes: mutates the shared parser AST in place — safe because idempotent
// (a null literal never re-rewrites). each.key is deliberately NOT rewritten;
// identifier interpolations like "${var.name}-${each.key}" stay unresolved
// (known-unknown, affects labels only).
func rewriteEachValue(expr hclsyntax.Expression) hclsyntax.Expression {
	switch e := expr.(type) {
	case *hclsyntax.ScopeTraversalExpr:
		if isEachValue(e.Traversal) {
			return &hclsyntax.LiteralValueExpr{Val: cty.NullVal(cty.DynamicPseudoType)}
		}
		return e
	case *hclsyntax.FunctionCallExpr:
		for i := range e.Args {
			e.Args[i] = rewriteEachValue(e.Args[i])
		}
		return e
	case *hclsyntax.ConditionalExpr:
		e.Condition = rewriteEachValue(e.Condition)
		e.TrueResult = rewriteEachValue(e.TrueResult)
		e.FalseResult = rewriteEachValue(e.FalseResult)
		return e
	case *hclsyntax.BinaryOpExpr:
		e.LHS = rewriteEachValue(e.LHS)
		e.RHS = rewriteEachValue(e.RHS)
		return e
	case *hclsyntax.UnaryOpExpr:
		e.Val = rewriteEachValue(e.Val)
		return e
	case *hclsyntax.ParenthesesExpr:
		e.Expression = rewriteEachValue(e.Expression)
		return e
	case *hclsyntax.TupleConsExpr:
		for i := range e.Exprs {
			e.Exprs[i] = rewriteEachValue(e.Exprs[i])
		}
		return e
	case *hclsyntax.ObjectConsExpr:
		for i := range e.Items {
			e.Items[i].KeyExpr = rewriteEachValue(e.Items[i].KeyExpr)
			e.Items[i].ValueExpr = rewriteEachValue(e.Items[i].ValueExpr)
		}
		return e
	case *hclsyntax.TemplateExpr:
		for i := range e.Parts {
			e.Parts[i] = rewriteEachValue(e.Parts[i])
		}
		return e
	case *hclsyntax.TemplateWrapExpr:
		e.Wrapped = rewriteEachValue(e.Wrapped)
		return e
	case *hclsyntax.IndexExpr:
		e.Collection = rewriteEachValue(e.Collection)
		e.Key = rewriteEachValue(e.Key)
		return e
	case *hclsyntax.SplatExpr:
		e.Source = rewriteEachValue(e.Source)
		return e
	case *hclsyntax.RelativeTraversalExpr:
		e.Source = rewriteEachValue(e.Source)
		return e
	default:
		return expr
	}
}

func isEachValue(t hcl.Traversal) bool {
	if len(t) < 2 {
		return false
	}
	root, ok := t[0].(hcl.TraverseRoot)
	if !ok || root.Name != "each" {
		return false
	}
	attr, ok := t[1].(hcl.TraverseAttr)
	return ok && attr.Name == "value"
}
