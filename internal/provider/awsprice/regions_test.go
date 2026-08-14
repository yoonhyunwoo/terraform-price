package awsprice

import (
	"context"
	"strings"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

type capturePricer struct {
	got provider.Query
}

func (c *capturePricer) UnitPrice(_ context.Context, q provider.Query) (float64, string, error) {
	c.got = q
	return 0.1, "Hrs", nil
}

func filterVal(q provider.Query, field string) (string, bool) {
	for _, f := range q.Filters {
		if f.Field == field {
			return f.Value, true
		}
	}
	return "", false
}

func TestComposeInjectsLocationAndPrefix(t *testing.T) {
	q, err := compose(provider.Query{
		Service: "AmazonEC2", Region: "ap-northeast-2",
		Filters: []provider.Filter{{Field: "usagetype", Value: "NatGateway-Hours"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loc, ok := filterVal(q, "location"); !ok || loc != "Asia Pacific (Seoul)" {
		t.Fatalf("location filter: want legacy value %q (cache keys depend on it), got %q ok=%v", "Asia Pacific (Seoul)", loc, ok)
	}
	if ut, ok := filterVal(q, "usagetype"); !ok || ut != "APN2-NatGateway-Hours" {
		t.Fatalf("usagetype: want APN2-NatGateway-Hours, got %q ok=%v", ut, ok)
	}
}

func TestComposeDifferentRegionsDiverge(t *testing.T) {
	a, err := compose(provider.Query{Region: "ap-northeast-2", Filters: []provider.Filter{{Field: "usagetype", Value: "X"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := compose(provider.Query{Region: "ap-northeast-1", Filters: []provider.Filter{{Field: "usagetype", Value: "X"}}})
	if err != nil {
		t.Fatal(err)
	}
	if filterString(a) == filterString(b) {
		t.Fatal("two regions must compose distinct filter sets or cache keys would collide")
	}
}

func filterString(q provider.Query) string {
	parts := make([]string, 0, len(q.Filters))
	for _, f := range q.Filters {
		parts = append(parts, f.Field+"="+f.Value)
	}
	return strings.Join(parts, ",")
}

func TestComposeUnknownRegion(t *testing.T) {
	_, err := compose(provider.Query{Region: "ap-outer-mongolia-1"})
	if err == nil || !strings.Contains(err.Error(), "unknown region") {
		t.Fatalf("want named unknown-region error, got %v", err)
	}
}

func TestComposerDelegatesComposed(t *testing.T) {
	cap := &capturePricer{}
	cp := NewComposer(cap)
	if _, _, err := cp.UnitPrice(context.Background(), provider.Query{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	if loc, ok := filterVal(cap.got, "location"); !ok || loc != "US East (N. Virginia)" {
		t.Fatalf("delegate saw uncomposed query: %+v", cap.got)
	}
}

func TestRegionTablesConsistent(t *testing.T) {
	if len(regionToLocation) != len(regionToUsagePrefix) {
		t.Fatalf("table drift: %d locations vs %d prefixes", len(regionToLocation), len(regionToUsagePrefix))
	}
	for r := range regionToLocation {
		if _, ok := regionToUsagePrefix[r]; !ok {
			t.Fatalf("region %q has location but no usagetype prefix", r)
		}
	}
}

func TestComposeUse1FamilyExceptions(t *testing.T) {
	cases := []struct {
		base, want string
	}{
		{"Aurora:StorageUsage", "Aurora:StorageUsage"},
		{"Aurora:StorageIOUsage", "Aurora:StorageIOUsage"},
		{"NatGateway-Hours", "NatGateway-Hours"},
		{"AWSSecretsManager-Secrets", "USE1-AWSSecretsManager-Secrets"},
		{"RDS:ProxyUsage", "USE1-RDS:ProxyUsage"},
	}
	for _, c := range cases {
		q, err := compose(provider.Query{Service: "AmazonEC2", Region: "us-east-1",
			Filters: []provider.Filter{{Field: "usagetype", Value: c.base}}})
		if err != nil {
			t.Fatalf("%s: %v", c.base, err)
		}
		if got, _ := filterVal(q, "usagetype"); got != c.want {
			t.Errorf("us-east-1 %s: want %q, got %q", c.base, c.want, got)
		}
		if loc, _ := filterVal(q, "location"); loc != "US East (N. Virginia)" {
			t.Errorf("us-east-1 location: got %q", loc)
		}
	}
}

func TestComposeDerivedRegionVocabulary(t *testing.T) {
	// Spot-check values frozen from the live Price List API.
	for region, wantLoc := range map[string]string{
		"us-east-2":    "US East (Ohio)",
		"ap-south-1":   "Asia Pacific (Mumbai)",
		"eu-west-2":    "EU (London)",
		"eu-central-1": "EU (Frankfurt)",
	} {
		q, err := compose(provider.Query{Region: region})
		if err != nil {
			t.Fatalf("%s: %v", region, err)
		}
		if got, _ := filterVal(q, "location"); got != wantLoc {
			t.Errorf("%s location: want %q, got %q", region, wantLoc, got)
		}
	}
	if _, err := compose(provider.Query{Region: "eu-central-1", Filters: []provider.Filter{{Field: "usagetype", Value: "RDS:ProxyUsage"}}}); err != nil {
		t.Fatal(err)
	}
	q, _ := compose(provider.Query{Region: "ap-south-1", Filters: []provider.Filter{{Field: "usagetype", Value: "NatGateway-Hours"}}})
	if got, _ := filterVal(q, "usagetype"); got != "APS3-NatGateway-Hours" {
		t.Errorf("Mumbai NAT prefix: want APS3-NatGateway-Hours, got %q", got)
	}
}
