package awsprice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	ptypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"

	"github.com/yoonhyunwoo/terraform-price/internal/provider"
)

type Client struct {
	client *pricing.Client
}

func NewClient(ctx context.Context) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	client := pricing.NewFromConfig(cfg, func(o *pricing.Options) {
		o.Region = "us-east-1"
	})
	return &Client{client: client}, nil
}

func (c *Client) UnitPrice(ctx context.Context, q provider.Query) (float64, string, error) {
	p, unit, err := c.query(ctx, q)
	if errors.Is(err, errNoPrice) {
		// DocDB/Neptune abbreviate multi-xlarge sizes on newer families
		// (db.r5.4xl) while r4 keeps full names — retry the abbreviated form.
		if alt, ok := abbrevUsagetype(q); ok {
			p, unit, err = c.query(ctx, alt)
		}
	}
	return p, unit, err
}

func (c *Client) query(ctx context.Context, q provider.Query) (float64, string, error) {
	filters := make([]ptypes.Filter, len(q.Filters))
	for i, f := range q.Filters {
		filters[i] = ptypes.Filter{
			Type:  ptypes.FilterTypeTermMatch,
			Field: aws.String(f.Field),
			Value: aws.String(f.Value),
		}
	}
	out, err := c.client.GetProducts(ctx, &pricing.GetProductsInput{
		ServiceCode: aws.String(q.Service),
		Filters:     filters,
	})
	if err != nil {
		return 0, "", err
	}
	return pickPrice(out.PriceList, q.Service, q.PreferUnit)
}

var multiXlarge = regexp.MustCompile(`^(.*\.\d+)xlarge$`)

// abbrevUsagetype rewrites the usagetype's multi-xlarge size to the
// abbreviated form (db.r5.4xlarge -> db.r5.4xl).
func abbrevUsagetype(q provider.Query) (provider.Query, bool) {
	alt := q
	alt.Filters = append([]provider.Filter(nil), q.Filters...)
	for i, f := range alt.Filters {
		if f.Field != "usagetype" {
			continue
		}
		if m := multiXlarge.FindStringSubmatch(f.Value); m != nil {
			alt.Filters[i].Value = m[1] + "xl"
			return alt, true
		}
	}
	return q, false
}

// errNoPrice marks a query that matched no positive OnDemand price, letting
// callers retry alternate usagetype spellings.
var errNoPrice = errors.New("no price match")

type priceListDoc struct {
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				Unit         string `json:"unit"`
				PricePerUnit struct {
					USD string `json:"USD"`
				} `json:"pricePerUnit"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

func pickPrice(rows []string, serviceCode, preferUnit string) (float64, string, error) {
	var firstPrice float64
	var firstUnit string
	for _, raw := range rows {
		var doc priceListDoc
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			continue
		}
		for _, term := range doc.Terms.OnDemand {
			for _, dim := range term.PriceDimensions {
				p, perr := strconv.ParseFloat(dim.PricePerUnit.USD, 64)
				if perr != nil || p <= 0 {
					continue
				}
				if preferUnit != "" && dim.Unit == preferUnit {
					return p, dim.Unit, nil
				}
				if firstUnit == "" {
					firstPrice, firstUnit = p, dim.Unit
				}
			}
		}
	}
	if firstUnit == "" {
		return 0, "", errNoPrice
	}
	if preferUnit != "" {
		return 0, "", fmt.Errorf("no OnDemand price with unit %q for %s (available unit: %s)", preferUnit, serviceCode, firstUnit)
	}
	return firstPrice, firstUnit, nil
}
