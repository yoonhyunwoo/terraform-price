package i18n

import (
	"encoding/json"
	"regexp"
	"testing"
)

func loadCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := files.ReadFile("active." + lang + ".json")
	if err != nil {
		t.Fatalf("read active.%s.json: %v", lang, err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse active.%s.json: %v", lang, err)
	}
	return m
}

var phRe = regexp.MustCompile(`\{\{\.[A-Za-z]+\}\}`)

// TestLocalesComplete is the CI gate for translation PRs: every locale must
// carry exactly the en key set, and every message must use the same
// {{.Placeholder}} names as its English source (msgfmt --check semantics).
func TestLocalesComplete(t *testing.T) {
	en := loadCatalog(t, "en")
	if len(en) == 0 {
		t.Fatal("active.en.json is empty")
	}
	for _, lang := range Languages {
		if lang == "en" {
			continue
		}
		loc := loadCatalog(t, lang)
		for id := range en {
			if _, ok := loc[id]; !ok {
				t.Errorf("locale %q: missing key %q", lang, id)
			}
		}
		for id := range loc {
			if _, ok := en[id]; !ok {
				t.Errorf("locale %q: unknown key %q (not in active.en.json)", lang, id)
			}
		}
		for id, msg := range loc {
			src, ok := en[id]
			if !ok {
				continue
			}
			got := map[string]bool{}
			for _, p := range phRe.FindAllString(msg, -1) {
				got[p] = true
			}
			for _, p := range phRe.FindAllString(src, -1) {
				if !got[p] {
					t.Errorf("locale %q key %q: missing placeholder %s (en: %q, %s: %q)", lang, id, p, src, lang, msg)
				}
			}
		}
	}
}

func TestTFallback(t *testing.T) {
	l := New("ko")
	if got := l.T(MsgReasonUsageBased); got != "사용량 기반 리소스" {
		t.Fatalf("ko lookup = %q", got)
	}
	en := New()
	if got := en.T(MsgReasonUsageBased); got != "usage-based" {
		t.Fatalf("en lookup = %q", got)
	}
	// unknown locale and unknown key both degrade to English, never error
	if got := New("zz").T(MsgReasonUsageBased); got != "usage-based" {
		t.Fatalf("unknown locale = %q", got)
	}
	if got := l.T("no_such_key"); got != "" {
		t.Fatalf("unknown key = %q, want empty", got)
	}
	if got := l.T(MsgHeadlineIncrease, map[string]interface{}{"Delta": "22.78", "Prior": "7.59", "Proposed": "30.37"}); got != "**월간 비용이 $22.78/mo 증가** ($7.59/mo → $30.37/mo) ↑" {
		t.Fatalf("ko templated = %q", got)
	}
}
