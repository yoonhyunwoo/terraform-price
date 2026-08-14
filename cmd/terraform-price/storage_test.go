package main

import (
	"context"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/output"
)

// Attached storage prices as its own row on the resource: instance root
// blocks multiply by count, EKS node groups fall back to the API-default
// 20GB root volume, and RDS allocated storage bills per GB-month.
func TestAttachedStoragePriced(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.tf": `
resource "aws_instance" "x" {
  count         = 2
  instance_type = "t3.micro"
  root_block_device {
    size = 30
  }
}
resource "aws_eks_node_group" "ng" {
  instance_types = ["t3.micro"]
  scaling_config {
    desired_size = 3
  }
}
resource "aws_db_instance" "db" {
  instance_class     = "db.t3.micro"
  engine             = "postgres"
  allocated_storage  = 100
}
`,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	root1 := itemWithSpec(t, items, "aws_instance.x", "gp3 30GB (root)")
	if !approx(root1.Monthly, 0.08*30*2) {
		t.Fatalf("instance root = %+v, want 0.08*30*2", root1)
	}
	ng := itemWithSpec(t, items, "aws_eks_node_group.ng", "gp3 20GB (node root, API default)")
	if !approx(ng.Monthly, 0.08*20*3) {
		t.Fatalf("node root = %+v, want 0.08*20*3", ng)
	}
	db := itemWithSpec(t, items, "aws_db_instance.db", "gp3 storage 100GB")
	if !approx(db.Monthly, 0.131*100) {
		t.Fatalf("rds storage = %+v, want 0.131*100", db)
	}
}

func approx(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func itemWithSpec(t *testing.T, items []output.CostItem, addr, spec string) output.CostItem {
	t.Helper()
	for _, it := range items {
		if it.Addr == addr && it.Spec == spec {
			return it
		}
	}
	t.Fatalf("addr %s spec %q not found", addr, spec)
	return output.CostItem{}
}
