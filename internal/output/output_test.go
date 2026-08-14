package output

import (
	"bytes"
	"strings"
	"testing"
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
	WriteMarkdown(&buf, "svc", "ap-northeast-2", items)
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
	WriteMarkdown(&buf, "svc", "ap-northeast-2", []CostItem{
		{Kind: Fixed, Addr: "aws_instance.a", Spec: "t3.micro", UnitPrice: 0.01, Unit: "Hrs", Monthly: 7.3},
	})
	out := buf.String()
	if strings.Contains(out, "## Unsupported") {
		t.Errorf("gap section should be omitted when no unsupported items")
	}
}
