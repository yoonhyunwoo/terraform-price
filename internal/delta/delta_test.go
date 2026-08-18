package delta

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/output"
)

func fixed(addr, spec string, monthly float64) output.CostItem {
	return output.CostItem{Kind: output.Fixed, Addr: addr, Spec: spec, Monthly: monthly}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestComputeUpdateCreateDeleteSkip(t *testing.T) {
	base := []output.CostItem{
		fixed("aws_instance.web", "t2.micro", 8.76),
		fixed("aws_instance.same", "t3.nano", 3.00),
		fixed("aws_nat_gateway.nat", "NAT gateway", 32.85),
	}
	proposed := []output.CostItem{
		fixed("aws_instance.web", "t3.medium", 30.37),
		fixed("aws_instance.same", "t3.nano", 3.00),
		fixed("aws_ebs_volume.data", "gp3 100GB", 8.00),
	}

	rows, totals := Compute(base, proposed)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (skip must drop unchanged): %+v", len(rows), rows)
	}
	wantOrder := []string{"aws_ebs_volume.data", "aws_instance.web", "aws_nat_gateway.nat"}
	for i, r := range rows {
		if r.Addr != wantOrder[i] {
			t.Fatalf("row %d addr = %s, want %s (sorted)", i, r.Addr, wantOrder[i])
		}
	}
	if rows[0].Kind != Create || rows[0].Delta != 8.00 {
		t.Errorf("create row = %+v", rows[0])
	}
	if rows[1].Kind != Update || rows[1].Change != "t2.micro → t3.medium" || !approx(rows[1].Delta, 21.61) {
		t.Errorf("update row = %+v", rows[1])
	}
	if rows[2].Kind != Delete || rows[2].Delta != -32.85 {
		t.Errorf("delete row = %+v", rows[2])
	}

	wantDelta := 8.00 + 21.61 - 32.85
	if !approx(totals.Delta, wantDelta) {
		t.Errorf("totals.Delta = %v, want %v", totals.Delta, wantDelta)
	}
	if !approx(totals.Prior, 8.76+3.00+32.85) || !approx(totals.Proposed, 30.37+3.00+8.00) {
		t.Errorf("totals = %+v", totals)
	}
	if !approx(totals.Delta, totals.Proposed-totals.Prior) {
		t.Errorf("delta %v != proposed-prior %v", totals.Delta, totals.Proposed-totals.Prior)
	}
	if totals.NotEstimated != 0 {
		t.Errorf("NotEstimated = %d, want 0", totals.NotEstimated)
	}
}

