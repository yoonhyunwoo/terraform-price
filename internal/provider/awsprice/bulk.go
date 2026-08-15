package awsprice

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/pricing"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

// bulkServiceCode maps Query API service codes to Bulk API codes where they differ.
var bulkServiceCode = map[string]string{
	"AWSKMS": "awskms",
	"AWSWAF": "awswaf",
}

// indexFields is the attribute vocabulary the bulk index can answer; queries
// filtering on any other field fall back to the Query API (GetProducts).
var indexFields = map[string]bool{
	"usagetype": true, "location": true, "instancetype": true,
	"operatingsystem": true, "preinstalledsw": true, "capacitystatus": true,
	"operation": true, "deploymentoption": true, "tenancy": true,
	"licensemodel": true, "group": true, "volumetype": true,
	"database_engine": true, "databasedition": true,
}

var (
	errBulkNoMatch      = errors.New("bulk: no price match")
	errBulkUnknownField = errors.New("bulk: filter field not indexed")
)

type priceDim struct {
	unit string
	usd  float64
}

type bulkProd struct {
	attrs  map[string]string // lowercased field -> raw value
	prices []priceDim        // OnDemand dimensions in file order
}

// Bulk prices queries from Price List Bulk API files, downloaded once per
// (service, region) into dir and parsed into an in-memory index. Wrap it in
// provider.Fallback so any bulk failure degrades to the Query API client.
type Bulk struct {
	client *Client
	dir    string
	ttl    time.Duration

	mu  sync.Mutex
	idx map[string]*bulkIndex
}

type bulkIndex struct {
	prods []bulkProd // products in file order
}

func NewBulk(c *Client, dir string, ttl time.Duration) *Bulk {
	return &Bulk{client: c, dir: dir, ttl: ttl, idx: map[string]*bulkIndex{}}
}

func (b *Bulk) UnitPrice(ctx context.Context, q provider.Query) (float64, string, error) {
	svc := bulkServiceCode[q.Service]
	if svc == "" {
		svc = q.Service
	}
	region := q.Region
	if region == "" {
		region = "us-east-1" // global services (e.g. Route53) price globally
	}
	for _, f := range q.Filters {
		if !indexFields[strings.ToLower(f.Field)] {
			return 0, "", fmt.Errorf("%w: %s", errBulkUnknownField, f.Field)
		}
	}
	key := svc + "|" + region
	b.mu.Lock()
	ix, ok := b.idx[key]
	b.mu.Unlock()
	if !ok {
		var err error
		ix, err = b.loadIndex(ctx, svc, region)
		if err != nil {
			return 0, "", fmt.Errorf("bulk %s %s: %w", svc, region, err)
		}
		b.mu.Lock()
		b.idx[key] = ix
		b.mu.Unlock()
	}
	return selectPrice(ix, q)
}

// selectPrice mirrors pickPrice's iteration contract: products in order, then
// OnDemand price dimensions in order; the first positive price with the
// preferred unit wins, otherwise the first positive price seen.
func selectPrice(ix *bulkIndex, q provider.Query) (float64, string, error) {
	var firstUSD float64
	var firstUnit string
	for _, p := range ix.prods {
		if !matchesAll(p, q.Filters) {
			continue
		}
		for _, d := range p.prices {
			if d.usd <= 0 {
				continue
			}
			if q.PreferUnit != "" && d.unit == q.PreferUnit {
				return d.usd, d.unit, nil
			}
			if firstUnit == "" {
				firstUSD, firstUnit = d.usd, d.unit
			}
		}
	}
	if firstUnit == "" {
		return 0, "", errBulkNoMatch
	}
	if q.PreferUnit != "" {
		// No dimension carried the preferred unit; defer to the Query API so
		// callers see its canonical error message.
		return 0, "", errBulkNoMatch
	}
	return firstUSD, firstUnit, nil
}

func matchesAll(p bulkProd, filters []provider.Filter) bool {
	for _, f := range filters {
		v, ok := p.attrs[strings.ToLower(f.Field)]
		if !ok || !strings.EqualFold(v, f.Value) {
			return false
		}
	}
	return true
}

