package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/output"
)

func writeModuleFixture(t *testing.T, files map[string]string) string {
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

func itemOf(t *testing.T, items []output.CostItem, addr string) output.CostItem {
	t.Helper()
	for _, it := range items {
		if it.Addr == addr {
			return it
		}
	}
	t.Fatalf("addr %s not found in %+v", addr, items)
	return output.CostItem{}
}

const modMain = `
variable "instance_type" {
  default = "t3.micro"
}
resource "aws_instance" "x" {
  instance_type = var.instance_type
}
`

// Local-path modules are expanded with their input values layered over
// the module's variable defaults.
func TestModuleExpansion(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "app" {
  source        = "./mod"
  instance_type = "t3.medium"
}
`,
		"mod/main.tf": modMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "module.app.aws_instance.x")
	if it.Spec != "t3.medium" || it.Monthly != 0.0496*730 {
		t.Fatalf("module item = %+v, want t3.medium @ 0.0496×730", it)
	}
	if it.Kind != output.Fixed {
		t.Fatalf("kind = %v, want Fixed", it.Kind)
	}
}

// Unset inputs fall back to the module's declared variable defaults.
func TestModuleDefaultInput(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "app" {
  source = "./mod"
}
`,
		"mod/main.tf": modMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "module.app.aws_instance.x")
	if it.Spec != "t3.micro" {
		t.Fatalf("spec = %q, want default t3.micro", it.Spec)
	}
}

// count = 0 on the module block gates the whole instance.
func TestModuleCountZeroGate(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "off" {
  source = "./mod"
  count  = 0
}
`,
		"mod/main.tf": modMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "module.off")
	if it.Unresolved == "" {
		t.Fatalf("want gated module row, got %+v", it)
	}
	for _, other := range items {
		if other.Addr == "module.off.aws_instance.x" {
			t.Fatal("count=0 module must not expand contents")
		}
	}
}

// Registry/remote sources keep the generic info row, no expansion.
func TestRegistryModuleInfoRow(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0"
}
`,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	it := itemOf(t, items, "module.vpc")
	if it.Kind != output.Unsupported || it.Note == "" {
		t.Fatalf("registry module row = %+v, want unsupported with note", it)
	}
}

// Module inputs can reference resources in the calling directory.
func TestModuleInputFromResourceRef(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
resource "aws_launch_template" "lt" {
  instance_type = "t3.medium"
}
module "app" {
  source        = "./mod"
  instance_type = aws_launch_template.lt.instance_type
}
`,
		"mod/main.tf": modMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if it := itemOf(t, items, "module.app.aws_instance.x"); it.Spec != "t3.medium" {
		t.Fatalf("spec = %q, want t3.medium via resource ref input", it.Spec)
	}
}

// Nested local modules address as module.outer.module.inner.type.name.
func TestNestedModuleAddressing(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "outer" {
  source        = "./outer"
  instance_type = "t3.medium"
}
`,
		"outer/main.tf": `
module "inner" {
  source        = "../inner"
  instance_type = var.instance_type
}
variable "instance_type" {
  default = "t3.micro"
}
`,
		"inner/main.tf": modMain,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if it := itemOf(t, items, "module.outer.module.inner.aws_instance.x"); it.Spec != "t3.medium" {
		t.Fatalf("nested module item = %+v, want t3.medium", it)
	}
}

// Module inputs win over a tfvars file inside the module directory —
// the caller-supplied value defines this instantiation.
func TestModuleInputBeatsModuleTfvars(t *testing.T) {
	root := writeModuleFixture(t, map[string]string{
		"main.tf": `
module "app" {
  source        = "./mod"
  instance_type = "t3.medium"
}
`,
		"mod/main.tf":            modMain,
		"mod/terraform.tfvars":   `instance_type = "t3.micro"`,
	})
	items, err := analyze(context.Background(), fakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if it := itemOf(t, items, "module.app.aws_instance.x"); it.Spec != "t3.medium" {
		t.Fatalf("spec = %q, want t3.medium (module input over tfvars)", it.Spec)
	}
}
