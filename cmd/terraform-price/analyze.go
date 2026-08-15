package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/tf/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/tf/refs"
	"github.com/yoonhyunwoo/terraform-price/internal/tf/resolver"
)

// defaultRegion matches the AWS Price List Query API home region and the
// infracost conformance fixtures, so unit prices line up for comparison.
const defaultRegion = "us-east-1"

const gatedResourceNote = "count = 0 — resource not created"

// maxModuleDepth guards against symlink loops; real stacks are shallower.
const maxModuleDepth = 5

func analyze(ctx context.Context, pricer provider.Pricer, dir string) ([]output.CostItem, error) {
	ds, err := buildDirScope(dir, nil)
	if err != nil {
		return nil, err
	}
	region := defaultRegion
	if v, ok := ds.res.VarString("aws_region"); ok {
		region = v
	}
	if v := ds.providerRegion(""); v != "" {
		region = v // the default aws provider block beats the var heuristic
	}
	// Registration must precede parent evaluation: outputs compute lazily
	// on first reference, so earlier resolution would miss them.
	ds.registerModules(dir, region, "", 0)
	ds.fixpoint()
	return priceDir(ctx, pricer, ds, region, "")
}

// providerRegion resolves an aliased aws provider block's region ("" = the
// default provider); empty when absent or unresolvable.
func (ds *dirScope) providerRegion(alias string) string {
	for _, p := range ds.provs {
		if p.Name != alias {
			continue
		}
		e, ok := p.Exprs["region"]
		if !ok {
			continue
		}
		if v, ok := ds.res.ResolveExpr(e); ok && v.Type() == cty.String {
			return v.AsString()
		}
	}
	return ""
}

type dirScope struct {
	res        *resolver.Resolver
	plain      []*parser.Resource
	mods       []*parser.Resource
	provs      []*parser.Resource
	idx        map[string]*parser.Resource
	rr         *refs.RefResolver
	countBased map[string]bool
	children   []*moduleInstance
}

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
		if r.Type == "provider" {
			ds.provs = append(ds.provs, r)
			continue
		}
		ds.plain = append(ds.plain, r)
	}
	ds.idx = parser.Index(ds.plain)
	ds.rr = refs.New(ds.plain, res)
	// A reference cycle (e.g. a self depends_on) degrades the members to
	// unresolved attributes — ResolveResource's re-entry guard keeps this
	// terminating — instead of aborting the whole directory.
	_ = ds.rr.Verify()
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

func (ds *dirScope) registerModules(dir, region, prefix string, depth int) {
	for _, m := range ds.mods {
		var fns []func() map[string]cty.Value
		for _, mi := range ds.moduleInstances(m, dir, region, prefix, depth) {
			fns = append(fns, mi.outputsFn)
			ds.children = append(ds.children, mi)
		}
		ds.res.RegisterModule(m.Name, mergeModuleFns(fns))
	}
	// Settle in registration order so later modules see earlier ones'
	// outputs; scope() itself must not force (mid-build hazard).
	for _, mi := range ds.children {
		ds.res.ForceModule(mi.m.Name)
	}
}

func (ds *dirScope) moduleInstances(m *parser.Resource, dir, region, prefix string, depth int) []*moduleInstance {
	single := func() *moduleInstance {
		return &moduleInstance{
			dir: dir, region: region, prefix: prefix + "module." + m.Name + ".",
			parent: ds.res, m: m, depth: depth,
		}
	}
	items, known := forEachItems(m, ds.res)
	if !known {
		return []*moduleInstance{single()}
	}
	if len(items) == 0 {
		return []*moduleInstance{single()}
	}
	out := make([]*moduleInstance, 0, len(items))
	for _, it := range items {
		out = append(out, &moduleInstance{
			dir: dir, region: region,
			prefix: fmt.Sprintf("%smodule.%s[%q].", prefix, m.Name, it.key),
			parent: ds.res, m: m, depth: depth,
			eachKey: it.key, eachKeyVal: it.keyVal, eachVal: it.val, hasEach: true,
		})
	}
	return out
}

func mergeModuleFns(fns []func() map[string]cty.Value) func() map[string]cty.Value {
	if len(fns) == 1 {
		return fns[0]
	}
	return func() map[string]cty.Value {
		out := map[string]cty.Value{}
		for _, fn := range fns {
			for k, v := range fn() {
				out[k] = v
			}
		}
		return out
	}
}

type eachItem struct {
	key    string
	keyVal cty.Value
	val    cty.Value
}

