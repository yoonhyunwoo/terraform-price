// SPDX-License-Identifier: Apache-2.0
// Package funcs provides the HCL expression function table used by the
// resolver's evaluation context.
package funcs

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

var toSetFunc = function.New(&function.Spec{
	Params: []function.Parameter{{
		Name:             "v",
		Type:             cty.DynamicPseudoType,
		AllowDynamicType: true,
		AllowUnknown:     true,
	}},
	Type: func(args []cty.Value) (cty.Type, error) {
		val := args[0]
		switch {
		case val.Type().IsSetType():
			return val.Type(), nil
		case val.Type().IsListType():
			return cty.Set(val.Type().ElementType()), nil
		case val.Type().IsTupleType():
			elem := cty.DynamicPseudoType
			elems := val.AsValueSlice()
			if len(elems) > 0 {
				elem = elems[0].Type()
				for _, e := range elems[1:] {
					if e.Type() != elem {
						elem = cty.DynamicPseudoType
						break
					}
				}
			}
			return cty.Set(elem), nil
		default:
			return cty.NilType, fmt.Errorf("toset requires a list or set, got %s", val.Type().FriendlyName())
		}
	},
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		return cty.SetVal(args[0].AsValueSlice()), nil
	},
})

var toListFunc = function.New(&function.Spec{
	Params: []function.Parameter{{
		Name:             "v",
		Type:             cty.DynamicPseudoType,
		AllowDynamicType: true,
		AllowUnknown:     true,
	}},
	Type: func(args []cty.Value) (cty.Type, error) {
		val := args[0]
		switch {
		case val.Type().IsListType():
			return val.Type(), nil
		case val.Type().IsSetType():
			return cty.List(val.Type().ElementType()), nil
		case val.Type().IsTupleType():
			elem := cty.DynamicPseudoType
			elems := val.AsValueSlice()
			if len(elems) > 0 {
				elem = elems[0].Type()
				for _, e := range elems[1:] {
					if e.Type() != elem {
						elem = cty.DynamicPseudoType
						break
					}
				}
			}
			return cty.List(elem), nil
		default:
			return cty.NilType, fmt.Errorf("tolist requires a list or set, got %s", val.Type().FriendlyName())
		}
	},
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		val := args[0]
		elems := val.AsValueSlice()
		if len(elems) == 0 {
			return cty.ListValEmpty(retType.ElementType()), nil
		}
		return cty.ListVal(elems), nil
	},
})

var startsWithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "s", Type: cty.String},
		{Name: "prefix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		return cty.BoolVal(strings.HasPrefix(args[0].AsString(), args[1].AsString())), nil
	},
})

var endsWithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "s", Type: cty.String},
		{Name: "suffix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		return cty.BoolVal(strings.HasSuffix(args[0].AsString(), args[1].AsString())), nil
	},
})

var sumFunc = function.New(&function.Spec{
	Params: []function.Parameter{{
		Name: "list",
		Type: cty.List(cty.Number),
	}},
	Type: function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		list := args[0].AsValueSlice()
		if len(list) == 0 {
			return cty.NilVal, fmt.Errorf("cannot sum empty list")
		}
		total := list[0]
		for _, v := range list[1:] {
			total, _ = stdlib.Add(total, v)
		}
		return total, nil
	},
})

var lengthFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name:             "collection",
			Type:             cty.DynamicPseudoType,
			AllowDynamicType: true,
		},
	},
	Type: function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		val := args[0]
		switch {
		case val.Type() == cty.String:
			return cty.NumberIntVal(int64(len([]rune(val.AsString())))), nil
		case val.Type().IsObjectType() && !val.IsNull():
			// cty stdlib rejects objects; Terraform's length counts attributes.
			return cty.NumberIntVal(int64(len(val.AsValueMap()))), nil
		}
		return stdlib.LengthFunc.Call([]cty.Value{val})
	},
})

var lookupFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name: "inputMap",
			Type: cty.DynamicPseudoType,
		},
		{
			Name: "key",
			Type: cty.String,
		},
	},
	VarParam: &function.Parameter{
		Name: "default",
		Type: cty.DynamicPseudoType,
	},
	Type: function.StaticReturnType(cty.DynamicPseudoType),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		m, key := args[0], args[1]
		if m.Type().IsObjectType() && m.Type().HasAttribute(key.AsString()) {
			return m.GetAttr(key.AsString()), nil
		}
		if len(args) > 2 {
			return stdlib.LookupFunc.Call(args)
		}
		return stdlib.LookupFunc.Call(args)
	},
})

// Core returns the function table for expression evaluation.
func Core() map[string]function.Function {
	return map[string]function.Function{
		"length":       lengthFunc,
		"tolist":       toListFunc,
		"toset":        toSetFunc,
		"element":      stdlib.ElementFunc,
		"concat":       stdlib.ConcatFunc,
		"contains":     stdlib.ContainsFunc,
		"distinct":     stdlib.DistinctFunc,
		"flatten":      stdlib.FlattenFunc,
		"index":        stdlib.IndexFunc,
		"keys":         stdlib.KeysFunc,
		"values":       stdlib.ValuesFunc,
		"merge":        stdlib.MergeFunc,
		"reverse":      stdlib.ReverseListFunc,
		"slice":        stdlib.SliceFunc,
		"sort":         stdlib.SortFunc,
		"sum":          sumFunc,
		"range":        stdlib.RangeFunc,
		"zipmap":       stdlib.ZipmapFunc,
		"setunion":     stdlib.SetUnionFunc,
		"setproduct":   stdlib.SetProductFunc,
		"chunklist":    stdlib.ChunklistFunc,
		"setintersect": stdlib.SetIntersectionFunc,
		"setsubtract":  stdlib.SetSubtractFunc,

		"join":       stdlib.JoinFunc,
		"split":      stdlib.SplitFunc,
		"lower":      stdlib.LowerFunc,
		"upper":      stdlib.UpperFunc,
		"trim":       stdlib.TrimFunc,
		"trimprefix": stdlib.TrimPrefixFunc,
		"trimsuffix": stdlib.TrimSuffixFunc,
		"trimspace":  stdlib.TrimSpaceFunc,
		"replace":    stdlib.ReplaceFunc,
		"format":     stdlib.FormatFunc,
		"formatlist": stdlib.FormatListFunc,
		"startswith": startsWithFunc,
		"endswith":   endsWithFunc,
		"strrev":     stdlib.ReverseFunc,
		"indent":     stdlib.IndentFunc,
		"title":      stdlib.TitleFunc,

		"abs":    stdlib.AbsoluteFunc,
		"ceil":   stdlib.CeilFunc,
		"floor":  stdlib.FloorFunc,
		"log":    stdlib.LogFunc,
		"max":    stdlib.MaxFunc,
		"min":    stdlib.MinFunc,
		"pow":    stdlib.PowFunc,
		"signum": stdlib.SignumFunc,

		"jsonencode": stdlib.JSONEncodeFunc,
		"jsondecode": stdlib.JSONDecodeFunc,

		"coalesce":     stdlib.CoalesceFunc,
		"coalescelist": stdlib.CoalesceListFunc,
		"compact":      stdlib.CompactFunc,
		"lookup":       lookupFunc,
		"try":          tryfunc.TryFunc,
		"can":          tryfunc.CanFunc,
	}
}

var Add = stdlib.Add

// Scope builds an EvalContext with var/local objects and the function table.
func Scope(vars, locals map[string]cty.Value) *hcl.EvalContext {
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: Core(),
	}
	if len(vars) > 0 {
		ctx.Variables["var"] = cty.ObjectVal(vars)
	}
	if len(locals) > 0 {
		ctx.Variables["local"] = cty.ObjectVal(locals)
	}
	return ctx
}
