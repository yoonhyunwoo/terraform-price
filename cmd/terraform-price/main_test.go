package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

func TestClassifyItemWiring(t *testing.T) {
	cases := []struct {
		kind mapper.Kind
		want output.Kind
	}{
		{mapper.KindVariable, output.Variable},
		{mapper.KindFree, output.Free},
		{mapper.KindUnsupported, output.Unsupported},
		{mapper.Kind(99), output.Unsupported},
	}
	for _, c := range cases {
		got := classifyItem(c.kind, "addr", "aws_x", "note")
		if got.Kind != c.want {
			t.Errorf("kind %v: want output %v, got %v", c.kind, c.want, got.Kind)
		}
		if got.Addr != "addr" || got.Type != "aws_x" || got.Note != "note" {
			t.Errorf("kind %v: fields not propagated: %+v", c.kind, got)
		}
	}
}

func TestMetaCount(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(
		"n = 3\nnames = { a = 1, b = 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`
resource "aws_instance" "literal"  { count = 3 }
resource "aws_instance" "via_var"  { count = var.n }
resource "aws_instance" "missing"  { count = var.missing }
resource "aws_instance" "for_each" { for_each = var.names }
resource "aws_instance" "toset"    { for_each = toset(["x"]) }
resource "aws_instance" "none"     {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := parser.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := resolver.NewResolver(dir)
	want := map[string]struct {
		n    int
		note bool
	}{
		"literal":  {3, false},
		"via_var":  {3, false},
		"missing":  {1, true},
		"for_each": {2, false},
		"toset":    {1, true},
		"none":     {1, false},
	}
	for _, r := range rs {
		w, ok := want[r.Name]
		if !ok {
			t.Fatalf("unexpected resource %q", r.Name)
		}
		n, note := metaCount(r, res)
		if n != w.n || (note != "") != w.note {
			t.Errorf("%s: metaCount = %d, %q; want %d note-present=%v", r.Name, n, note, w.n, w.note)
		}
	}
}
