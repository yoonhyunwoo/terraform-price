package main

import (
	"testing"

	"github.com/yoonhyunwoo/terraform-price/internal/mapper"
	"github.com/yoonhyunwoo/terraform-price/internal/output"
)

func TestClassifyItemWiring(t *testing.T) {
	cases := []struct {
		kind mapper.Kind
		want output.Kind
	}{
		{mapper.KindVariable, output.Variable},
		{mapper.KindFree, output.Free},
		{mapper.KindUnsupported, output.Unsupported},
		{mapper.Kind(99), output.Unsupported},
	}
	for _, c := range cases {
		got := classifyItem(c.kind, "addr", "aws_x", "note")
		if got.Kind != c.want {
			t.Errorf("kind %v: want output %v, got %v", c.kind, c.want, got.Kind)
		}
		if got.Addr != "addr" || got.Type != "aws_x" || got.Note != "note" {
			t.Errorf("kind %v: fields not propagated: %+v", c.kind, got)
		}
	}
}
