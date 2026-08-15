package resolver

import (
	"strconv"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

// Str returns v as a Go string when it is a known, non-null cty string.
func Str(v cty.Value) (string, bool) {
	if !v.IsKnown() || v.IsNull() || v.Type() != cty.String {
		return "", false
	}
	return v.AsString(), true
}

// Num returns v as a float64 when it is a known, non-null cty number.
// Num reads a numeric attribute the way the AWS provider would: numbers
// pass through and numeric strings ("50") coerce.
func Num(v cty.Value) (float64, bool) {
	if !v.IsKnown() || v.IsNull() {
		return 0, false
	}
	if v.Type() == cty.String {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v.AsString()), 64); err == nil {
			return f, true
		}
		return 0, false
	}
	if v.Type() != cty.Number {
		return 0, false
	}
	f, _ := v.AsBigFloat().Float64()
	return f, true
}

// Bool reads an attribute the way the AWS provider would: booleans pass
// through, and string-armed values ("true"/"1") coerce — variable type
// constraints legitimately deliver bool-looking values as strings.
func Bool(v cty.Value) bool {
	if !v.IsKnown() || v.IsNull() {
		return false
	}
	if v.Type() == cty.Bool {
		return v.True()
	}
	if v.Type() == cty.String {
		switch v.AsString() {
		case "true", "1":
			return true
		}
	}
	return false
}
