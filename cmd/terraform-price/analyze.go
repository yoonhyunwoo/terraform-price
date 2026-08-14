package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zclconf/go-cty/cty"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/refres"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

// defaultRegion is the single owner of the fallback region used when the
// directory's tfvars do not set aws_region.
const defaultRegion = "ap-northeast-2"

// gatedResourceNote marks a resource whose count/for_each resolved to 0:
// it is not created, so nothing is priced.
const gatedResourceNote = "count = 0 — resource not created"

// maxModuleDepth bounds module recursion (defensive against symlink
// loops); real module stacks stay far shallower.
const maxModuleDepth = 5

// analyze parses the Terraform directory and prices every resource,
// recursively expanding module blocks. The region comes from the root
// directory's tfvars (aws_region, default ap-northeast-2) and is inherited
// by module instances. Module outputs referenced by the parent
// (module.x.out) chain lazily into the child scope.
func analyze(ctx context.Context, pricer provider.Pricer, dir string) ([]output.CostItem, error) {
	ds, err := buildDirScope(dir, nil)
	if err != nil {
		return nil, err
	}
	region := defaultRegion
	if v, ok := ds.res.VarString("aws_region"); ok {
		region = v
	}
	// Register module scopes BEFORE parent expressions are evaluated;
	// registration is not evaluation — outputs compute on first reference.
	ds.registerModules(dir, region, "", 0)
	ds.fixpoint()
	return priceDir(ctx, pricer, ds, region, "")
}

// dirScope is one directory's fully resolved evaluation scope: the
// resolver (vars, locals, cross-resource attrs) plus its parsed resources.
type dirScope struct {
	res        *resolver.Resolver
	plain      []*parser.Resource
	mods       []*parser.Resource
	idx        map[string]*parser.Resource
	rr         *refres.RefResolver
	countBased map[string]bool
	children   []*moduleInstance
}

// buildDirScope resolves a directory: resolver construction, parsing, and
// the locals/cross-resource fixpoint. Module output references cannot
// resolve yet — callers must registerModules and re-run fixpoint.
func buildDirScope(dir string, inputs map[string]cty.Value) (*dirScope, error) {
	res := resolver.NewResolverWithVars(dir, inputs)
	resources, err := parser.ParseDir(dir)
	if err != nil {
		return nil, err
	}
	ds := &dirScope{res: res}
	for _, r := range resources {
		if r.Type == "module" {
			ds.mods = append(ds.mods, r)
			continue
		}
		ds.plain = append(ds.plain, r)
	}
	ds.idx = parser.Index(ds.plain)
	ds.rr = refres.New(ds.plain, res)
	if err := ds.rr.Verify(); err != nil {
		return nil, err
	}
	ds.countBased = map[string]bool{}
	for _, r := range ds.plain {
		if _, ok := r.Exprs["count"]; ok {
			ds.countBased[r.Addr()] = true
		}
		if _, ok := r.Exprs["for_each"]; ok {
			ds.countBased[r.Addr()] = true
		}
	}
	res.SetResources(ds.rr.AllResolved(), ds.countBased)
	ds.fixpoint()
	return ds, nil
}

// registerModules registers each module block's instance with the scope's
// resolver so module.m.out references chain lazily into the child scope.
func (ds *dirScope) registerModules(dir, region, prefix string, depth int) {
	for _, m := range ds.mods {
		mi := &moduleInstance{
			dir: dir, region: region, prefix: prefix + "module." + m.Name + ".",
			parent: ds.res, m: m, depth: depth,
		}
		ds.res.RegisterModule(m.Name, mi.outputsFn)
		ds.children = append(ds.children, mi)
	}
}

// fixpoint drives locals / cross-resource resolution to convergence.
// Re-running after module registration picks up module-output references.
func (ds *dirScope) fixpoint() {
	for ds.res.RetryLocals() {
		ds.rr.Reset()
		ds.res.SetResources(ds.rr.AllResolved(), ds.countBased)
	}
}

// moduleMetaAttrs are module-block arguments that are not inputs.
var moduleMetaAttrs = map[string]bool{
	"source": true, "count": true, "for_each": true, "providers": true, "depends_on": true,
}

// moduleInstance is one module call, lazily built and memoized: the child
// scope and its output values are computed once on first demand — either a
// parent module.m.out reference or the pricing pass — so both share a
// single evaluation. scope == nil after force means unfetchable or failed;
// everything degrades to the generic info row.
type moduleInstance struct {
	dir, region, prefix string
	parent              *resolver.Resolver
	m                   *parser.Resource
	depth               int

	built   bool
	scope   *dirScope
	outputs map[string]cty.Value // nil = unavailable (unfetchable/cycle)
}

