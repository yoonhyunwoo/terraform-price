package resolver

import "github.com/zclconf/go-cty/cty"

// Str returns v as a Go string when it is a known, non-null cty string.
func Str(v cty.Value) (string, bool) {
	if !v.IsKnown() || v.IsNull() || v.Type() != cty.String {
		return "", false
	}
	return v.AsString(), true
}

// Num returns v as a float64 when it is a known, non-null cty number.
func Num(v cty.Value) (float64, bool) {
	if !v.IsKnown() || v.IsNull() || v.Type() != cty.Number {
		return 0, false
	}
	f, _ := v.AsBigFloat().Float64()
	return f, true
}

// Bool returns v's value when it is a known, non-null cty bool.
func Bool(v cty.Value) bool {
	return v.IsKnown() && !v.IsNull() && v.Type() == cty.Bool && v.True()
}
