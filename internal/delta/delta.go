// Package delta compares two cost analyses (a baseline branch and the
// proposed branch) and computes the per-resource monthly cost delta.
// Only items with a computed monthly price participate in the delta;
// usage-based, unsupported, and unresolved items are surfaced as
// not-estimated rows instead of being silently dropped.
package delta

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/yoonhyunwoo/terraform-price/internal/output"
)

type Kind int

const (
	Update Kind = iota
	Create
	Delete
	NotEstimated
)

type Row struct {
	Addr     string
	Change   string
	Prior    float64
	Proposed float64
	Delta    float64
	Note     string
	Kind     Kind
}

type Totals struct {
	Prior        float64
	Proposed     float64
	Delta        float64
	NotEstimated int
}

func priced(it output.CostItem) bool {
	return it.Unresolved == "" && (it.Kind == output.Fixed || it.Kind == output.Free)
}

func reason(it output.CostItem) string {
	if it.Unresolved != "" {
		return "unresolved (" + it.Unresolved + ")"
	}
	if it.Kind == output.Variable {
		return "usage-based"
	}
	return "unsupported type"
}

func specChange(prior, proposed string) string {
	switch {
	case prior == proposed:
		if prior == "" {
			return "update"
		}
		return prior + " (price changed)"
	case prior == "":
		return proposed
	case proposed == "":
		return prior
	default:
		return prior + " → " + proposed
	}
}

func Compute(base, proposed []output.CostItem) ([]Row, Totals) {
	// One address can yield several cost rows (e.g. an RDS instance plus its
	// storage); pair rows within an address, never collapse them.
	baseBy := make(map[string][]output.CostItem, len(base))
	for _, it := range base {
		baseBy[it.Addr] = append(baseBy[it.Addr], it)
	}
	curBy := make(map[string][]output.CostItem, len(proposed))
	for _, it := range proposed {
		curBy[it.Addr] = append(curBy[it.Addr], it)
	}

	var t Totals
	for _, it := range base {
		if priced(it) {
			t.Prior += it.Monthly
		}
	}
	for _, it := range proposed {
		if priced(it) {
			t.Proposed += it.Monthly
		}
	}

	addrs := make([]string, 0, len(baseBy)+len(curBy))
	for a := range baseBy {
		addrs = append(addrs, a)
	}
	for a := range curBy {
		if _, ok := baseBy[a]; !ok {
			addrs = append(addrs, a)
		}
	}
	sort.Strings(addrs)

	var rows []Row
	for _, addr := range addrs {
		bList, curList := baseBy[addr], curBy[addr]
		// Identical specs pair first (unchanged components like storage when
		// only the instance type changes); leftovers pair in order.
		bUsed := make([]bool, len(bList))
		cPair := make([]int, len(curList)) // cur index -> base index, -1 unpaired
		for i := range cPair {
			cPair[i] = -1
		}
		for ci, c := range curList {
			for bi, b := range bList {
				if !bUsed[bi] && b.Spec == c.Spec {
					bUsed[bi] = true
					cPair[ci] = bi
					break
				}
			}
		}
		for ci := range curList {
			if cPair[ci] >= 0 {
				continue
			}
			for bi := range bList {
				if !bUsed[bi] {
					bUsed[bi] = true
					cPair[ci] = bi
					break
				}
			}
		}
		for ci, c := range curList {
			if bi := cPair[ci]; bi >= 0 {
				b := bList[bi]
				if priced(b) && priced(c) && b.Spec == c.Spec && b.Monthly == c.Monthly {
					continue
				}
				rows = append(rows, rowFor(addr, b, c))
			} else {
				rows = append(rows, newRow(addr, c))
			}
		}
		for bi, b := range bList {
			if !bUsed[bi] {
				rows = append(rows, removedRow(addr, b))
			}
		}
	}

	for _, r := range rows {
		if r.Kind != NotEstimated {
			t.Delta += r.Delta
		} else {
			t.NotEstimated++
		}
	}
	return rows, t
}

func rowFor(addr string, b, c output.CostItem) Row {
	if priced(b) && priced(c) {
		return Row{
			Kind: Update, Addr: addr, Change: specChange(b.Spec, c.Spec),
			Prior: b.Monthly, Proposed: c.Monthly, Delta: c.Monthly - b.Monthly,
		}
	}
	var parts []string
	if !priced(b) {
		parts = append(parts, "baseline "+reason(b))
	}
	if !priced(c) {
		parts = append(parts, "proposed "+reason(c))
	}
	return Row{
		Kind: NotEstimated, Addr: addr, Change: specChange(b.Spec, c.Spec),
		Note: strings.Join(parts, "; "),
	}
}

