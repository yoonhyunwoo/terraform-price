package output

import (
	"fmt"
	"io"
)

type Kind int

const (
	Fixed Kind = iota
	Variable
	Free
	Unsupported
)

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
	resolved := 0
	fmt.Fprintf(w, "# Cost Estimate — %s (`%s`)\n\n", service, region)
	fmt.Fprintf(w, "> ⚠️ **OnDemand 정가(list price) 기준** — RI / Savings Plan / EDP 협상 할인 적용 시 실제 청구 단가는 더 낮습니다.\n\n")

	fmt.Fprintln(w, "## 고정비 (Fixed) — 시간당·GB-월 기반")
	fmt.Fprintln(w, "| Resource | Spec | 단가 (USD) | 단위 | 월 (USD) |")
	fmt.Fprintln(w, "|---|---|---:|---|---:|")
	for _, it := range fixed {
		if it.Unresolved != "" {
			fmt.Fprintf(w, "| `%s` | — | — | — | ⚠️ %s |\n", it.Addr, it.Unresolved)
			continue
		}
		total += it.Monthly
		resolved++
		fmt.Fprintf(w, "| `%s` | %s | %.4f | %s | %.2f |\n", it.Addr, it.Spec, it.UnitPrice, it.Unit, it.Monthly)
	}
	fmt.Fprintf(w, "| **고정비 합계 / 월** | | | | **%.2f** |\n\n", total)

	if len(variable) > 0 {
		fmt.Fprintln(w, "## 유동비 (usage 기반, 설계상 합계 제외)")
		fmt.Fprintln(w, "| Resource | 유형 | 비고 |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, it := range variable {
			fmt.Fprintf(w, "| `%s` | %s | %s |\n", it.Addr, it.Type, it.Note)
		}
		fmt.Fprintln(w)
	}

	if len(unsupported) > 0 {
		fmt.Fprintf(w, "## ⚠️ 미지원 과금 리소스 (매핑 누락 — 추정에 미포함)\n\n")
		fmt.Fprintln(w, "| Resource | 유형 | 비고 |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, it := range unsupported {
			fmt.Fprintf(w, "| `%s` | %s | %s |\n", it.Addr, it.Type, it.Note)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "_고정비 %d건 산정 · 유동비 %d건(제외) · 미지원 과금 %d건(⚠️ 추정 누락) · 무과금 %d건 · 단가 = AWS Price List API · 월 = 단가 × usage(730h / GB·월) × 수 · 실행: `terraform-price`_\n", resolved, len(variable), len(unsupported), len(free))
}
