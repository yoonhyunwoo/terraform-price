package resolver

import (
	"testing"
)

func TestResolveFunctionExpr(t *testing.T) {
	r := newResolver(t, `
		names = ["a", "b", "c"]
		env   = "prod"
	`)
	tests := []struct {
		expr string
		want int64
	}{
		{`length(var.names)`, 3},
		{`length(toset(var.names))`, 3},
		{`length(var.env)`, 4},
	}
	for _, tt := range tests {
		v, ok := r.ResolveExpr(parseExpr(t, tt.expr))
		if !ok {
			t.Fatalf(`%s: resolve failed`, tt.expr)
		}
		got, _ := v.AsBigFloat().Int64()
		if got != tt.want {
			t.Fatalf(`%s: want %d, got %d`, tt.expr, tt.want, got)
		}
	}
}

func TestResolveCountWithLength(t *testing.T) {
	r := newResolver(t, `
		subnets = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
	`)
	v, ok := r.ResolveExpr(parseExpr(t, `length(var.subnets)`))
	if !ok {
		t.Fatal(`length(var.subnets): resolve failed`)
	}
	got, _ := v.AsBigFloat().Int64()
	if got != 3 {
		t.Fatalf(`want 3, got %d`, got)
	}
}

func TestResolveIndexAfterFunction(t *testing.T) {
	r := newResolver(t, `
		zones = ["a", "b", "c"]
	`)
	v, ok := r.ResolveExpr(parseExpr(t, `sort(var.zones)[0]`))
	if !ok || v.AsString() != "a" {
		t.Fatalf(`sort(var.zones)[0]: want a/true, got %v/%v`, v, ok)
	}
}

func TestResolveConcatAndIndex(t *testing.T) {
	r := newResolver(t, `
		a = ["x"]
		b = ["y", "z"]
	`)
	v, ok := r.ResolveExpr(parseExpr(t, `concat(var.a, var.b)[1]`))
	if !ok || v.AsString() != "y" {
		t.Fatalf(`concat(var.a, var.b)[1]: want y/true, got %v/%v`, v, ok)
	}
}

func TestResolveMergeLookup(t *testing.T) {
	r := newResolver(t, `
		tags = { Name = "web" }
		defaults = { Env = "prod" }
	`)
	v, ok := r.ResolveExpr(parseExpr(t, `lookup(merge(var.defaults, var.tags), "Name")`))
	if !ok || v.AsString() != "web" {
		t.Fatalf(`lookup(merge(...)): want web/true, got %v/%v`, v, ok)
	}
}

func TestResolveTosetForEach(t *testing.T) {
	r := newResolver(t, `
		envs = ["dev", "stg", "prod"]
	`)
	v, ok := r.ResolveExpr(parseExpr(t, `toset(var.envs)`))
	if !ok {
		t.Fatal(`toset(var.envs): resolve failed`)
	}
	if !v.Type().IsSetType() {
		t.Fatalf(`toset should return a set, got %s`, v.Type().FriendlyName())
	}
	if l, _ := v.Length().AsBigFloat().Int64(); l != 3 {
		t.Fatalf(`toset length: want 3, got %d`, l)
	}
}
