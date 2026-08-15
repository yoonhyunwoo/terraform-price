package awsprice

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

// Fixture mirrors the bulk price list file shape: top-level products and terms
// maps keyed by SKU, tiered OnDemand dimensions, mixed-case attribute keys.
const fixture = `{
  "formatVersion": "v1.0",
  "products": {
    "SKU1": {
      "productFamily": "Compute Instance",
      "attributes": {
        "ServiceCode": "AmazonEKS",
        "location": "Asia Pacific (Seoul)",
        "instanceType": "t3.medium",
        "usagetype": "APN2-Use of EKS-Hours:perHour",
        "operation": "RunInstances"
      }
    },
    "SKU2": {
      "attributes": {
        "location": "Asia Pacific (Seoul)",
        "usagetype": "APN2-HostedZone",
        " tier": "ignoreme"
      }
    }
  },
  "terms": {
    "SKU1": {
      "OnDemand": {
        "SKU1.XYZ.OnDemand": {
          "priceDimensions": {
            "SKU1.XYZ.OnDemand.1": {
              "unit": "Hrs",
              "pricePerUnit": {"USD": "0.07"}
            }
          }
        }
      }
    },
    "SKU2": {
      "OnDemand": {
        "SKU2.XYZ.OnDemand": {
          "priceDimensions": {
            "SKU2.XYZ.OnDemand.0": {
              "unit": "HostedZone",
              "pricePerUnit": {"USD": "0.50"}
            },
            "SKU2.XYZ.OnDemand.1": {
              "unit": "HostedZone",
              "pricePerUnit": {"USD": "0.10"}
            }
          }
        }
      },
      "Reserved": {
        "SKU2.XYZ.Reserved": {
          "priceDimensions": {
            "SKU2.XYZ.Reserved.1": {
              "unit": "Hrs",
              "pricePerUnit": {"USD": "99"}
            }
          }
        }
      }
    },
    "SKU3-UNKNOWN": {
      "OnDemand": {
        "X": {
          "priceDimensions": {
            "Y": {"unit": "Hrs", "pricePerUnit": {"USD": "1"}}
          }
        }
      }
    }
  }
}`

func TestBulkParseAndSelect(t *testing.T) {
	ix, err := parseBulkFile(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.prods) != 2 {
		t.Fatalf("indexed %d products, want 2 (unknown SKU must not become a product)", len(ix.prods))
	}
	q := provider.Query{
		Service: "AmazonEKS", Region: "ap-northeast-2", PreferUnit: "Hrs",
		Filters: []provider.Filter{
			{Field: "instanceType", Value: "T3.MEDIUM"}, // case-insensitive field + value
			{Field: "usagetype", Value: "APN2-Use of EKS-Hours:perHour"},
		},
	}
	p, unit, err := selectPrice(ix, q)
	if err != nil || unit != "Hrs" || p != 0.07 {
		t.Fatalf("selectPrice = %v %q %v, want 0.07 Hrs", p, unit, err)
	}
	// Tiered dims: first file-order dimension with the preferred unit wins.
	q2 := provider.Query{PreferUnit: "HostedZone",
		Filters: []provider.Filter{{Field: "usagetype", Value: "APN2-HostedZone"}}}
	p2, unit2, err := selectPrice(ix, q2)
	if err != nil || unit2 != "HostedZone" || p2 != 0.50 {
		t.Fatalf("tiered selectPrice = %v %q %v, want 0.50 HostedZone", p2, unit2, err)
	}
	// No match -> sentinel error (caller falls back to the Query API).
	if _, _, err := selectPrice(ix, provider.Query{Filters: []provider.Filter{{Field: "usagetype", Value: "nope"}}}); err == nil {
		t.Fatal("expected no-match error")
	}
	// Non-indexed field -> caller falls back before scanning.
	b := NewBulk(nil, t.TempDir(), time.Hour)
	if _, _, err := b.UnitPrice(context.Background(), provider.Query{
		Service: "AmazonEKS",
		Filters: []provider.Filter{{Field: "eksproducttype", Value: "x"}},
	}); err == nil || !strings.Contains(err.Error(), "not indexed") {
		t.Fatalf("expected not-indexed error, got %v", err)
	}
}

func TestBulkServesFromDiskFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bulk"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "bulk", "AmazonEKS_ap-northeast-2.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(fixture)); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	f.Close()
	b := NewBulk(nil, filepath.Join(dir, "bulk"), time.Hour) // nil client: download must not be attempted
	p, unit, err := b.UnitPrice(context.Background(), provider.Query{
		Service: "AmazonEKS", Region: "ap-northeast-2", PreferUnit: "Hrs",
		Filters: []provider.Filter{{Field: "usagetype", Value: "APN2-Use of EKS-Hours:perHour"}},
	})
	if err != nil || unit != "Hrs" || p != 0.07 {
		t.Fatalf("UnitPrice = %v %q %v, want 0.07 Hrs from disk file", p, unit, err)
	}
	// Global service: empty region maps to us-east-1 file.
	if err := os.WriteFile(filepath.Join(dir, "bulk", "AmazonRoute53_us-east-1.json.gz"), gzipBytes(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.UnitPrice(context.Background(), provider.Query{
		Service: "AmazonRoute53",
	}); err != nil {
		t.Fatalf("global query failed: %v", err)
	}
}

func TestBulkServiceCodeMap(t *testing.T) {
	if bulkServiceCode["AWSKMS"] != "awskms" || bulkServiceCode["AWSWAF"] != "awswaf" {
		t.Fatalf("mapping = %v", bulkServiceCode)
	}
}

func gzipBytes(s string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte(s))
	gz.Close()
	return buf.Bytes()
}
