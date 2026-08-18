package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/i18n"

	"github.com/yoonhyunwoo/terraform-price/internal/delta"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

type fakePricer struct{}

func (fakePricer) UnitPrice(_ context.Context, q provider.Query) (float64, string, error) {
	for _, f := range q.Filters {
		switch f.Field {
		case "instanceType":
			switch f.Value {
			case "t3.micro":
				return 0.0124, "Hrs", nil
			case "t3.medium":
				return 0.0496, "Hrs", nil
			}
		case "volumeApiName":
			if f.Value == "gp3" {
				return 0.08, "GB-Mo", nil
			}
		case "usagetype":
			switch f.Value {
			case "NatGateway-Hours":
				return 0.045, "Hrs", nil
			case "RDS:GP3-Storage":
				return 0.131, "GB-Mo", nil
			case "WebACLV2":
				return 5.0, "Month", nil
			case "RuleV2":
				return 1.0, "Month", nil
			}
		case "location":
			return 0.10, "Hrs", nil
		}
	}
	return 0, "", fmt.Errorf("no fake price for filters %+v", q.Filters)
}

func writeFixture(t *testing.T, dir, tf string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tf), 0o644); err != nil {
		t.Fatal(err)
	}
}

func monthlyOf(t *testing.T, items []output.CostItem, addr string) float64 {
	t.Helper()
	for _, it := range items {
		if it.Addr == addr {
			if it.Unresolved != "" {
				t.Fatalf("%s unresolved: %s", addr, it.Unresolved)
			}
			return it.Monthly
		}
	}
	t.Fatalf("addr %s not found", addr)
	return 0
}

// TestAnalyzeDeltaEndToEnd runs the extracted pipeline over two fixture
// directories (baseline vs proposed) and checks the delta classification.
func TestAnalyzeDeltaEndToEnd(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, `
resource "aws_instance" "web" {
  instance_type = "t3.micro"
}

resource "aws_nat_gateway" "nat" {
}
`)
	cur := t.TempDir()
	writeFixture(t, cur, `
resource "aws_instance" "web" {
  instance_type = "t3.medium"
}

resource "aws_ebs_volume" "data" {
  type = "gp3"
  size = 100
}
`)

	ctx := context.Background()
	baseItems, err := analyze(ctx, fakePricer{}, base)
	if err != nil {
		t.Fatalf("baseline analyze: %v", err)
	}
	curItems, err := analyze(ctx, fakePricer{}, cur)
	if err != nil {
		t.Fatalf("proposed analyze: %v", err)
	}

	rows, totals := delta.Compute(i18n.New(), baseItems, curItems)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(rows), rows)
	}
	want := []struct {
		addr string
		kind delta.Kind
	}{
		{"aws_ebs_volume.data", delta.Create},
		{"aws_instance.web", delta.Update},
		{"aws_nat_gateway.nat", delta.Delete},
	}
	for i, w := range want {
		if rows[i].Addr != w.addr || rows[i].Kind != w.kind {
			t.Errorf("row %d = %+v, want %s/%v", i, rows[i], w.addr, w.kind)
		}
	}
	webPrior := monthlyOf(t, baseItems, "aws_instance.web")
	webProp := monthlyOf(t, curItems, "aws_instance.web")
	if rows[1].Prior != webPrior || rows[1].Proposed != webProp {
		t.Errorf("update row = %+v, want prior %v proposed %v", rows[1], webPrior, webProp)
	}
	if rows[1].Delta != webProp-webPrior {
		t.Errorf("update Delta = %v, want %v", rows[1].Delta, webProp-webPrior)
	}
	if rows[1].Change != "t3.micro → t3.medium" {
		t.Errorf("update Change = %q", rows[1].Change)
	}
	if rows[0].Proposed != monthlyOf(t, curItems, "aws_ebs_volume.data") || rows[0].Delta != rows[0].Proposed {
		t.Errorf("create row = %+v", rows[0])
	}
	natPrior := monthlyOf(t, baseItems, "aws_nat_gateway.nat")
	if rows[2].Prior != natPrior || rows[2].Delta != -natPrior {
		t.Errorf("delete row = %+v, want prior %v", rows[2], natPrior)
	}

	sumDeltas := 0.0
	for _, r := range rows {
		if r.Kind != delta.NotEstimated {
			sumDeltas += r.Delta
		}
	}
	if totals.Delta != sumDeltas {
		t.Errorf("totals.Delta = %v, want row sum %v", totals.Delta, sumDeltas)
	}
	if totals.Delta != totals.Proposed-totals.Prior {
		t.Errorf("Delta %v != Proposed-Prior %v", totals.Delta, totals.Proposed-totals.Prior)
	}
	if totals.NotEstimated != 0 {
		t.Errorf("NotEstimated = %d, want 0", totals.NotEstimated)
	}
}