// forEachItems expands a module block's for_each; known=false means no
// usable for_each (absent or unresolvable) — caller builds one instance.
func forEachItems(m *parser.Resource, res *resolver.Resolver) ([]eachItem, bool) {
	e, ok := m.Exprs["for_each"]
	if !ok {
		return nil, false
	}
	v, ok := res.ResolveExpr(e)
	if !ok || !v.IsKnown() || v.IsNull() {
		return nil, false
	}
	t := v.Type()
	if t.IsMapType() || t.IsObjectType() {
		keys := map[string]cty.Value{}
		for it := v.ElementIterator(); it.Next(); {
			k, val := it.Element()
			ks, ok := resolver.Str(k)
			if !ok {
				return nil, false
			}
			keys[ks] = val
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		items := make([]eachItem, 0, len(sorted))
		for _, k := range sorted {
			items = append(items, eachItem{key: k, keyVal: cty.StringVal(k), val: keys[k]})
		}
		return items, true
	}
	if t.IsListType() || t.IsSetType() || t.IsTupleType() {
		var items []eachItem
		for _, val := range v.AsValueSlice() {
			key, ok := resolver.Str(val)
			if !ok {
				return nil, false
			}
			items = append(items, eachItem{key: key, keyVal: val, val: val})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
		return items, true
	}
	return nil, false
}

func (ds *dirScope) fixpoint() {
	for ds.res.RetryLocals() {
		ds.rr.Reset()
		ds.res.SetResources(ds.rr.AllResolved(), ds.countBased)
	}
}

var moduleMetaAttrs = map[string]bool{
	"source": true, "count": true, "for_each": true, "providers": true, "depends_on": true,
}

// moduleInstance memoizes one module call: output references and the
// pricing pass share a single evaluation. scope == nil after force means
// unfetchable.
type moduleInstance struct {
	dir, region, prefix string
	parent              *resolver.Resolver
	m                   *parser.Resource
	depth               int

	eachKey    string
	eachKeyVal cty.Value
	eachVal    cty.Value
	hasEach    bool

	built   bool
	scope   *dirScope
	outputs map[string]cty.Value // nil = unavailable (unfetchable/cycle)
}

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
		var v cty.Value
		var ok bool
		if mi.hasEach {
			v, ok = mi.parent.ResolveExprWithEach(e, mi.eachKeyVal, mi.eachVal)
		} else {
			v, ok = mi.parent.ResolveExpr(e)
		}
		if ok && v.IsKnown() && !v.IsNull() {
			inputs[k] = v
		}
	}
	ds, err := buildDirScope(modDir, inputs)
	if err != nil {
		return // degrade to an info row in priceModule
	}
	mi.scope = ds
	ds.registerModules(modDir, mi.region, mi.prefix, mi.depth+1)
	ds.fixpoint()
	mi.outputs = evalOutputs(modDir, ds.res)
}

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

func (mi *moduleInstance) outputsFn() map[string]cty.Value {
	mi.force()
	return mi.outputs
}

func evalOutputs(dir string, res *resolver.Resolver) map[string]cty.Value {
	out := map[string]cty.Value{}
	for name, expr := range parser.ParseOutputs(dir) {
		if v, ok := res.ResolveExpr(expr); ok && v.IsKnown() && !v.IsNull() {
			out[name] = v
		}
	}
	return out
}

func priceDir(ctx context.Context, pricer provider.Pricer, ds *dirScope, region, prefix string) ([]output.CostItem, error) {
	items, err := priceResources(ctx, pricer, ds, ds.plain, region, prefix)
	if err != nil {
		return nil, err
	}
	for _, mi := range ds.children {
		items = append(items, priceModule(ctx, pricer, mi)...)
	}
	return items, nil
}

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

func priceResources(ctx context.Context, pricer provider.Pricer, ds *dirScope, resources []*parser.Resource, region, prefix string) ([]output.CostItem, error) {
	var items []output.CostItem
	for _, r := range resources {
		addr := prefix + r.Type + "." + r.Name
		rowRegion := resourceRegion(ds, r, region)
		if each, ok := forEachItems(r, ds.res); ok {
			if len(each) == 0 {
				items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: gatedResourceNote})
				continue
			}
			// Expand per for_each key with `each` bound so item attrs
			// (instance_types = each.value.node_instance_types) resolve.
			for _, it := range each {
				ds.res.SetResourceEach(it.keyVal, it.val)
				row := priceOneResource(ctx, pricer, r, ds.res, ds.idx, rowRegion, fmt.Sprintf("%s[%q]", addr, it.key), 1, "")
				ds.res.ClearResourceEach()
				items = append(items, row...)
			}
			continue
		}
		// count/for_each = 0 gates everything: the resource is not created,
		// so resolution failures below are moot. metaCount returns a note
		// whenever it falls back to 1, so n == 0 always means a resolved 0.
		n, metaNote := metaCount(r, ds.res)
		if n == 0 {
			items = append(items, output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: gatedResourceNote})
			continue
		}
		items = append(items, priceOneResource(ctx, pricer, r, ds.res, ds.idx, rowRegion, addr, n, metaNote)...)
	}
	return items, nil
}

