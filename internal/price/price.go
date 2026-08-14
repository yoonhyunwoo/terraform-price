package price

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	ptypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

type Pricer interface {
	UnitPrice(ctx context.Context, serviceCode string, filters []ptypes.Filter, preferUnit string) (float64, string, error)
}

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

type Client struct {
	client *pricing.Client
}

func NewClient(ctx context.Context, profile string) (*Client, error) {
	opts := []func(*config.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	client := pricing.NewFromConfig(cfg, func(o *pricing.Options) {
		o.Region = "us-east-1"
	})
	return &Client{client: client}, nil
}

func (c *Client) UnitPrice(ctx context.Context, serviceCode string, filters []ptypes.Filter, preferUnit string) (float64, string, error) {
	out, err := c.client.GetProducts(ctx, &pricing.GetProductsInput{
		ServiceCode: aws.String(serviceCode),
		Filters:     filters,
	})
	if err != nil {
		return 0, "", err
	}
	return pickPrice(out.PriceList, serviceCode, preferUnit)
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
		return 0, "", fmt.Errorf("no price match")
	}
	if preferUnit != "" {
		return 0, "", fmt.Errorf("no OnDemand price with unit %q for %s (available unit: %s)", preferUnit, serviceCode, firstUnit)
	}
	return firstPrice, firstUnit, nil
}
