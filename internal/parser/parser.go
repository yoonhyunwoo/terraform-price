package parser

import (
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Resource struct {
	Type  string
	Name  string
	File  string
	Exprs map[string]hcl.Expression
}

func ParseDir(dir string) ([]*Resource, error) {
	parser := hclparse.NewParser()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var resources []*Resource
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			continue
		}
		sbody, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range sbody.Blocks {
			if blk.Type != "resource" || len(blk.Labels) != 2 {
				continue
			}
			exprs := map[string]hcl.Expression{}
			collectExprs(blk.Body, "", exprs)
			resources = append(resources, &Resource{
				Type: blk.Labels[0], Name: blk.Labels[1], File: e.Name(), Exprs: exprs,
			})
		}
	}
	return resources, nil
}

func collectExprs(body *hclsyntax.Body, prefix string, out map[string]hcl.Expression) {
	for name, attr := range body.Attributes {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		out[key] = attr.Expr
	}
	for _, blk := range body.Blocks {
		bkey := blk.Type
		if prefix != "" {
			bkey = prefix + "." + blk.Type
		}
		collectExprs(blk.Body, bkey, out)
	}
}