func (b *Bulk) loadIndex(ctx context.Context, svc, region string) (*bulkIndex, error) {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(b.dir, svc+"_"+region+".json.gz")
	if fi, err := os.Stat(path); err != nil || time.Since(fi.ModTime()) > b.ttl {
		if err := b.download(ctx, svc, region, path); err != nil {
			// A stale local file beats a failed download: stability first.
			if _, serr := os.Stat(path); serr != nil {
				return nil, err
			}
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return parseBulkFile(gz)
}

func (b *Bulk) download(ctx context.Context, svc, region, path string) error {
	out, err := b.client.client.ListPriceLists(ctx, &pricing.ListPriceListsInput{
		ServiceCode:   aws.String(svc),
		CurrencyCode:  aws.String("USD"),
		RegionCode:    aws.String(region),
		EffectiveDate: aws.Time(time.Now().UTC()),
	})
	if err != nil {
		return err
	}
	if len(out.PriceLists) == 0 {
		return errors.New("no price list for effective date")
	}
	u, err := b.client.client.GetPriceListFileUrl(ctx, &pricing.GetPriceListFileUrlInput{
		PriceListArn: out.PriceLists[0].PriceListArn,
		FileFormat:   aws.String("JSON"),
	})
	if err != nil {
		return err
	}
	resp, err := http.Get(*u.Url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download price list: %s", resp.Status)
	}
	tmp := path + ".tmp"
	gzf, err := os.Create(tmp)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(gzf)
	_, copyErr := io.Copy(gz, resp.Body)
	closeErr := gz.Close()
	if cerr := gzf.Close(); cerr != nil && copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return os.Rename(tmp, path)
}

// parseBulkFile streams the price list file (products and terms are top-level
// objects keyed by SKU) preserving file order, which selectPrice depends on.
func parseBulkFile(r io.Reader) (*bulkIndex, error) {
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	ix := &bulkIndex{}
	skuIdx := map[string]int{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		switch key {
		case "products":
			if err := expectDelim(dec, '{'); err != nil {
				return nil, err
			}
			for dec.More() {
				skuTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				sku, _ := skuTok.(string)
				var p struct {
					Attributes map[string]json.RawMessage `json:"attributes"`
				}
				if err := dec.Decode(&p); err != nil {
					return nil, err
				}
				attrs := make(map[string]string, len(indexFields))
				for k, raw := range p.Attributes {
					lk := strings.ToLower(k)
					if !indexFields[lk] {
						continue
					}
					if s, err := strconv.Unquote(string(raw)); err == nil {
						attrs[lk] = s
					} else {
						attrs[lk] = string(raw)
					}
				}
				skuIdx[sku] = len(ix.prods)
				ix.prods = append(ix.prods, bulkProd{attrs: attrs})
			}
			if err := expectDelim(dec, '}'); err != nil {
				return nil, err
			}
		case "terms":
			if err := expectDelim(dec, '{'); err != nil {
				return nil, err
			}
			for dec.More() {
				skuTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				sku, _ := skuTok.(string)
				i, known := skuIdx[sku]
				if !known {
					if err := skipValue(dec); err != nil {
						return nil, err
					}
					continue
				}
				dims, err := parseTermDims(dec)
				if err != nil {
					return nil, err
				}
				ix.prods[i].prices = dims
			}
			if err := expectDelim(dec, '}'); err != nil {
				return nil, err
			}
		default:
			if err := skipValue(dec); err != nil {
				return nil, err
			}
		}
	}
	_, err := dec.Token()
	return ix, err
}

// parseTermDims consumes one terms[sku] value and returns its OnDemand price
// dimensions in file order.
func parseTermDims(dec *json.Decoder) ([]priceDim, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	var dims []priceDim
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		if key != "OnDemand" {
			if err := skipValue(dec); err != nil {
				return nil, err
			}
			continue
		}
		if err := expectDelim(dec, '{'); err != nil {
			return nil, err
		}
		for dec.More() {
			if _, err := dec.Token(); err != nil { // offer code, single in practice
				return nil, err
			}
			offer, err := parseOfferDims(dec)
			if err != nil {
				return nil, err
			}
			dims = append(dims, offer...)
		}
		if err := expectDelim(dec, '}'); err != nil {
			return nil, err
		}
	}
	return dims, expectDelim(dec, '}')
}

func parseOfferDims(dec *json.Decoder) ([]priceDim, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	var dims []priceDim
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		if key != "priceDimensions" {
			if err := skipValue(dec); err != nil {
				return nil, err
			}
			continue
		}
		if err := expectDelim(dec, '{'); err != nil {
			return nil, err
		}
		for dec.More() {
			if _, err := dec.Token(); err != nil { // dimension key
				return nil, err
			}
			var d struct {
				Unit         string `json:"unit"`
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			}
			if err := dec.Decode(&d); err != nil {
				return nil, err
			}
			p, _ := strconv.ParseFloat(d.PricePerUnit.USD, 64)
			dims = append(dims, priceDim{unit: d.Unit, usd: p})
		}
		if err := expectDelim(dec, '}'); err != nil {
			return nil, err
		}
	}
	return dims, expectDelim(dec, '}')
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, got %v", want, tok)
	}
	return nil
}

func skipValue(dec *json.Decoder) error {
	var raw json.RawMessage
	return dec.Decode(&raw)
}
