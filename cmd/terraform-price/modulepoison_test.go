package main

import (
	"context"
	"testing"
)

// A module input that is a template/conditional expr evaluates via
// scope(), which builds the `module` object and can observe a module
// mid-build. A module whose input references another module's output must
// still see the finished outputs (no permanently memoized nil).
func TestModuleOutputSurvivesMidBuildScope(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
variable "x" {
  default = "t3.medium"
}
module "m1" {
  source = "./tpl"
  it     = "${var.x}"
}
module "m2" {
  source = "./use"
  it     = module.m1.o
}
`,
		"tpl/main.tf": `
variable "it" {
  default = "t3.micro"
}
output "o" {
  value = var.it
}
`,
		"use/main.tf": `
variable "it" {
  default = "t3.micro"
}
resource "aws_instance" "x" {
  instance_type = var.it
}
`,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "module.m2.aws_instance.x")
	if it.Spec != "t3.medium" {
		t.Fatalf("m2 instance = %+v, want t3.medium via m1's output (got default → nil was memoized)", it)
	}
}
