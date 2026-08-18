package output

import (
	"fmt"
	"io"
	"strings"
)

// WriteCompact renders the one-screen CI variant: headline total, a single
// priced-resources table, and everything else collapsed into a details block.
func WriteCompact(w io.Writer, service, region string, items []CostItem) {
	var total float64
	table := newMdTable([]string{"Resource", "Spec", "$/mo"}, []string{"---", "---", "---:"})
	var others []CostItem
	for _, it := range items {
		switch {
		case it.Kind == Fixed && it.Unresolved == "":
			total += it.Monthly
			table.row(it.Addr, it.Spec, fmt.Sprintf("%.2f", it.Monthly))
		case it.Kind == Fixed:
			others = append(others, it)
		default:
			others = append(others, it)
		}
	}
	fmt.Fprintf(w, "## terraform-price — %s (`%s`)\n\n", service, region)
	fmt.Fprintf(w, "**$%.2f/mo** — %d priced\n\n", total, len(table.rows))
	table.render(w)
	writeDetails(w, others)
}

func writeDetails(w io.Writer, items []CostItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n<details><summary>%d not in the total (usage-based / unsupported / unresolved)</summary>\n\n", len(items))
	table := newMdTable([]string{"Resource", "Reason"}, []string{"---", "---"})
	for _, it := range items {
		table.row(it.Addr, shortReason(it))
	}
	table.render(w)
	fmt.Fprintln(w, "\n</details>")
}

func shortReason(it CostItem) string {
	if it.Unresolved != "" {
		// Unresolved strings embed full AWS SDK errors; the prefix carries the meaning.
		if s, _, ok := strings.Cut(it.Unresolved, ":"); ok && len(s) < 40 {
			return "unresolved: " + s
		}
		return "unresolved"
	}
	if it.Note != "" {
		return it.Note
	}
	switch it.Kind {
	case Variable:
		return "usage-based"
	case Unsupported:
		return "unsupported type"
	case Free:
		return "free"
	}
	return ""
}
