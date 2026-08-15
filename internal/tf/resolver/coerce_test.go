package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// The coercion gate: every entry (tfvars, module input, default) converts to
// the declared type constraint before evaluation, so conditionals like
// `cond ? var.groups : []` unify (list vs empty tuple) instead of failing on
// heterogeneous tuples — the hrp/monster class of defect.
func TestCoerceGateDeclaredType(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "variables.tf"), []byte(`
variable "groups" {
  type = list(object({
    name   = string
    itype  = string
    volume = number
  }))
}
variable "region" {
  type    = string
  default = 5
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(`
groups = [
  {name = "a", itype = "r7i.2xlarge", volume = 100},
  {name = "b", itype = "m5.large", volume = 50},
]
`), 0o644)
	os.WriteFile(filepath.Join(dir, "locals.tf"), []byte(`
locals {
  chosen = var.groups != null ? var.groups : []
}
`), 0o644)

	r := NewResolver(dir)
	g, ok := r.ResolveLocal("chosen")
	if !ok {
		t.Fatal("conditional over typed var did not resolve (tuple unify failure)")
	}
	if !g.Type().IsListType() {
		t.Fatalf("groups = %s, want list type", g.Type().FriendlyName())
	}
	if n := g.LengthInt(); n != 2 {
		t.Fatalf("groups len = %d, want 2", n)
	}
	// default coerced to declared type too (number → string)
	reg, ok := r.VarString("region")
	if !ok || reg != "5" {
		t.Fatalf("region = %q %v, want coerced \"5\"", reg, ok)
	}
}

// Module inputs pass the same gate: NewResolverWithVars values convert to
// the child's declared types.
func TestCoerceGateModuleInputs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "variables.tf"), []byte(`
variable "instances" {
  type = map(object({ itype = string }))
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "locals.tf"), []byte(`
locals {
  a = var.instances["a"].itype
}
`), 0o644)
	inputs := map[string]cty.Value{
		"instances": cty.ObjectVal(map[string]cty.Value{
			"a": cty.ObjectVal(map[string]cty.Value{"itype": cty.StringVal("t3.micro")}),
			"b": cty.ObjectVal(map[string]cty.Value{"itype": cty.StringVal("t3.medium")}),
		}),
	}
	r := NewResolverWithVars(dir, inputs)
	v, ok := r.ResolveLocal("a")
	if !ok {
		t.Fatal("input traversal did not resolve through the coercion gate")
	}
	if got := v.AsString(); got != "t3.micro" {
		t.Fatalf("a = %q, want t3.micro (object→map coercion)", got)
	}
}
