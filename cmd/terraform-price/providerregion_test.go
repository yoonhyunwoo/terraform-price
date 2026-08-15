package main

import (
	"context"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

// Region resolution: the default aws provider block beats the aws_region var
// heuristic, and resources pinned to an aliased provider (provider = aws.ue2)
// price at that alias's region.
func TestProviderBlockRegions(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.tf": `
provider "aws" {
  region = "eu-central-1"
}
provider "aws" "ue2" {
  region = "us-east-2"
}
variable "aws_region" {
  default = "ap-northeast-2"
}
resource "aws_eks_cluster" "base" {
  version = "1.34"
}
resource "aws_eks_cluster" "ue2cluster" {
  version  = "1.34"
  provider = aws.ue2
}
`,
	})
	stub := &capturePricer{}
	items, err := analyze(context.Background(), stub, root)
	if err != nil {
		t.Fatal(err)
	}
	byAddr := map[string]string{}
	for _, it := range items {
		byAddr[it.Addr] = ""
	}
	for _, want := range []string{"aws_eks_cluster.base", "aws_eks_cluster.ue2cluster"} {
		if _, ok := byAddr[want]; !ok {
			t.Errorf("missing row %s", want)
		}
	}
	rec := &regionRecorder{}
	if _, err := analyze(context.Background(), rec, root); err != nil {
		t.Fatal(err)
	}
	if rec.regions["eu-central-1"] == 0 {
		t.Errorf("default-provider resource not priced in eu-central-1: %v", rec.regions)
	}
	if rec.regions["us-east-2"] == 0 {
		t.Errorf("aliased-provider resource not priced in us-east-2: %v", rec.regions)
	}
	if rec.regions["ap-northeast-2"] > 0 {
		t.Errorf("aws_region var default must lose to the provider block: %v", rec.regions)
	}
}

type regionRecorder struct {
	regions map[string]int
}

func (r *regionRecorder) UnitPrice(_ context.Context, q provider.Query) (float64, string, error) {
	if r.regions == nil {
		r.regions = map[string]int{}
	}
	r.regions[q.Region]++
	return 1.0, "Hours", nil
}
