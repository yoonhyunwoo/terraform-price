package awsprice

import (
	"fmt"
	"strings"
	"testing"
)

func row(unit, usd string) string {
	return fmt.Sprintf(`{"terms":{"OnDemand":{"x":{"priceDimensions":{"d":{"unit":%q,"pricePerUnit":{"USD":%q}}}}}}}`, unit, usd)
}

func TestPickPricePrefersRequestedUnit(t *testing.T) {
	rows := []string{row("GB-Mo", "0.10"), row("Hrs", "0.05")}
	p, unit, err := pickPrice(rows, "AmazonEC2", "Hrs")
	if err != nil || unit != "Hrs" || p != 0.05 {
		t.Fatalf("want 0.05 Hrs, got %v %q %v", p, unit, err)
	}
}

func TestPickPriceFailsOnUnitMismatch(t *testing.T) {
	rows := []string{row("GB-Mo", "0.10"), row("IOs", "0.20")}
	_, _, err := pickPrice(rows, "AmazonRDS", "Hrs")
	if err == nil || !strings.Contains(err.Error(), "Hrs") || !strings.Contains(err.Error(), "GB-Mo") {
		t.Fatalf("unit mismatch must fail naming both units, got %v", err)
	}
}

func TestPickPriceFallbackWithoutPreference(t *testing.T) {
	rows := []string{row("GB-Mo", "0.10")}
	p, unit, err := pickPrice(rows, "AmazonEC2", "")
	if err != nil || unit != "GB-Mo" || p != 0.10 {
		t.Fatalf("want first positive price, got %v %q %v", p, unit, err)
	}
}

func TestPickPriceSkipsZeroAndGarbage(t *testing.T) {
	rows := []string{`not json`, row("Hrs", "0.00"), row("Hrs", "0.05")}
	p, unit, err := pickPrice(rows, "AmazonEC2", "")
	if err != nil || unit != "Hrs" || p != 0.05 {
		t.Fatalf("want 0.05 after skipping garbage/zero, got %v %q %v", p, unit, err)
	}
}

func TestPickPriceNoMatch(t *testing.T) {
	if _, _, err := pickPrice(nil, "AmazonEC2", ""); err == nil {
		t.Fatal("empty price list must error")
	}
}
