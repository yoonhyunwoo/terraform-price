package output

import (
	"fmt"
	"io"
	"strings"
)

type Kind int

const (
	Fixed Kind = iota
	Variable
	Free
	Unsupported
)

type RateLine struct {
	Label     string
	UnitPrice float64
	Unit      string
}

type CostItem struct {
	Addr       string
	Type       string
	Spec       string
	UnitPrice  float64
	Unit       string
	Monthly    float64
	Kind       Kind
	Note       string
	Unresolved string
	Rates      []RateLine
}

// mdTable accumulates cells and renders once. Escaping pipes/newlines and
// clamping cell count to the headers happen only here — the single
// structure→bytes boundary (kubectl printTable clamps arity, go-pretty
// RenderMarkdown escapes cells); callers can never break the table with
// free-form Terraform strings.
type mdTable struct {
	headers []string
	aligns  []string
	rows    [][]string
}

func newMdTable(headers, aligns []string) *mdTable {
	return &mdTable{headers: headers, aligns: aligns}
}

func (t *mdTable) row(cells ...string) {
	if len(cells) > len(t.headers) {
		cells = cells[:len(t.headers)]
	}
	t.rows = append(t.rows, cells)
}

func mdEscapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", "<br/>")
	return s
}

func (t *mdTable) render(w io.Writer) {
	writeRow(w, t.headers)
	fmt.Fprintf(w, "|%s|\n", strings.Join(t.aligns, "|"))
	for _, r := range t.rows {
		for i := range r {
			r[i] = mdEscapeCell(r[i])
		}
		writeRow(w, r)
	}
}

func writeRow(w io.Writer, cells []string) {
	fmt.Fprint(w, "|")
	for _, c := range cells {
		if c == "" {
			fmt.Fprint(w, " |")
		} else {
			fmt.Fprintf(w, " %s |", c)
		}
	}
	fmt.Fprintln(w)
}

func WriteMarkdown(w io.Writer, service, region string, items []CostItem) {
	var fixed, variable, unsupported, free []CostItem
	for _, it := range items {
		switch it.Kind {
		case Fixed:
			fixed = append(fixed, it)
		case Variable:
			variable = append(variable, it)
		case Free:
			free = append(free, it)
		default:
			unsupported = append(unsupported, it)
		}
	}
	total := 0.0
	fmt.Fprintf(w, "# Cost Estimate — %s (`%s`)\n\n", service, region)
	fmt.Fprintf(w, "> OnDemand list prices only — RI / Savings Plan / EDP discounts not applied\n\n")
	if len(items) == 0 {
		fmt.Fprintf(w, "> No resources found in this directory. If the Terraform lives in a\n> subdirectory (e.g. examples/xxx or modules/xxx), point the tool at that path.\n\n")
	}

	fmt.Fprintln(w, "## Fixed")
	fixedT := newMdTable(
		[]string{"Resource", "Spec", "Unit price (USD)", "Unit", "Monthly (USD)"},
		[]string{"---", "---", "---:", "---", "---:"},
	)
	for _, it := range fixed {
		if it.Unresolved != "" {
			fixedT.row("`"+it.Addr+"`", "—", "—", "—", it.Unresolved)
			continue
		}
		total += it.Monthly
		fixedT.row("`"+it.Addr+"`", it.Spec, fmt.Sprintf("%.4f", it.UnitPrice), it.Unit, fmt.Sprintf("%.2f", it.Monthly))
	}
	fixedT.row("**Fixed total / month**", "", "", "", "**"+fmt.Sprintf("%.2f", total)+"**")
	fixedT.render(w)
	fmt.Fprintln(w)

	if len(variable) > 0 {
		fmt.Fprintln(w, "## Variable")
		varT := newMdTable(
			[]string{"Resource", "Type", "Unit price (USD)", "Notes"},
			[]string{"---", "---", "---", "---"},
		)
		for _, it := range variable {
			rate := "—"
			if len(it.Rates) > 0 {
				parts := make([]string, len(it.Rates))
				for i, r := range it.Rates {
					parts[i] = fmt.Sprintf("%s %.4f/%s", r.Label, r.UnitPrice, r.Unit)
				}
				rate = strings.Join(parts, " · ")
			}
			varT.row("`"+it.Addr+"`", it.Type, rate, it.Note)
		}
		varT.render(w)
		fmt.Fprintln(w)
	}

	if len(unsupported) > 0 {
		fmt.Fprintf(w, "## Unsupported\n\n")
		unsT := newMdTable(
			[]string{"Resource", "Type", "Notes"},
			[]string{"---", "---", "---"},
		)
		for _, it := range unsupported {
			unsT.row("`"+it.Addr+"`", it.Type, it.Note)
		}
		unsT.render(w)
		fmt.Fprintln(w)
	}

	if len(free) > 0 {
		fmt.Fprintf(w, "## Free\n\n")
		freeT := newMdTable(
			[]string{"Resource", "Type"},
			[]string{"---", "---"},
		)
		for _, it := range free {
			freeT.row("`"+it.Addr+"`", it.Type)
		}
		freeT.render(w)
		fmt.Fprintln(w)
	}
}
