package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

// The conformance gate: vendored infracost fixtures (see
// testdata/conformance/README.md) must enumerate the same set of resource
// addresses after every parsing change. Run with -update to regenerate the
// snapshot after an INTENDED change; an unintended diff fails the gate.
var conformanceUpdate = flag.Bool("conformance-update", false, "regenerate testdata/conformance/snapshot.txt")

func TestConformanceAddresses(t *testing.T) {
	update := *conformanceUpdate
	root := filepath.Join("..", "..", "testdata", "conformance", "aws")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("fixtures missing: %v (see testdata/conformance/README.md)", err)
	}
	got := map[string][]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		// Determinism: two independent runs must enumerate identically.
		a1 := runFixture(t, dir)
		a2 := runFixture(t, dir)
		if strings.Join(a1, "\n") != strings.Join(a2, "\n") {
			t.Errorf("%s: nondeterministic enumeration across two runs", e.Name())
		}
		got[e.Name()] = a1
	}
	snapPath := filepath.Join(root, "..", "snapshot.txt")
	if update {
		if err := writeSnapshot(snapPath, got); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := readSnapshot(snapPath)
	if err != nil {
		t.Fatalf("snapshot: %v (regenerate with go test -run TestConformanceAddresses -update)", err)
	}
	for name, addrs := range got {
		w, ok := want[name]
		if !ok {
			t.Errorf("%s: new fixture dir not in snapshot (regenerate with -update)", name)
			continue
		}
		missing, extra := diffSets(w, addrs)
		for _, m := range missing {
			t.Errorf("%s: address %q LOST — parsing regression", name, m)
		}
		for _, x := range extra {
			t.Errorf("%s: address %q gained — verify intended, then -update", name, x)
		}
	}
}

// stubPricer prices every query so every mappable resource becomes a row;
// address enumeration must not depend on price availability.
type stubPricer struct{}

func (stubPricer) UnitPrice(context.Context, provider.Query) (float64, string, error) {
	return 1.0, "Hrs", nil
}

func runFixture(t *testing.T, dir string) []string {
	t.Helper()
	items, err := analyze(context.Background(), stubPricer{}, dir)
	if err != nil {
		t.Errorf("%s: analyze: %v", filepath.Base(dir), err)
		return nil
	}
	addrs := make([]string, 0, len(items))
	for _, it := range items {
		addrs = append(addrs, it.Addr)
	}
	sort.Strings(addrs)
	return addrs
}

func diffSets(want, got []string) (missing, extra []string) {
	ws := map[string]bool{}
	for _, w := range want {
		ws[w] = true
	}
	gs := map[string]bool{}
	for _, g := range got {
		gs[g] = true
	}
	for _, w := range want {
		if !gs[w] {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !ws[g] {
			extra = append(extra, g)
		}
	}
	return missing, extra
}

func readSnapshot(path string) (map[string][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		name, addr, ok := strings.Cut(line, "\t")
		if !ok || name == "" || addr == "" {
			return nil, fmt.Errorf("bad snapshot line %q", line)
		}
		if _, seen := out[name]; !seen {
			out[name] = nil
		}
		if addr != "-" {
			out[name] = append(out[name], addr)
		}
	}
	return out, sc.Err()
}

func writeSnapshot(path string, snap map[string][]string) error {
	names := make([]string, 0, len(snap))
	for n := range snap {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		if len(snap[n]) == 0 {
			fmt.Fprintf(&b, "%s\t-\n", n) // fixture with no resource rows
			continue
		}
		for _, a := range snap[n] {
			fmt.Fprintf(&b, "%s\t%s\n", n, a)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
