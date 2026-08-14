package parser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Resource struct {
	Type  string
	Name  string
	Exprs map[string]hcl.Expression
	// BlockCounts records how many times a repeated nested block type
	// appeared (Exprs keeps only the last instance's attributes).
	BlockCounts map[string]int
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
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %s\n", e.Name(), diags.Error())
			continue
		}
		sbody, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range sbody.Blocks {
			if blk.Type == "resource" && len(blk.Labels) == 2 {
				exprs := map[string]hcl.Expression{}
				counts := map[string]int{}
				collectExprsCounts(blk.Body, "", exprs, counts)
				if len(counts) == 0 {
					counts = nil
				}
				resources = append(resources, &Resource{
					Type: blk.Labels[0], Name: blk.Labels[1], Exprs: exprs, BlockCounts: counts,
				})
				continue
			}
			if blk.Type == "module" && len(blk.Labels) == 1 {
				exprs := map[string]hcl.Expression{}
				for name, attr := range blk.Body.Attributes {
					exprs[name] = attr.Expr
				}
				resources = append(resources, &Resource{
					Type: "module", Name: blk.Labels[0], Exprs: exprs,
				})
			}
		}
	}
	return resources, nil
}

func collectExprs(body *hclsyntax.Body, prefix string, out map[string]hcl.Expression) {
	collectExprsCounts(body, prefix, out, nil)
}

func collectExprsCounts(body *hclsyntax.Body, prefix string, out map[string]hcl.Expression, counts map[string]int) {
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
		if blk.Type != "dynamic" && counts != nil {
			counts[bkey]++
		}
		if blk.Type == "dynamic" && len(blk.Labels) == 1 {
			// dynamic "x" { content { ... } }: register content attrs
			// under x.* so mapper probes hit.
			for _, inner := range blk.Body.Blocks {
				if inner.Type != "content" {
					continue
				}
				ckey := blk.Labels[0]
				if prefix != "" {
					ckey = prefix + "." + blk.Labels[0]
				}
				collectExprsCounts(inner.Body, ckey, out, nil)
			}
		}
		collectExprsCounts(blk.Body, bkey, out, counts)
	}
}

// ParseOutputs maps output block names to their value expressions.
func ParseOutputs(dir string) map[string]hcl.Expression {
	out := map[string]hcl.Expression{}
	parser := hclparse.NewParser()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		f, diags := parser.ParseHCLFile(filepath.Join(dir, e.Name()))
		if diags.HasErrors() || f == nil {
			continue
		}
		sbody, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range sbody.Blocks {
			if blk.Type != "output" || len(blk.Labels) != 1 {
				continue
			}
			if attr, ok := blk.Body.Attributes["value"]; ok {
				out[blk.Labels[0]] = attr.Expr
			}
		}
	}
	return out
}

// VariableNames lists the directory's declared variable block names.
func VariableNames(dir string) map[string]bool {
	names := map[string]bool{}
	parser := hclparse.NewParser()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		f, diags := parser.ParseHCLFile(filepath.Join(dir, e.Name()))
		if diags.HasErrors() || f == nil {
			continue
		}
		sbody, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range sbody.Blocks {
			if blk.Type == "variable" && len(blk.Labels) == 1 {
				names[blk.Labels[0]] = true
			}
		}
	}
	return names
}
