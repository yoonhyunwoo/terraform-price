package awsprice

import (
	"context"
	"fmt"
	"strings"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

// Derived from the live Price List API (GetProducts AmazonEC2, 2026-08-14):
// every region the pricing dataset itself covers, including the fact that
// eu-central-1's location string is "EU (Frankfurt)", not "Europe (Frankfurt)".
// Derived from the live Price List API (GetProducts AmazonEC2, 2026-08-14):
// every region the pricing dataset itself covers, including the fact that
// eu-central-1's location string is "EU (Frankfurt)", not "Europe (Frankfurt)".
var regionToLocation = map[string]string{
	"af-south-1":      "Africa (Cape Town)",
	"ap-east-1":       "Asia Pacific (Hong Kong)",
	"ap-east-2":       "Asia Pacific (Taipei)",
	"ap-northeast-1":  "Asia Pacific (Tokyo)",
	"ap-northeast-2":  "Asia Pacific (Seoul)",
	"ap-northeast-3":  "Asia Pacific (Osaka)",
	"ap-south-1":      "Asia Pacific (Mumbai)",
	"ap-south-2":      "Asia Pacific (Hyderabad)",
	"ap-southeast-1":  "Asia Pacific (Singapore)",
	"ap-southeast-2":  "Asia Pacific (Sydney)",
	"ap-southeast-3":  "Asia Pacific (Jakarta)",
	"ap-southeast-4":  "Asia Pacific (Melbourne)",
	"ap-southeast-5":  "Asia Pacific (Malaysia)",
	"ap-southeast-6":  "Asia Pacific (New Zealand)",
	"ap-southeast-7":  "Asia Pacific (Thailand)",
	"ca-central-1":    "Canada (Central)",
	"ca-west-1":       "Canada West (Calgary)",
	"cn-north-1":      "China (Beijing)",
	"cn-northwest-1":  "China (Ningxia)",
	"eu-central-1":    "EU (Frankfurt)",
	"eu-central-2":    "Europe (Zurich)",
	"eu-north-1":      "EU (Stockholm)",
	"eu-south-1":      "EU (Milan)",
	"eu-south-2":      "Europe (Spain)",
	"eu-west-1":       "EU (Ireland)",
	"eu-west-2":       "EU (London)",
	"eu-west-3":       "EU (Paris)",
	"il-central-1":    "Israel (Tel Aviv)",
	"me-central-1":    "Middle East (UAE)",
	"me-south-1":      "Middle East (Bahrain)",
	"mx-central-1":    "Mexico (Central)",
	"sa-east-1":       "South America (Sao Paulo)",
	"us-east-1":       "US East (N. Virginia)",
	"us-east-2":       "US East (Ohio)",
	"us-gov-east-1":   "AWS GovCloud (US-East)",
	"us-gov-west-1":   "AWS GovCloud (US-West)",
	"us-west-1":       "US West (N. California)",
	"us-west-2":       "US West (Oregon)",
	"us-west-2-lax-1": "US West (Los Angeles)",
}

var regionToUsagePrefix = map[string]string{
	"af-south-1":      "AFS1",
	"ap-east-1":       "APE1",
	"ap-east-2":       "APE2",
	"ap-northeast-1":  "APN1",
	"ap-northeast-2":  "APN2",
	"ap-northeast-3":  "APN3",
	"ap-south-1":      "APS3",
	"ap-south-2":      "APS5",
	"ap-southeast-1":  "APS1",
	"ap-southeast-2":  "APS2",
	"ap-southeast-3":  "APS4",
	"ap-southeast-4":  "APS6",
	"ap-southeast-5":  "APS7",
	"ap-southeast-6":  "APS8",
	"ap-southeast-7":  "APS9",
	"ca-central-1":    "CAN1",
	"ca-west-1":       "CAN2",
	"cn-north-1":      "CNN1",
	"cn-northwest-1":  "CNW1",
	"eu-central-1":    "EUC1",
	"eu-central-2":    "EUC2",
	"eu-north-1":      "EUN1",
	"eu-south-1":      "EUS1",
	"eu-south-2":      "EUS2",
	"eu-west-1":       "EU",
	"eu-west-2":       "EUW2",
	"eu-west-3":       "EUW3",
	"il-central-1":    "ILC1",
	"me-central-1":    "MEC1",
	"me-south-1":      "MES1",
	"mx-central-1":    "MXC1",
	"sa-east-1":       "SAE1",
	"us-east-1":       "USE1",
	"us-east-2":       "USE2",
	"us-gov-east-1":   "UGE1",
	"us-gov-west-1":   "UGW1",
	"us-west-1":       "USW1",
	"us-west-2":       "USW2",
	"us-west-2-lax-1": "LAX1",
}

// The pricing dataset prefixes usagetype values with the region code everywhere
// except a handful of us-east-1 product families, which ship unprefixed.
var usEast1UnprefixedUsagetypes = map[string]bool{
	"Aurora:IO-OptimizedStorageUsage": true,
	"Aurora:StorageIOUsage":           true,
	"Aurora:StorageUsage":             true,
	"NatGateway-Hours":                true,
}

// usEast1UnprefixedUsagePrefixes extends the exception to usagetype families:
// instance/node usage rows ship unprefixed in us-east-1 across RDS, DocDB,
// Neptune and ElastiCache (live GetProducts survey, 2026-08-15), while aux
// rows (ExtendedSupport, Neptune NCU) stay USE1- prefixed.
var usEast1UnprefixedUsagePrefixes = []string{"InstanceUsage:", "NodeUsage:"}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// compose must run before provider.Cached: the location filter it injects is
// part of the cache key, so regions never collide on one key.
func compose(q provider.Query) (provider.Query, error) {
	if q.Region == "" {
		// Global services (Route53 zones): no location filter, usagetype
		// used verbatim.
		return q, nil
	}
	loc, ok := regionToLocation[q.Region]
	if !ok {
		return provider.Query{}, fmt.Errorf("unknown region %q (%d regions supported)", q.Region, len(regionToLocation))
	}
	prefix := regionToUsagePrefix[q.Region]
	filters := make([]provider.Filter, 0, len(q.Filters)+1)
	for _, f := range q.Filters {
		if f.Field == "usagetype" {
			switch {
			case q.Region == "us-east-1" && usEast1UnprefixedUsagetypes[f.Value],
				q.Region == "us-east-1" && hasAnyPrefix(f.Value, usEast1UnprefixedUsagePrefixes):
			case strings.HasPrefix(f.Value, q.Region+"-"):
				// KMS ships usagetypes prefixed with the region code
				// (ap-northeast-2-KMS-Keys), not the short form.
			default:
				f.Value = prefix + "-" + f.Value
			}
		}
		filters = append(filters, f)
	}
	filters = append(filters, provider.Filter{Field: "location", Value: loc})
	return provider.Query{Service: q.Service, Region: q.Region, Filters: filters, PreferUnit: q.PreferUnit}, nil
}

type Composer struct {
	inner provider.Pricer
}

func NewComposer(inner provider.Pricer) *Composer {
	return &Composer{inner: inner}
}

func (c *Composer) UnitPrice(ctx context.Context, q provider.Query) (float64, string, error) {
	q, err := compose(q)
	if err != nil {
		return 0, "", err
	}
	return c.inner.UnitPrice(ctx, q)
}
