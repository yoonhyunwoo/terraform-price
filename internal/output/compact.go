package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/yoonhyunwoo/terraform-price/internal/i18n"
)

// WriteCompact renders the one-screen CI variant: headline total, a single
// priced-resources table, and everything else collapsed into a details block.
func WriteCompact(w io.Writer, l *i18n.L, service, region string, items []CostItem) {
	var total float64
	table := newMdTable([]string{l.T(i18n.MsgColResource), l.T(i18n.MsgColSpec), l.T(i18n.MsgColMonthly)}, []string{"---", "---", "---:"})
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
	fmt.Fprintln(w, l.T(i18n.MsgTitle, map[string]interface{}{"Service": service, "Region": region}))
	fmt.Fprintln(w)
	fmt.Fprintln(w, l.T(i18n.MsgTotal, map[string]interface{}{"Total": fmt.Sprintf("%.2f", total), "Count": len(table.rows)}))
	fmt.Fprintln(w)
	table.render(w)
	writeDetails(w, l, others)
}

func writeDetails(w io.Writer, l *i18n.L, items []CostItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n<details><summary>%s</summary>\n\n", l.T(i18n.MsgNotInTotal, map[string]interface{}{"Count": len(items)}))
	table := newMdTable([]string{l.T(i18n.MsgColResource), l.T(i18n.MsgColNotes)}, []string{"---", "---"})
	for _, it := range items {
		table.row(it.Addr, shortReason(l, it))
	}
	table.render(w)
	fmt.Fprintln(w, "\n</details>")
}

func shortReason(l *i18n.L, it CostItem) string {
	if it.Unresolved != "" {
		// Unresolved strings embed full AWS SDK errors; the prefix carries the meaning.
		if s, _, ok := strings.Cut(it.Unresolved, ":"); ok && len(s) < 40 {
			return l.T(i18n.MsgReasonUnresolvedWith, map[string]interface{}{"Reason": s})
		}
		return l.T(i18n.MsgReasonUnresolved)
	}
	if it.Note != "" {
		return it.Note
	}
	switch it.Kind {
	case Variable:
		return l.T(i18n.MsgReasonUsageBased)
	case Unsupported:
		return l.T(i18n.MsgReasonUnsupportedType)
	case Free:
		return l.T(i18n.MsgReasonFree)
	}
	return ""
}