func newRow(addr string, c output.CostItem) Row {
	if priced(c) {
		return Row{Kind: Create, Addr: addr, Change: c.Spec, Proposed: c.Monthly, Delta: c.Monthly}
	}
	return Row{Kind: NotEstimated, Addr: addr, Change: "new", Note: reason(c)}
}

func removedRow(addr string, b output.CostItem) Row {
	if priced(b) {
		return Row{Kind: Delete, Addr: addr, Change: b.Spec, Prior: b.Monthly, Delta: -b.Monthly}
	}
	return Row{Kind: NotEstimated, Addr: addr, Change: "removed", Note: reason(b)}
}

func WriteMarkdown(w io.Writer, label string, rows []Row, t Totals) {
	fmt.Fprintf(w, "## Monthly cost change vs `%s`\n\n", label)
	if len(rows) == 0 {
		fmt.Fprintln(w, "No priced changes.")
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintln(w, "| Resource | Change | Prior/mo (USD) | Proposed/mo (USD) | Δ/mo (USD) |")
	fmt.Fprintln(w, "|---|---|---:|---:|---:|")
	for _, r := range rows {
		if r.Kind == NotEstimated {
			fmt.Fprintf(w, "| `%s` | %s | — | — | not estimated: %s |\n", r.Addr, r.Change, r.Note)
			continue
		}
		fmt.Fprintf(w, "| `%s` | %s | %.2f | %.2f | %+.2f |\n", r.Addr, r.Change, r.Prior, r.Proposed, r.Delta)
	}
	fmt.Fprintf(w, "\nBaseline $%.2f/mo → Proposed $%.2f/mo (Δ %+.2f, not estimated: %d)\n\n",
		t.Prior, t.Proposed, t.Delta, t.NotEstimated)
}

// WriteCompact renders the CI variant: headline delta, one table of changed
// resources, unchanged rows counted rather than listed.
func WriteCompact(w io.Writer, rows []Row, t Totals) {
	fmt.Fprintln(w, "## Monthly cost change")
	switch {
	case t.Delta > 0:
		fmt.Fprintf(w, "**Monthly cost increased by $%.2f/mo** ($%.2f/mo → $%.2f/mo) ↑\n\n", t.Delta, t.Prior, t.Proposed)
	case t.Delta < 0:
		fmt.Fprintf(w, "**Monthly cost decreased by $%.2f/mo** ($%.2f/mo → $%.2f/mo) ↓\n\n", -t.Delta, t.Prior, t.Proposed)
	default:
		fmt.Fprintf(w, "**No monthly cost change** ($%.2f/mo)\n\n", t.Proposed)
	}
	fmt.Fprintln(w, "| Resource | Change | New $/mo | Change $/mo |")
	fmt.Fprintln(w, "|---|---|---:|---:|")
	var unchanged, notEstimated int
	var ne []string
	for _, r := range rows {
		switch {
		case r.Kind == NotEstimated:
			notEstimated++
			ne = append(ne, fmt.Sprintf("- `%s` (%s): %s", r.Addr, r.Change, shortNote(r.Note)))
		case r.Delta == 0:
			unchanged++
		default:
			fmt.Fprintf(w, "| `%s` | %s | %.2f | %+.2f |\n", r.Addr, escCell(r.Change), r.Proposed, r.Delta)
		}
	}
	foot := ""
	if unchanged > 0 {
		foot = fmt.Sprintf("%d unchanged (not shown)", unchanged)
	}
	if notEstimated > 0 {
		if foot != "" {
			foot += " · "
		}
		foot += fmt.Sprintf("%d not estimated", notEstimated)
	}
	if foot != "" {
		fmt.Fprintf(w, "\n%s\n", foot)
	}
	if len(ne) > 0 {
		fmt.Fprintln(w, "\n<details><summary>not estimated</summary>")
		for _, l := range ne {
			fmt.Fprintln(w, l)
		}
		fmt.Fprintln(w, "</details>")
	}
}

func escCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// shortNote keeps unresolved rows to the meaningful prefix — the full AWS SDK
// error chain is in the log, not the PR comment.
func shortNote(note string) string {
	if s, _, ok := strings.Cut(note, ":"); ok && strings.HasPrefix(note, "unresolved") && len(s) < 40 {
		return s
	}
	if len(note) > 60 {
		return note[:57] + "…"
	}
	return note
}
