package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

// Locals spread across multiple files (darktrace pattern)
func TestLocalsAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "locals.tf"), []byte(`
locals {
  all_tags = merge({deployment = local.deployment_id}, var.extra)
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "deployment.tf"), []byte(`
locals {
  deployment_id = "prod-42"
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "variables.tf"), []byte(`
variable "extra" {
  default = {team = "core"}
}
`), 0o644)

	r := NewResolver(dir)
	v, ok := r.ResolveLocal("all_tags")
	if !ok {
		t.Fatal("all_tags not resolved")
	}
	_ = v
	// deployment via merge should work
	dep, ok := r.ResolveLocal("deployment_id")
	if !ok {
		t.Fatal("deployment_id not resolved (cross-file local)")
	}
	_ = dep
}

// Var defaults from variables.tf
func TestVarDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "variables.tf"), []byte(`
variable "region" {
  default = "us-east-1"
}
variable "env" {
  type = string
}
`), 0o644)
	r := NewResolver(dir)
	if v, ok := r.VarString("region"); !ok || v != "us-east-1" {
		t.Fatalf("region default: want us-east-1, got %q ok=%v", v, ok)
	}
	if _, ok := r.VarString("env"); ok {
		t.Fatal("env has no default, should be unset")
	}
}

// auto.tfvars
func TestAutoTfvars(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(`region = "eu-west-1"`), 0o644)
	os.WriteFile(filepath.Join(dir, "prod.auto.tfvars"), []byte(`region = "ap-northeast-2"`), 0o644)
	r := NewResolver(dir)
	if v, ok := r.VarString("region"); !ok || v != "ap-northeast-2" {
		t.Fatalf("auto.tfvars should win: got %q ok=%v", v, ok)
	}
}
