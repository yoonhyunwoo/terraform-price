package main

import (
	"context"
	"strings"
	"testing"
)

func TestCrossResourceRefEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, `
resource "aws_instance" "base" {
  instance_type = "t3.micro"
}
resource "aws_launch_template" "lt" {
  instance_type = aws_instance.base.instance_type
}
`)
	items, err := analyze(context.Background(), fakePricer{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	monthlyOf(t, items, "aws_instance.base")
	// Launch template itself is info-priced (priced via ASG), but the
	// key check is that analysis doesn't fail and the ASG-style resolution works.
}

func TestCrossResourceRefASG(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, `
resource "aws_launch_template" "lt" {
  instance_type = aws_instance.base.instance_type
}
resource "aws_instance" "base" {
  instance_type = "t3.micro"
}
resource "aws_autoscaling_group" "asg" {
  desired_capacity = 2
  launch_template { id = aws_launch_template.lt.id }
}
`)
	items, err := analyze(context.Background(), fakePricer{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := monthlyOf(t, items, "aws_autoscaling_group.asg")
	if m <= 0 {
		t.Fatalf("ASG monthly should be positive, got %f", m)
	}
}

func TestCycleFailsAnalysis(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, `
resource "aws_instance" "a" {
  instance_type = aws_instance.b.instance_type
}
resource "aws_instance" "b" {
  instance_type = aws_instance.a.instance_type
}
`)
	_, err := analyze(context.Background(), fakePricer{}, dir)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want cycle error, got %v", err)
	}
}
