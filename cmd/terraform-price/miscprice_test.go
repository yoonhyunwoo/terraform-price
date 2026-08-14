package main

import (
	"context"
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

// WAFv2 ACLs price the $5/mo web ACL plus $1/mo per rule; KMS keys are
// $1/mo via the region-code-prefixed usagetype; Route53 zones use the
// global unprefixed HostedZone row.
func TestWAFKMSRoute53Priced(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.tf": `
resource "aws_wafv2_web_acl" "acl" {
  name  = "x"
  scope = "REGIONAL"
  rules = [
    { name = "a" },
    { name = "b" },
    { name = "c" },
  ]
}
resource "aws_kms_key" "k" {
  description = "x"
}
resource "aws_route53_zone" "z" {
  name = "example.com"
}
`,
	})
	items, err := analyze(context.Background(), globalFakePricer{}, root)
	if err != nil {
		t.Fatal(err)
	}
	acl := itemWithSpec(t, items, "aws_wafv2_web_acl.acl", "web ACL (+3 rules)")
	if !approx(acl.Monthly, 5.0) {
		t.Fatalf("acl = %+v, want $5", acl)
	}
	rules := itemWithSpec(t, items, "aws_wafv2_web_acl.acl", "3 WAF rules")
	if !approx(rules.Monthly, 3.0) {
		t.Fatalf("rules = %+v, want $3", rules)
	}
	key := itemWithSpec(t, items, "aws_kms_key.k", "customer managed key")
	if !approx(key.Monthly, 1.0) {
		t.Fatalf("key = %+v, want $1", key)
	}
	zone := itemWithSpec(t, items, "aws_route53_zone.z", "hosted zone (first 25 tier)")
	if !approx(zone.Monthly, 0.5) {
		t.Fatalf("zone = %+v, want $0.50", zone)
	}
}

type globalFakePricer struct {
	fakePricer
}

func (globalFakePricer) UnitPrice(ctx context.Context, q provider.Query) (float64, string, error) {
	for _, f := range q.Filters {
		if f.Field == "usagetype" {
			switch f.Value {
			case "ap-northeast-2-KMS-Keys":
				return 1.0, "Keys", nil
			case "HostedZone":
				return 0.5, "HostedZone", nil
			}
		}
	}
	return fakePricer{}.UnitPrice(ctx, q)
}
