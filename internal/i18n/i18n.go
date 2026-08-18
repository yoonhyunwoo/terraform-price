// Package i18n holds the message catalogs and resolves the report language.
// English (active.en.json) is the source of truth: keys may only be added or
// reworded there; every other locale file must carry the same key set with the
// same {{.Placeholder}} names (enforced by TestLocalesComplete).
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed active.en.json active.ko.json
var files embed.FS

// Languages is the single registry of supported locales; --lang validates
// against it and TestLocalesComplete asserts an active.<code>.json exists
// for every entry. Keep it in sync only here (AdGuardHome's duplicated
// whitelist is the counter-example).
var Languages = []string{"en", "ko"}

// Message keys. English strings live in active.en.json, not in code.
const (
	MsgTitle                 = "title"
	MsgTotal                 = "total"
	MsgNotInTotal            = "not_in_total"
	MsgMonthlyChange         = "monthly_cost_change"
	MsgMonthlyChangeVs       = "monthly_cost_change_vs"
	MsgHeadlineIncrease      = "headline_increase"
	MsgHeadlineDecrease      = "headline_decrease"
	MsgHeadlineNoChange      = "headline_no_change"
	MsgColResource           = "col_resource"
	MsgColSpec               = "col_spec"
	MsgColChange             = "col_change"
	MsgColNewMonthly         = "col_new_monthly"
	MsgColChangeMonthly      = "col_change_monthly"
	MsgColType               = "col_type"
	MsgColNotes              = "col_notes"
	MsgColUnitPrice          = "col_unit_price"
	MsgColUnit               = "col_unit"
	MsgColMonthly            = "col_monthly"
	MsgColPrior              = "col_prior"
	MsgColProposed           = "col_proposed"
	MsgColDelta              = "col_delta"
	MsgFixedSection          = "fixed_section"
	MsgVariableSection       = "variable_section"
	MsgUnsupportedSection    = "unsupported_section"
	MsgFreeSection           = "free_section"
	MsgFixedTotal            = "fixed_total"
	MsgCaveat                = "caveat"
	MsgEmbeddedCatalog       = "embedded_catalog"
	MsgNoResources           = "no_resources"
	MsgNoPricedChanges       = "no_priced_changes"
	MsgTotalsLine            = "totals_line"
	MsgNotEstimatedCount     = "not_estimated_count"
	MsgUnchangedCount        = "unchanged_count"
	MsgNotEstimatedLabel     = "not_estimated_label"
	MsgNotEstimatedIn        = "not_estimated_in"
	MsgReasonUsageBased      = "reason_usage_based"
	MsgReasonUnsupportedType = "reason_unsupported_type"
	MsgReasonFree            = "reason_free"
	MsgReasonUnresolved      = "reason_unresolved"
	MsgReasonUnresolvedWith  = "reason_unresolved_with"
	MsgChangeUpdate          = "change_update"
	MsgPriceChanged          = "price_changed"
	MsgChangeNew             = "change_new"
	MsgChangeRemoved         = "change_removed"
	MsgPrefixBaseline        = "prefix_baseline"
	MsgPrefixProposed        = "prefix_proposed"
)

var defaultBundle = func() *i18n.Bundle {
	b := i18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, lang := range Languages {
		f := "active." + lang + ".json"
		bts, err := files.ReadFile(f)
		if err != nil {
			panic(fmt.Sprintf("i18n: %s listed in Languages but missing: %v", f, err))
		}
		if _, err := b.ParseMessageFileBytes(bts, f); err != nil {
			panic(fmt.Sprintf("i18n: parse %s: %v", f, err))
		}
	}
	return b
}()

// L renders messages in one resolved locale. The zero-config New() is English.
type L struct {
	loc *i18n.Localizer
}

// New resolves the locale from the given preferences (later wins is not
// assumed; go-i18n negotiates). Empty prefs fall back to English.
func New(prefs ...string) *L {
	return &L{loc: i18n.NewLocalizer(defaultBundle, prefs...)}
}

// T renders the message; a missing key or failed template falls back to the
// English text (per-key degradation, never an error — gettext semantics).
func (l *L) T(id string, data ...map[string]interface{}) string {
	var d map[string]interface{}
	if len(data) > 0 {
		d = data[0]
	}
	s, err := l.loc.Localize(&i18n.LocalizeConfig{MessageID: id, TemplateData: d})
	if err != nil || s == "" {
		s, _ = i18n.NewLocalizer(defaultBundle, "en").Localize(&i18n.LocalizeConfig{MessageID: id, TemplateData: d})
	}
	return s
}