// force builds the instance exactly once.
func (mi *moduleInstance) force() {
	if mi.built {
		return
	}
	mi.built = true
	modDir := mi.sourceDir()
	if modDir == "" {
		return
	}
	inputs := map[string]cty.Value{}
	for k, e := range mi.m.Exprs {
		if moduleMetaAttrs[k] {
			continue
		}
		if v, ok := mi.parent.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() {
			inputs[k] = v
		}
	}
	ds, err := buildDirScope(modDir, inputs)
	if err != nil {
		return // a broken child module must not kill the whole report
	}
	mi.scope = ds
	ds.registerModules(modDir, mi.region, mi.prefix, mi.depth+1)
	ds.fixpoint()
	mi.outputs = evalOutputs(modDir, ds.res)
}

// sourceDir resolves the module block's source to a local-path or cached
// registry directory; "" when unfetchable or beyond the depth bound.
func (mi *moduleInstance) sourceDir() string {
	if mi.depth >= maxModuleDepth {
		return ""
	}
	src := ""
	if e, ok := mi.m.Exprs["source"]; ok {
		if v, ok := mi.parent.ResolveExpr(e); ok {
			if s, ok := resolver.Str(v); ok {
				src = s
			}
		}
	}
	if d := localModuleDir(mi.dir, src); d != "" {
		return d
	}
	return registryModuleDir(mi.m, mi.parent, src)
}

// outputsFn is the thunk registered on the parent resolver; re-entrant
// calls (module-output cycles) are cut off by the resolver's running flag.
func (mi *moduleInstance) outputsFn() map[string]cty.Value {
	mi.force()
	return mi.outputs
}

// evalOutputs evaluates the module directory's output block values in the
// child scope. Outputs referencing computed attrs or unknown scopes stay
// out (they are unknown-unknowns for pricing).
func evalOutputs(dir string, res *resolver.Resolver) map[string]cty.Value {
	out := map[string]cty.Value{}
	for name, expr := range parser.ParseOutputs(dir) {
		if v, ok := res.ResolveExpr(expr); ok && v.IsKnown() && !v.IsNull() {
			out[name] = v
		}
	}
	return out
}

// priceDir prices one directory's direct resources, then each registered
// module instance's subtree.
func priceDir(ctx context.Context, pricer provider.Pricer, ds *dirScope, region, prefix string) ([]output.CostItem, error) {
	items, err := priceResources(ctx, pricer, ds.plain, ds.res, ds.idx, region, prefix)
	if err != nil {
		return nil, err
	}
	for _, mi := range ds.children {
		items = append(items, priceModule(ctx, pricer, mi)...)
	}
	return items, nil
}

// priceModule prices one module instance. A count/for_each of 0 gates the
// whole instance; an unfetchable or failed source degrades to the generic
// info row — a fetch problem must never kill the report.
func priceModule(ctx context.Context, pricer provider.Pricer, mi *moduleInstance) []output.CostItem {
	addr := strings.TrimSuffix(mi.prefix, ".")
	if n, note := metaCount(mi.m, mi.parent); n == 0 && note == "" {
		return []output.CostItem{{Kind: output.Fixed, Addr: addr, Unresolved: "count = 0 — module not created"}}
	}
	mi.force()
	if mi.scope == nil {
		kind, _, note := mapper.MapResource(mi.m, mi.parent, nil, mi.region)
		return []output.CostItem{classifyItem(kind, addr, mi.m.Type, note)}
	}
	items, err := priceDir(ctx, pricer, mi.scope, mi.region, mi.prefix)
	if err != nil {
		return []output.CostItem{{Kind: output.Unsupported, Addr: addr, Type: "module", Note: "module analysis failed: " + err.Error()}}
	}
	return items
}

// localModuleDir resolves a "./" / "../" source to a directory, or ""
// when the source is remote/registry-shaped.
func localModuleDir(dir, src string) string {
	if src == "" || strings.Contains(src, "://") || strings.HasPrefix(src, "git::") {
		return ""
	}
	if parseRegistrySource(src) != nil {
		return ""
	}
	modDir := filepath.Join(dir, src)
	if fi, err := os.Stat(modDir); err != nil || !fi.IsDir() {
		return ""
	}
	return modDir
}