func TestComputeNotEstimatedExcludedFromDelta(t *testing.T) {
	base := []output.CostItem{
		{Kind: output.Variable, Addr: "aws_s3_bucket.logs"},
		fixed("aws_instance.web", "t3.micro", 9.05),
	}
	proposed := []output.CostItem{
		fixed("aws_s3_bucket.logs", "S3 standard", 5.00),
		fixed("aws_instance.web", "t3.micro", 9.05),
	}

	rows, totals := Compute(base, proposed)
	if len(rows) != 1 || rows[0].Kind != NotEstimated {
		t.Fatalf("rows = %+v, want single NotEstimated", rows)
	}
	if rows[0].Note != "baseline usage-based" {
		t.Errorf("note = %q", rows[0].Note)
	}
	if totals.Delta != 0 {
		t.Errorf("Delta = %v, want 0 (variable→fixed is not estimated)", totals.Delta)
	}
	if totals.NotEstimated != 1 {
		t.Errorf("NotEstimated = %d, want 1", totals.NotEstimated)
	}
	if totals.Prior != 9.05 || totals.Proposed != 9.05+5.00 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestComputeFreeTransitions(t *testing.T) {
	base := []output.CostItem{
		{Kind: output.Free, Addr: "aws_iam_role.r", Type: "aws_iam_role"},
		fixed("aws_instance.web", "t3.micro", 9.05),
	}
	proposed := []output.CostItem{
		{Kind: output.Free, Addr: "aws_iam_role.r", Type: "aws_iam_role"},
		fixed("aws_instance.web", "t3.medium", 36.21),
	}

	rows, totals := Compute(base, proposed)
	if len(rows) != 1 || rows[0].Addr != "aws_instance.web" {
		t.Fatalf("rows = %+v, want only the instance update (free→free skipped)", rows)
	}
	if !approx(totals.Delta, 27.16) {
		t.Errorf("Delta = %v, want 27.16", totals.Delta)
	}
}

func TestComputeUnresolvedReasons(t *testing.T) {
	base := []output.CostItem{
		{Kind: output.Fixed, Addr: "aws_db_instance.db", Unresolved: "engine unresolved"},
	}
	proposed := []output.CostItem{
		{Kind: output.Unsupported, Addr: "aws_db_instance.db"},
	}

	rows, _ := Compute(base, proposed)
	if len(rows) != 1 || rows[0].Kind != NotEstimated {
		t.Fatalf("rows = %+v", rows)
	}
	want := "baseline unresolved (engine unresolved); proposed unsupported type"
	if rows[0].Note != want {
		t.Errorf("note = %q, want %q", rows[0].Note, want)
	}

	onlyBase, _ := Compute(base, nil)
	if len(onlyBase) != 1 || onlyBase[0].Kind != NotEstimated || onlyBase[0].Change != "removed" {
		t.Errorf("removed-unresolved row = %+v", onlyBase[0])
	}
}

func TestWriteMarkdown(t *testing.T) {
	rows := []Row{
		{Kind: Update, Addr: "aws_instance.web", Change: "t2.micro → t3.medium", Prior: 8.76, Proposed: 30.37, Delta: 21.61},
		{Kind: NotEstimated, Addr: "aws_s3_bucket.logs", Change: "new", Note: "usage-based"},
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, "base", rows, Totals{Prior: 41.90, Proposed: 63.37, Delta: 21.61, NotEstimated: 1})
	out := buf.String()
	for _, want := range []string{
		"## Delta vs baseline (`base`)",
		"| `aws_instance.web` | t2.micro → t3.medium | 8.76 | 30.37 | +21.61 |",
		"| `aws_s3_bucket.logs` | new | — | — | not estimated: usage-based |",
		"Baseline $41.90/mo → Proposed $63.37/mo (Δ +21.61, not estimated: 1)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	WriteMarkdown(&buf, "base", nil, Totals{})
	if !strings.Contains(buf.String(), "No priced changes.") {
		t.Errorf("empty rows output = %q", buf.String())
	}
}

func TestWriteCompact(t *testing.T) {
	rows := []Row{
		{Kind: Update, Addr: "aws_instance.web", Change: "t2.micro → t3.medium", Prior: 8.76, Proposed: 30.37, Delta: 21.61},
		{Kind: Update, Addr: "aws_instance.same", Change: "t3.micro", Prior: 7.59, Proposed: 7.59, Delta: 0},
		{Kind: NotEstimated, Addr: "aws_s3_bucket.logs", Change: "new", Note: "unresolved (price lookup failed: operation error Pricing: GetProducts, get identity: " + strings.Repeat("x", 300) + ")"},
	}
	var buf bytes.Buffer
	WriteCompact(&buf, "baseline", rows, Totals{Prior: 41.90, Proposed: 63.37, Delta: 21.61, NotEstimated: 1})
	out := buf.String()
	for _, want := range []string{
		"**$41.90/mo → $63.37/mo** (Δ **+21.61/mo**)",
		"| `aws_instance.web` | t2.micro → t3.medium | 30.37 | +21.61 |",
		"1 unchanged (not shown) · 1 not estimated",
		"- `aws_s3_bucket.logs` (new): unresolved (price lookup failed",
		"<details><summary>not estimated</summary>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "aws_instance.same") {
		t.Errorf("unchanged row must not be listed:\n%s", out)
	}
	if len(out) > 600 {
		t.Errorf("compact output too long (%d bytes):\n%s", len(out), out)
	}
}
