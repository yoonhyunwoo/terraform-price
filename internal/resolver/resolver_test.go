package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

func newResolver(t *testing.T, tfvars string) *Resolver {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(tfvars), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewResolver(dir)
}

func parseExpr(t *testing.T, src string) hcl.Expression {
	t.Helper()
	e, diags := hclsyntax.ParseExpression([]byte(src), "test.tf", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse %q: %v", src, diags)
	}
	return e
}

func TestResolveIndexedVar(t *testing.T) {
	r := newResolver(t, `m = { a = "x", b = "y" }`)
	v, ok := r.ResolveExpr(parseExpr(t, `var.m["b"]`))
	if !ok || v.AsString() != "y" {
		t.Fatalf(`var.m["b"]: want y/true, got %v/%v`, v, ok)
	}
}

func TestResolveIndexedVarMissingKey(t *testing.T) {
	r := newResolver(t, `m = { a = "x" }`)
	if _, ok := r.ResolveExpr(parseExpr(t, `var.m["zzz"]`)); ok {
		t.Fatal(`var.m["zzz"]: want not-found`)
	}
}

func TestResolveListIndex(t *testing.T) {
	r := newResolver(t, `list = ["p", "q"]`)
	v, ok := r.ResolveExpr(parseExpr(t, `var.list[1]`))
	if !ok || v.AsString() != "q" {
		t.Fatalf("var.list[1]: want q/true, got %v/%v", v, ok)
	}
	if _, ok := r.ResolveExpr(parseExpr(t, `var.list[5]`)); ok {
		t.Fatal("var.list[5]: want out-of-range not-found")
	}
}

// Regression: var["name"] used to panic on an unchecked TraverseAttr assertion.
func TestResolveRootIndexNoPanic(t *testing.T) {
	r := newResolver(t, `region = "ap-northeast-2"`)
	v, ok := r.ResolveExpr(parseExpr(t, `var["region"]`))
	if !ok || v.AsString() != "ap-northeast-2" {
		t.Fatalf(`var["region"]: want ap-northeast-2/true, got %v/%v`, v, ok)
	}
	if _, ok := r.ResolveExpr(parseExpr(t, `var[0]`)); ok {
		t.Fatal("var[0]: numeric root index must not resolve")
	}
}

// Regression: traversing into a null var used to panic inside cty.
func TestNullVarTraversalNoPanic(t *testing.T) {
	r := newResolver(t, "it = null\nm = { a = null }")
	for _, src := range []string{`var.it.dev`, `var.m.a.b`, `var.m["k"]`} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s: panic: %v", src, p)
				}
			}()
			if _, ok := r.ResolveExpr(parseExpr(t, src)); ok {
				t.Errorf("%s: null traversal must not resolve", src)
			}
		}()
	}
}
