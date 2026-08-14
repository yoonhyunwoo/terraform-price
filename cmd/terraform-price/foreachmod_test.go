package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A for_each'd module block expands per key; inputs see the bound each
// (direct refs and try() fallbacks to defaults); a for-expression
// for_each with an `if` guard over length(object) resolves.
func TestForEachModuleExpandsWithEach(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.tf": `
variable "outpost_config" {
  type    = any
  default = {}
}
variable "groups" { default = null }
variable "defaults" { default = null }
locals {
  enabled = length(var.outpost_config) == 0
}
module "ng" {
  for_each = { for k, v in var.groups : k => v if local.enabled }
  source   = "./mod"
  it       = try(each.value.it, var.defaults.it, "t3.micro")
  desired  = each.value.desired
}
`,
		"terraform.tfvars": `
groups = {
  a = { desired = 2 }
  b = { desired = 3, it = "t3.medium" }
}
defaults = { it = "t3.micro" }
`,
		"mod/main.tf": `
variable "it" { default = "t3.micro" }
variable "desired" { default = 1 }
resource "aws_instance" "x" {
  count         = var.desired
  instance_type = var.it
}
`,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	xa := itemOf(t, items, `module.ng["a"].aws_instance.x`)
	if xa.Spec != "t3.micro × 2" {
		t.Fatalf("group a spec = %q, want t3.micro x2 via defaults fallback", xa.Spec)
	}
	xb := itemOf(t, items, `module.ng["b"].aws_instance.x`)
	if xb.Spec != "t3.medium × 3" {
		t.Fatalf("group b spec = %q, want t3.medium via each.value override", xb.Spec)
	}
	if want := 0.0496 * 730 * 3; xb.Monthly != want {
		t.Fatalf("group b monthly = %v, want %v (t3.medium x desired 3)", xb.Monthly, want)
	}
	if want := 0.0124 * 730 * 2; xa.Monthly != want {
		t.Fatalf("group a monthly = %v, want %v (t3.micro x desired 2)", xa.Monthly, want)
	}
}
