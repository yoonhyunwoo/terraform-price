package main

import (
	"strings"
	"testing"
)

func TestSatisfiesPessimistic(t *testing.T) {
	cases := []struct {
		pin  string
		ver  string
		want bool
	}{
		{"~> 5", "5.9.9", true},
		{"~> 5", "6.0.0", false}, // ~> 5 caps below the next major
		{"~> 5", "4.9.0", false},
		{"~> 5.5", "5.5.0", true},
		{"~> 5.5", "5.9.9", true},
		{"~> 5.5", "6.0.0", false},
		{"~> 5.5", "5.4.9", false},
		{"~> 1.0", "1.2.0", true},
		{"~> 5.5.1", "5.5.3", true},
		{"~> 5.5.1", "5.6.0", false},
	}
	for _, c := range cases {
		got := satisfiesPessimistic(strings.Split(c.pin[3:], "."), strings.Split(c.ver, "."))
		if got != c.want {
			t.Errorf("%s vs %s = %v, want %v", c.pin, c.ver, got, c.want)
		}
	}
}

func TestVersionLess(t *testing.T) {
	if !versionLess("5.9.9", "6.0.0") {
		t.Error("5.9.9 should be < 6.0.0")
	}
	if !versionLess("1.2.0", "1.10.0") {
		t.Error("1.2.0 should be < 1.10.0 (numeric, not lexicographic)")
	}
	if versionLess("1.2.0", "1.2.0") {
		t.Error("equal versions must not compare less")
	}
}
