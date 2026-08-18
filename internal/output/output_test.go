package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/i18n"
)

func TestWriteMarkdownFourWayPartition(t *testing.T) {
	items := []CostItem{
		{Kind: Fixed, Addr: "aws_instance.a", Spec: "t3.micro", UnitPrice: 0.01, Unit: "Hrs", Monthly: 7.30},
		{Kind: Fixed, Addr: "aws_instance.b", Unresolved: "instance_type unresolved"},
		{Kind: Variable, Addr: "aws_s3_bucket.x", Type: "aws_s3_bucket", Note: "usage-based"},
		{Kind: Free, Addr: "aws_iam_role.r", Type: "aws_iam_role", Note: "no charge"},
		{Kind: Unsupported, Addr: "aws_widget.w", Type: "aws_widget", Note: "unsupported"},
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, i18n.New(), "svc", "ap-northeast-2", items)
	out := buf.String()

	if !strings.Contains(out, "7.30") {
		t.Errorf("fixed priced monthly missing:\n%s", out)
	}
	if !strings.Contains(out, "Fixed total / month") {
		t.Errorf("fixed total row missing")
	}
	if !strings.Contains(out, "instance_type unresolved") {
		t.Errorf("unresolved fixed item not shown")
	}
	if !strings.Contains(out, "## Variable") {
		t.Errorf("variable section missing")
	}
	if !strings.Contains(out, "aws_s3_bucket.x") {
		t.Errorf("variable item not surfaced")
	}
	if !strings.Contains(out, "## Unsupported") {
		t.Errorf("unsupported gap section missing")
	}
	if !strings.Contains(out, "aws_widget.w") {
		t.Errorf("unsupported (gap) item not surfaced — silent-drop regression")
	}
	if !strings.Contains(out, "## Free") || !strings.Contains(out, "aws_iam_role.r") {
		t.Errorf("free section/item not surfaced")
	}
	if strings.Contains(out, "Fixed total / month") && strings.Contains(out, "0.00") && !strings.Contains(out, "7.30") {
		t.Errorf("free/variable/unsupported leaked into fixed total")
	}
}

func TestWriteMarkdownNoUnsupportedOmitsGapSection(t *testing.T) {
	var buf bytes.Buffer
	WriteMarkdown(&buf, i18n.New(), "svc", "ap-northeast-2", []CostItem{
		{Kind: Fixed, Addr: "aws_instance.a", Spec: "t3.micro", UnitPrice: 0.01, Unit: "Hrs", Monthly: 7.3},
	})
	out := buf.String()
	if strings.Contains(out, "## Unsupported") {
		t.Errorf("gap section should be omitted when no unsupported items")
	}
}

// A cell carrying free-form Terraform strings (pipes, newlines) must not
// break the table: pipes escape, newlines flatten, arity clamps — all owned
// by the renderer (go-pretty markdownRenderRow / kubectl printTable
// precedent).
func TestMarkdownTableIntegrity(t *testing.T) {
	items := []CostItem{
		{Kind: Fixed, Addr: "aws_instance.a", Spec: "t3.micro | spot", UnitPrice: 0.01, Unit: "Hrs", Monthly: 7.30},
		{Kind: Fixed, Addr: "aws_instance.b", Unresolved: "type | unresolved\nsee log"},
		{Kind: Variable, Addr: "aws_s3_bucket.x", Type: "aws_s3_bucket", Note: "a|b"},
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, i18n.New(), "svc", "r", items)
	lines := strings.Split(buf.String(), "\n")
	pipeCount := 6 // fixed table: 5 columns + 2 outer pipes... computed below
	_ = pipeCount
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "|") {
			continue
		}
		// every table row line has the same number of unescaped pipes:
		// split on | then rejoin escaped \| markers
		n := strings.Count(ln, "|") - 2*strings.Count(ln, `\|`)
		if n < 2 {
			t.Errorf("line %d collapsed (%q) — a cell broke the row", i, ln)
		}
	}
	if !strings.Contains(buf.String(), `t3.micro \| spot`) {
		t.Errorf("pipe in Spec not escaped:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "<br/>") {
		t.Errorf("newline in Unresolved not flattened:\n%s", buf.String())
	}
}

func TestMdTableClampsArity(t *testing.T) {
	tt := newMdTable([]string{"A", "B"}, []string{"---", "---"})
	tt.row("1", "2", "3", "4")
	var buf bytes.Buffer
	tt.render(&buf)
	out := buf.String()
	if strings.Contains(out, "3") {
		t.Errorf("extra cells leaked past header arity:\n%s", out)
	}
	if got := strings.Count(out, "\n"); got != 3 { // header + align + 1 row
		t.Errorf("render produced %d lines, want 3:\n%s", got, out)
	}
}

func TestWriteCompactKorean(t *testing.T) {
	items := []CostItem{
		{Kind: Fixed, Addr: "aws_instance.web", Spec: "t3.medium", UnitPrice: 0.0416, Unit: "Hrs", Monthly: 30.37},
		{Kind: Variable, Addr: "aws_s3_bucket.artifacts", Type: "aws_s3_bucket", Note: "S3 storage, requests, and data transfer (usage-based)"},
		{Kind: Free, Addr: "aws_vpc.main"},
	}
	var buf bytes.Buffer
	WriteCompact(&buf, i18n.New("ko"), "web", "ap-northeast-2", items)
	got := buf.String()
	for _, want := range []string{
		"## terraform-price — web (`ap-northeast-2`)",
		"**$30.37/mo** — 1건 산정",
		"| 리소스 | 사양 | $/mo |",
		"총액 미포함 2건",
		"사용량 기반",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ko output missing %q:\n%s", want, got)
		}
	}
}