// resourceRegion overrides the dir's base region when the resource pins an
// aliased provider (provider = aws.ue2) whose block carries its own region.
func resourceRegion(ds *dirScope, r *parser.Resource, base string) string {
	e, ok := r.Exprs["provider"]
	if !ok {
		return base
	}
	tr, diags := hcl.AbsTraversalForExpr(e)
	if diags.HasErrors() || len(tr) < 2 {
		return base
	}
	attr, ok := tr[1].(hcl.TraverseAttr)
	if !ok {
		return base
	}
	if v := ds.providerRegion(attr.Name); v != "" {
		return v
	}
	return base
}

func priceOneResource(ctx context.Context, pricer provider.Pricer, r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource, region, addr string, n int, metaNote string) []output.CostItem {
	kind, spec, note := mapper.MapResource(r, res, idx, region)
	if kind == mapper.KindVariable {
		return []output.CostItem{variableItem(ctx, pricer, addr, r.Type, note, spec)}
	}
	if kind != mapper.KindFixed {
		return []output.CostItem{classifyItem(kind, addr, r.Type, note)}
	}
	if spec == nil {
		return []output.CostItem{{Kind: output.Fixed, Addr: addr, Unresolved: note}}
	}
	spec.Count *= n
	if n > 1 {
		spec.Label = fmt.Sprintf("%s × %d", spec.Label, n)
	}
	if metaNote != "" {
		spec.Label += " (" + metaNote + ")"
	}
	items := []output.CostItem{pricedItem(ctx, pricer, addr, r.Type, spec)}
	for _, extra := range mapper.ExtraSpecs(r, res, idx) {
		if !extra.Global {
			extra.Region = region
			for i := range extra.Rates {
				extra.Rates[i].Region = region
			}
		}
		extra.Count *= spec.Count
		items = append(items, pricedItem(ctx, pricer, addr, r.Type, extra))
	}
	return items
}

func pricedItem(ctx context.Context, pricer provider.Pricer, addr, typ string, spec *mapper.Spec) output.CostItem {
	if spec.FlatPrice != nil {
		// Published flat fee with no Price List row (e.g. EKS extended support).
		return output.CostItem{
			Kind: output.Fixed, Addr: addr, Type: typ, Spec: spec.Label,
			UnitPrice: *spec.FlatPrice, Unit: "Hours",
			Monthly: *spec.FlatPrice * spec.UsageQty * float64(spec.Count),
		}
	}
	p, unit, err := pricer.UnitPrice(ctx, provider.Query{Service: spec.ServiceCode, Region: spec.Region, Filters: spec.Filters, PreferUnit: spec.PreferUnit})
	if err != nil {
		return output.CostItem{Kind: output.Fixed, Addr: addr, Unresolved: "price lookup failed: " + err.Error()}
	}
	return output.CostItem{
		Kind: output.Fixed, Addr: addr, Type: typ, Spec: spec.Label,
		UnitPrice: p, Unit: unit, Monthly: p * spec.UsageQty * float64(spec.Count),
	}
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

func registryModuleDir(m *parser.Resource, parent *resolver.Resolver, src string) string {
	rs := parseRegistrySource(src)
	if rs == nil {
		return ""
	}
	pin := ""
	exact := false
	if e, ok := m.Exprs["version"]; ok {
		if v, ok := parent.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() && v.Type() == cty.String {
			pin = v.AsString()
			exact = regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(pin)
		}
	}
	candidates, err := pickVersions(rs, pin)
	if err != nil {
		return ""
	}
	if !exact {
		candidates = newestPerMajor(candidates)
	}
	// An unpinned module written against an older interface (Terraform
	// rejects undeclared inputs) walks back one major at a time until a
	// version declares every input the call passes; exact pins are final.
	var bestEffort string
	for i, v := range candidates {
		if i >= 10 {
			break
		}
		dir, ok := fetchRegistryModule(rs, v)
		if !ok {
			continue
		}
		if bestEffort == "" {
			bestEffort = dir
		}
		if moduleDeclaresInputs(dir, m) {
			return dir
		}
	}
	return bestEffort
}

func newestPerMajor(vers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vers {
		maj := strings.Split(v, ".")[0]
		if !seen[maj] {
			seen[maj] = true
			out = append(out, v)
		}
	}
	return out
}

func moduleDeclaresInputs(dir string, m *parser.Resource) bool {
	declared := parser.VariableNames(dir)
	for name := range m.Exprs {
		if !moduleMetaAttrs[name] && name != "version" && !declared[name] {
			return false
		}
	}
	return true
}
