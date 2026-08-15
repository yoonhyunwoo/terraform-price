package resolver

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// coerceToDeclaredType is the single boundary every incoming value — tfvars,
// module inputs, variable defaults — passes: convert to the variable's
// declared type constraint, exactly what Terraform core does before the value
// reaches evaluation (configschema coercion). Untyped variables and failed
// conversions keep the raw value: partial information beats an error here.
// Per cross-domain precedent (Blink NativeValueTraits, Calcite TypeCoercion)
// the entry kinds differ only in the value they carry, never in the gate.
func coerceToDeclaredType(v cty.Value, typeExpr hcl.Expression) cty.Value {
	if !v.IsKnown() || v.IsNull() || typeExpr == nil {
		return v
	}
	t, err := typeexpr.TypeConstraint(typeExpr)
	if err != nil {
		return v
	}
	if cv, err := convert.Convert(v, t); err == nil {
		return cv
	}
	return v
}
