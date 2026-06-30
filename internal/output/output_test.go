package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteMarkdownFourWayPartition(t *testing.T) {
	items := []CostItem{
		{Kind: Fixed, Addr: "aws_instance.a", Spec: "t3.micro", UnitPrice: 0.01, Unit: "Hrs", Monthly: 7.30},
		{Kind: Fixed, Addr: "aws_instance.b", Unresolved: "instance_type 미해석"},
		{Kind: Variable, Addr: "aws_s3_bucket.x", Type: "aws_s3_bucket", Note: "usage 기반"},
		{Kind: Free, Addr: "aws_iam_role.r", Type: "aws_iam_role", Note: "무과금"},
		{Kind: Unsupported, Addr: "aws_widget.w", Type: "aws_widget", Note: "미지원"},
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, "svc", "ap-northeast-2", items)
	out := buf.String()

	if !strings.Contains(out, "7.30") {
		t.Errorf("fixed priced monthly missing:\n%s", out)
	}
	if !strings.Contains(out, "고정비 합계") {
		t.Errorf("fixed total row missing")
	}
	if !strings.Contains(out, "instance_type 미해석") {
		t.Errorf("unresolved fixed item not shown")
	}
	if !strings.Contains(out, "유동비 (usage") {
		t.Errorf("variable (usage) section missing")
	}
	if !strings.Contains(out, "aws_s3_bucket.x") {
		t.Errorf("variable item not surfaced")
	}
	if !strings.Contains(out, "미지원 과금 리소스") {
		t.Errorf("unsupported gap section missing")
	}
	if !strings.Contains(out, "aws_widget.w") {
		t.Errorf("unsupported (gap) item not surfaced — silent-drop regression")
	}
	if !strings.Contains(out, "무과금 1건") {
		t.Errorf("free count missing in footer")
	}
	if !strings.Contains(out, "미지원 과금 1건") {
		t.Errorf("unsupported count missing in footer")
	}
	if strings.Contains(out, "고정비 합계") && strings.Contains(out, "0.00") && !strings.Contains(out, "7.30") {
		t.Errorf("free/variable/unsupported leaked into fixed total")
	}
}

func TestWriteMarkdownNoUnsupportedOmitsGapSection(t *testing.T) {
	var buf bytes.Buffer
	WriteMarkdown(&buf, "svc", "ap-northeast-2", []CostItem{
		{Kind: Fixed, Addr: "aws_instance.a", Spec: "t3.micro", UnitPrice: 0.01, Unit: "Hrs", Monthly: 7.3},
	})
	out := buf.String()
	if strings.Contains(out, "미지원 과금 리소스") {
		t.Errorf("gap section should be omitted when no unsupported items")
	}
}