// priceResources maps and prices a directory's direct resources.
func priceResources(ctx context.Context, pricer provider.Pricer, resources []*parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource, region, prefix string) ([]output.CostItem, error) {
	var items []output.CostItem
	for _, r := range resources {
		addr := prefix + r.Type + "." + r.Name
		// count/for_each = 0 gates everything: the resource is not created,
		// so resolution failures below are moot. metaCount returns a note
		// whenever it falls back to 1, so n == 0 always means a resolved 0.
		n, metaNote := metaCount(r, res)
		if n == 0 {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: gatedResourceNote})
			continue
		}
		kind, spec, note := mapper.MapResource(r, res, idx, region)
		if kind == mapper.KindVariable {
			items = append(items, variableItem(ctx, pricer, addr, r.Type, note, spec))
			continue
		}
		if kind != mapper.KindFixed {
			items = append(items, classifyItem(kind, addr, r.Type, note))
			continue
		}
		if spec == nil {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: note})
			continue
		}
		spec.Count *= n
		if n > 1 {
			spec.Label = fmt.Sprintf("%s × %d", spec.Label, n)
		}
		if metaNote != "" {
			spec.Label += " (" + metaNote + ")"
		}
		p, unit, err := pricer.UnitPrice(ctx, provider.Query{Service: spec.ServiceCode, Region: spec.Region, Filters: spec.Filters, PreferUnit: spec.PreferUnit})
		if err != nil {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: "price lookup failed: " + err.Error()})
			continue
		}
		items = append(items, output.CostItem{
			Kind: output.Fixed, Addr: addr, Type: r.Type, Spec: spec.Label,
			UnitPrice: p, Unit: unit, Monthly: p * spec.UsageQty * float64(spec.Count),
		})
	}
	return items, nil
}

func variableItem(ctx context.Context, pricer provider.Pricer, addr, typ, note string, spec *mapper.Spec) output.CostItem {
	item := output.CostItem{Kind: output.Variable, Addr: addr, Type: typ, Note: note}
	if spec != nil && spec.Note != "" {
		item.Note = spec.Note
	}
	if spec == nil {
		return item
	}
	// Keep the spec label so the report shows what drives the variable fee.
	item.Spec = spec.Label
	return item
}

func classifyItem(kind mapper.Kind, addr, typ, note string) output.CostItem {
	switch kind {
	case mapper.KindVariable:
		return output.CostItem{Kind: output.Variable, Addr: addr, Type: typ, Note: note}
	case mapper.KindFree:
		return output.CostItem{Kind: output.Free, Addr: addr, Type: typ, Note: note}
	default:
		return output.CostItem{Kind: output.Unsupported, Addr: addr, Type: typ, Note: note}
	}
}

// metaCount resolves count/for_each to an instance count. A literal
// count, a for_each over a resolvable map/set, and a resolved numeric
// count all multiply; anything unresolvable prices as one with a note.
func metaCount(r *parser.Resource, res *resolver.Resolver) (int, string) {
	if e, ok := r.Exprs["count"]; ok {
		if v, ok := res.ResolveExpr(e); ok {
			if n, ok := resolver.Num(v); ok {
				if n < 1 {
					return 0, ""
				}
				return int(n), ""
			}
		}
		return 1, "count unresolved — priced as 1"
	}
	if e, ok := r.Exprs["for_each"]; ok {
		if v, ok := res.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() {
			t := v.Type()
			if t.IsMapType() || t.IsObjectType() {
				return len(v.AsValueMap()), ""
			}
			if t.IsListType() || t.IsSetType() || t.IsTupleType() {
				return len(v.AsValueSlice()), ""
			}
		}
		return 1, "for_each unresolved — priced as 1"
	}
	return 1, ""
}

// registryModuleDir fetches a public-registry module (exact or ~>-pinned
// version, else latest) into the cache and returns its directory; "" on
// any failure. A `version` attr of the module block is the pin.
func registryModuleDir(m *parser.Resource, parent *resolver.Resolver, src string) string {
	rs := parseRegistrySource(src)
	if rs == nil {
		return ""
	}
	pin := ""
	if e, ok := m.Exprs["version"]; ok {
		if v, ok := parent.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() && v.Type() == cty.String {
			pin = v.AsString()
		}
	}
	version, err := resolveVersion(rs, pin)
	if err != nil {
		return ""
	}
	dir, ok := fetchRegistryModule(rs, version)
	if !ok {
		return ""
	}
	return dir
}
