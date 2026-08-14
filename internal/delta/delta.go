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
	baseBy := make(map[string]output.CostItem, len(base))
	for _, it := range base {
		baseBy[it.Addr] = it
	}
	curBy := make(map[string]output.CostItem, len(proposed))
	for _, it := range proposed {
		curBy[it.Addr] = it
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
		b, inBase := baseBy[addr]
		c, inCur := curBy[addr]
		switch {
		case inBase && inCur:
			if priced(b) && priced(c) {
				if b.Spec == c.Spec && b.Monthly == c.Monthly {
					continue
				}
				rows = append(rows, Row{
					Kind: Update, Addr: addr, Change: specChange(b.Spec, c.Spec),
					Prior: b.Monthly, Proposed: c.Monthly, Delta: c.Monthly - b.Monthly,
				})
				continue
			}
			var parts []string
			if !priced(b) {
				parts = append(parts, "baseline "+reason(b))
			}
			if !priced(c) {
				parts = append(parts, "proposed "+reason(c))
			}
			rows = append(rows, Row{
				Kind: NotEstimated, Addr: addr, Change: specChange(b.Spec, c.Spec),
				Note: strings.Join(parts, "; "),
			})
		case inCur:
			if priced(c) {
				rows = append(rows, Row{
					Kind: Create, Addr: addr, Change: c.Spec,
					Proposed: c.Monthly, Delta: c.Monthly,
				})
				continue
			}
			rows = append(rows, Row{Kind: NotEstimated, Addr: addr, Change: "new", Note: reason(c)})
		default:
			if priced(b) {
				rows = append(rows, Row{
					Kind: Delete, Addr: addr, Change: b.Spec,
					Prior: b.Monthly, Delta: -b.Monthly,
				})
				continue
			}
			rows = append(rows, Row{Kind: NotEstimated, Addr: addr, Change: "removed", Note: reason(b)})
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

func WriteMarkdown(w io.Writer, label string, rows []Row, t Totals) {
	fmt.Fprintf(w, "## Delta vs baseline (`%s`)\n\n", label)
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
