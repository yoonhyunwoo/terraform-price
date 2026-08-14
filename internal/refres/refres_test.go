package refres

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func setup(t *testing.T, files map[string]string) *RefResolver {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := parser.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := resolver.NewResolver(dir)
	return New(rs, res)
}

func TestBasicRefFollow(t *testing.T) {
	r := setup(t, map[string]string{
		"main.tf": `
resource "aws_instance" "base" {
  instance_type = "t3.micro"
}
resource "aws_launch_template" "lt" {
  instance_type = aws_instance.base.instance_type
}
`,
	})
	if err := r.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	v, ok := r.ResolveAttr("aws_launch_template.lt", "instance_type")
	if !ok || v.AsString() != "t3.micro" {
		t.Fatalf(`want t3.micro/true, got %v/%v`, v, ok)
	}
}

func TestTransitiveRef(t *testing.T) {
	r := setup(t, map[string]string{
		"main.tf": `
resource "aws_instance" "a" {
  instance_type = "t3.small"
}
resource "aws_launch_template" "b" {
  instance_type = aws_instance.a.instance_type
}
resource "aws_autoscaling_group" "c" {
  instance_type = aws_launch_template.b.instance_type
}
`,
	})
	v, ok := r.ResolveAttr("aws_autoscaling_group.c", "instance_type")
	if !ok || v.AsString() != "t3.small" {
		t.Fatalf(`want t3.small/true, got %v/%v`, v, ok)
	}
}

func TestCycleDetection(t *testing.T) {
	r := setup(t, map[string]string{
		"main.tf": `
resource "aws_instance" "a" {
  instance_type = aws_instance.b.instance_type
}
resource "aws_instance" "b" {
  instance_type = aws_instance.a.instance_type
}
`,
	})
	if err := r.Verify(); err == nil {
		t.Fatal("cycle not detected")
	}
}

func TestSelfReference(t *testing.T) {
	r := setup(t, map[string]string{
		"main.tf": `
resource "aws_instance" "a" {
  instance_type = aws_instance.a.instance_type
}
`,
	})
	if err := r.Verify(); err == nil {
		t.Fatal("self-reference cycle not detected")
	}
}

func TestMemoization(t *testing.T) {
	r := setup(t, map[string]string{
		"main.tf": `
resource "aws_instance" "a" {
  instance_type = "t3.micro"
}
resource "aws_launch_template" "b" {
  instance_type = aws_instance.a.instance_type
}
`,
	})
	r.ResolveResource("aws_launch_template.b")
	// Second call should return cached result
	attrs := r.ResolveResource("aws_launch_template.b")
	if attrs == nil {
		t.Fatal("memoized resolve returned nil")
	}
}

func TestVarAndRefMixed(t *testing.T) {
	r := setup(t, map[string]string{
		"terraform.tfvars": `env = "prod"`,
		"main.tf": `
resource "aws_instance" "a" {
  instance_type = "t3.micro"
  tags = { env = var.env }
}
resource "aws_launch_template" "b" {
  instance_type = aws_instance.a.instance_type
}
`,
	})
	v, ok := r.ResolveAttr("aws_instance.a", "tags")
	if !ok {
		t.Fatal("tags not resolved")
	}
	if v.Type().IsObjectType() {
		if e := v.GetAttr("env"); e.IsKnown() && e.AsString() != "prod" {
			t.Fatalf(`tags.env: want prod, got %v`, e)
		}
	}
}
