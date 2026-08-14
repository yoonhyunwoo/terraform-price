package main

import (
	"context"
	"testing"
)

const modOutMain = `
variable "instance_type" {
  default = "t3.micro"
}
resource "aws_instance" "x" {
  instance_type = var.instance_type
}
output "it" {
  value = var.instance_type
}
`

// Parent expressions resolve module.m.out by chaining lazily into the
// child scope's outputs (direct reference).
func TestModuleOutputReference(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "m" {
  source        = "./mod"
  instance_type = "t3.medium"
}
resource "aws_instance" "x" {
  instance_type = module.m.it
}
`,
		"mod/main.tf": modOutMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "aws_instance.x")
	if it.Spec != "t3.medium" {
		t.Fatalf("instance via module output = %+v, want t3.medium", it)
	}
}

// A local referencing a module output must resolve after the module
// scopes are registered (the second fixpoint pass).
func TestModuleOutputThroughLocal(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "m" {
  source        = "./mod"
  instance_type = "t3.medium"
}
locals {
  it = module.m.it
}
resource "aws_instance" "x" {
  instance_type = local.it
}
`,
		"mod/main.tf": modOutMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "aws_instance.x")
	if it.Spec != "t3.medium" {
		t.Fatalf("instance via local→module output = %+v, want t3.medium", it)
	}
}

// Two modules whose inputs cross-reference each other's outputs form a
// cycle; the running guard must degrade both to unresolved without
// hanging, and the child resources still price via their defaults.
func TestModuleOutputCycleDegrades(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "a" {
  source        = "./mod"
  instance_type = module.b.it
}
module "b" {
  source        = "./mod"
  instance_type = module.a.it
}
resource "aws_instance" "root" {
  instance_type = module.a.it
}
`,
		"mod/main.tf": modOutMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	// The cross-reference module.b.it inside a's inputs is cut off while
	// a is still building (cycle guard); b falls back to its variable
	// default, and a then receives b's default-derived output. Nothing
	// hangs; everything prices on the default.
	it := itemOf(t, items, "module.a.aws_instance.x")
	if it.Spec != "t3.micro" {
		t.Fatalf("cycle fallback instance = %+v, want t3.micro default", it)
	}
	itb := itemOf(t, items, "module.b.aws_instance.x")
	if itb.Spec != "t3.micro" {
		t.Fatalf("cycle fallback b instance = %+v, want t3.micro default", itb)
	}
	root1 := itemOf(t, items, "aws_instance.root")
	if root1.Spec != "t3.micro" || root1.Unresolved != "" {
		t.Fatalf("root via cyclic module output = %+v, want priced t3.micro", root1)
	}
}
