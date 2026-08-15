package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseDirCollectsModuleBlocks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mod.tf", `
module "db" {
  source = "./modules/rds"
}
`)
	rs, err := ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Type != "module" || rs[0].Name != "db" {
		t.Fatalf("want module.db, got %+v", rs)
	}
}

func TestParseDirSkipsBrokenFileKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bad.tf", `resource "aws_instance" "x" { count = }`)
	write(t, dir, "good.tf", `resource "aws_instance" "ok" { instance_type = "t3.micro" }`)
	rs, err := ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Name != "ok" {
		t.Fatalf("want only the resource from good.tf, got %+v", rs)
	}
	if rs[0].Exprs["count"] != nil {
		t.Fatalf("good.tf resource should not carry a count expr, got %+v", rs[0].Exprs["count"])
	}
}

func TestParseDirCollectsCountExpr(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "c.tf", `resource "aws_instance" "web" {
  count         = 3
  instance_type = "t3.micro"
}`)
	rs, err := ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Exprs["count"] == nil {
		t.Fatalf("count expr not collected: %+v", rs)
	}
}
