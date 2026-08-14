package main

import (
	"context"
	"testing"
)

// for_each'd plain resources expand per key with `each` bound, so item
// attributes (instance_types = each.value.types) resolve; a conditional
// gating the collection with tuple-vs-empty-list branches must not block
// the local that feeds it.
func TestForEachResourceExpandsWithEach(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.tf": `
variable "groups" { default = null }
locals {
  groups = var.groups != null && length(var.groups) > 0 ? var.groups : []
}
module "eks" {
  source                   = "./mod"
  node_group_configurations = local.groups
}
`,
		"terraform.tfvars": `
groups = [
  { name = "a", types = ["t3.medium"], desired = 2 },
  { name = "b", types = ["t3.micro"],  desired = 3 }
]
`,
		"mod/main.tf": `
variable "node_group_configurations" { default = [] }
resource "aws_eks_node_group" "ng" {
  for_each = { for g in var.node_group_configurations : g.name => g }
  instance_types = each.value.types
  scaling_config {
    desired_size = each.value.desired
  }
}
`,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	a := itemOf(t, items, `module.eks.aws_eks_node_group.ng["a"]`)
	if a.Spec != "t3.medium × 2 (EKS node)" {
		t.Fatalf("group a = %+v, want t3.medium x2", a)
	}
	if want := 0.0496 * 730 * 2; a.Monthly != want {
		t.Fatalf("group a monthly = %v, want %v", a.Monthly, want)
	}
	b := itemOf(t, items, `module.eks.aws_eks_node_group.ng["b"]`)
	if b.Spec != "t3.micro × 3 (EKS node)" {
		t.Fatalf("group b = %+v, want t3.micro x3", b)
	}
}
