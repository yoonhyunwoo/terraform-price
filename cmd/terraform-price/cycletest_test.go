package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfDependsOnDegrades(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.tf", `
resource "aws_instance" "x" {
  instance_type = "t3.micro"
  depends_on    = [aws_instance.x]
}

resource "null_resource" "pw" {
  triggers = {
    id = aws_instance.x.id
  }
  depends_on = [aws_instance.x]
}
`)
	items, err := analyze(context.Background(), fakePricer{}, dir)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	it := itemOf(t, items, "aws_instance.x")
	if it.Spec != "t3.micro" {
		t.Fatalf("spec = %q, want t3.micro (self depends_on must not abort)", it.Spec)
	}
	if it.Monthly <= 0 {
		t.Fatalf("monthly = %v, want priced", it.Monthly)
	}
}

func TestCycleDegrades(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, `
resource "aws_instance" "a" {
  instance_type = aws_instance.b.instance_type
}
resource "aws_instance" "b" {
  instance_type = aws_instance.a.instance_type
}
`)
	items, err := analyze(context.Background(), fakePricer{}, dir)
	if err != nil {
		t.Fatalf("value cycle must degrade, not error: %v", err)
	}
	for _, addr := range []string{"aws_instance.a", "aws_instance.b"} {
		it := itemOf(t, items, addr)
		if it.Spec != "" || !strings.Contains(it.Unresolved, "unresolved") {
			t.Fatalf("%s = %+v, want unresolved degradation", addr, it)
		}
	}
}
