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

func TestSatisfiesAll(t *testing.T) {
	cases := []struct {
		v, constraint string
		want          bool
	}{
		{"5.50.2", "~> 5.50.0", true},
		{"5.51.0", "~> 5.50.0", false}, // ~> x.y.z caps the minor
		{"6.1.0", "~> 5.50.0", false},
		{"5.60.0", ">= 5.60.0", true},
		{"5.50.0", ">= 5.60.0", false},
		{"5.70.1", ">= 5.60.0, < 6.0.0", true},
		{"6.0.0", ">= 5.60.0, < 6.0.0", false},
		{"5.50.1", "5.50.1", true}, // bare = exact
		{"5.50.2", "5.50.1", false},
		{"1.9.0", "= 1.9.0", true},
		{"1.9.1", "!= 1.9.1", false},
		{"1.9.0", "!= 1.9.1", true},
		{"6.20.1", "<= 6.20.1", true},
		{"6.20.2", "<= 6.20.1", false},
	}
	for _, c := range cases {
		if got := satisfiesAll(c.v, c.constraint); got != c.want {
			t.Errorf("satisfiesAll(%q, %q) = %v, want %v", c.v, c.constraint, got, c.want)
		}
	}
}
