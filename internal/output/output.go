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
	fmt.Fprintf(w, "> OnDemand 정가 기준 (RI / Savings Plan / EDP 할인 미반영)\n\n")

	fmt.Fprintln(w, "## 고정비")
	fmt.Fprintln(w, "| Resource | Spec | 단가 (USD) | 단위 | 월 (USD) |")
	fmt.Fprintln(w, "|---|---|---:|---|---:|")
	for _, it := range fixed {
		if it.Unresolved != "" {
			fmt.Fprintf(w, "| `%s` | — | — | — | ⚠️ %s |\n", it.Addr, it.Unresolved)
			continue
		}
		total += it.Monthly
		fmt.Fprintf(w, "| `%s` | %s | %.4f | %s | %.2f |\n", it.Addr, it.Spec, it.UnitPrice, it.Unit, it.Monthly)
	}
	fmt.Fprintf(w, "| **고정비 합계 / 월** | | | | **%.2f** |\n\n", total)

	if len(variable) > 0 {
		fmt.Fprintln(w, "## 유동비")
		fmt.Fprintln(w, "| Resource | 유형 | 단가 (USD) | 비고 |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, it := range variable {
			rate := "—"
			if len(it.Rates) > 0 {
				parts := make([]string, len(it.Rates))
				for i, r := range it.Rates {
					parts[i] = fmt.Sprintf("%s %.4f/%s", r.Label, r.UnitPrice, r.Unit)
				}
				rate = strings.Join(parts, " · ")
			}
			fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n", it.Addr, it.Type, rate, it.Note)
		}
		fmt.Fprintln(w)
	}

	if len(unsupported) > 0 {
		fmt.Fprintf(w, "## ⚠️ 미지원\n\n")
		fmt.Fprintln(w, "| Resource | 유형 | 비고 |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, it := range unsupported {
			fmt.Fprintf(w, "| `%s` | %s | %s |\n", it.Addr, it.Type, it.Note)
		}
		fmt.Fprintln(w)
	}

	if len(free) > 0 {
		fmt.Fprintf(w, "## 무료\n\n")
		fmt.Fprintln(w, "| Resource | 유형 |")
		fmt.Fprintln(w, "|---|---|")
		for _, it := range free {
			fmt.Fprintf(w, "| `%s` | %s |\n", it.Addr, it.Type)
		}
		fmt.Fprintln(w)
	}
}
