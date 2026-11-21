// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

var placeholderPattern = regexp.MustCompile(`\{\{[a-zA-Z0-9_]+\}\}`)

func extractPlaceholders(s string) []string {
	matches := placeholderPattern.FindAllString(s, -1)
	sort.Strings(matches)
	return matches
}

func TestSupportedLocalesCanonicalOrder(t *testing.T) {
	want := []Locale{
		LocaleZhHans,
		LocaleZhHant,
		LocaleDe,
		LocaleEn,
		LocaleEs,
		LocaleFr,
		LocaleID,
		LocaleIt,
		LocaleJa,
		LocaleKo,
		LocalePt,
		LocaleRu,
		LocaleTh,
		LocaleTr,
		LocaleVi,
	}
	got := SupportedLocales()
	if len(got) != len(want) {
		t.Fatalf("SupportedLocales() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SupportedLocales()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllEmbeddedCatalogsCompleteAndParity(t *testing.T) {
	enCatalog, ok := Catalog(LocaleEn)
	if !ok || len(enCatalog) == 0 {
		t.Fatal("English source catalog is missing or empty")
	}

	for _, loc := range SupportedLocales() {
		cat, exists := Catalog(loc)
		if !exists {
			t.Fatalf("missing embedded catalog for %q", loc)
		}
		for key, enVal := range enCatalog {
			val, found := cat[key]
			if !found {
				t.Errorf("locale %q missing key %q", loc, key)
				continue
			}
			if strings.TrimSpace(val) == "" {
				t.Errorf("locale %q has empty value for key %q", loc, key)
				continue
			}
			wantPH := extractPlaceholders(enVal)
			gotPH := extractPlaceholders(val)
			if strings.Join(wantPH, ",") != strings.Join(gotPH, ",") {
				t.Errorf("locale %q key %q placeholder mismatch: got %v, want %v", loc, key, gotPH, wantPH)
			}
		}
		for key := range cat {
			if _, ok := enCatalog[key]; !ok {
				t.Errorf("locale %q has extra key %q not in English source", loc, key)
			}
		}
	}
}

func TestChineseVariantsDistinctTerminology(t *testing.T) {
	hans, _ := Catalog(LocaleZhHans)
	hant, _ := Catalog(LocaleZhHant)

	// Verify zh-Hant uses authentic Traditional regional terms (e.g., 伺服器, 連線, 金鑰, 檔案)
	// rather than naive character conversion of Simplified terms.
	if !strings.Contains(hant["cli.serve.err.failed"], "伺服器") {
		t.Errorf("expected zh-Hant cli.serve.err.failed to use 伺服器, got %q", hant["cli.serve.err.failed"])
	}
	if !strings.Contains(hans["cli.serve.err.failed"], "服务器") {
		t.Errorf("expected zh-Hans cli.serve.err.failed to use 服务器, got %q", hans["cli.serve.err.failed"])
	}
	if !strings.Contains(hant["cli.remote.key.list.short"], "金鑰") {
		t.Errorf("expected zh-Hant cli.remote.key.list.short to use 金鑰, got %q", hant["cli.remote.key.list.short"])
	}
	if !strings.Contains(hans["cli.remote.key.list.short"], "密钥") {
		t.Errorf("expected zh-Hans cli.remote.key.list.short to use 密钥, got %q", hans["cli.remote.key.list.short"])
	}
}

func TestParseLocaleAndAcceptLanguage(t *testing.T) {
	cases := []struct {
		raw  string
		want Locale
		ok   bool
	}{
		{"zh-Hans", LocaleZhHans, true},
		{"zh_CN.UTF-8", LocaleZhHans, true},
		{"zh-SG", LocaleZhHans, true},
		{"zh-Hant", LocaleZhHant, true},
		{"zh-TW", LocaleZhHant, true},
		{"zh_HK.UTF-8", LocaleZhHant, true},
		{"zh-MO", LocaleZhHant, true},
		{"in_ID", LocaleID, true},
		{"vi_VN.UTF-8", LocaleVi, true},
		{"de_DE@euro", LocaleDe, true},
		{"auto", "", false},
		{"C.UTF-8", "", false},
		{"POSIX", "", false},
		{"xx-YY", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseLocale(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseLocale(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}

	if got := ResolveAcceptLanguage("sv-SE, zh-TW;q=0.9, en-US;q=0.8"); got != LocaleZhHant {
		t.Errorf("ResolveAcceptLanguage(zh-TW) = %q, want %q", got, LocaleZhHant)
	}
	if got := ResolveAcceptLanguage("fr-FR;q=0.5, ja-JP;q=0.9, en;q=0.7"); got != LocaleJa {
		t.Errorf("ResolveAcceptLanguage(q-weight) = %q, want %q", got, LocaleJa)
	}
	if got := ResolveAcceptLanguage("ja;q=0, vi;q=0.6"); got != LocaleVi {
		t.Errorf("ResolveAcceptLanguage(zero q) = %q, want %q", got, LocaleVi)
	}
	if got := ResolveAcceptLanguage("sv-SE, nb-NO"); got != LocaleEn {
		t.Errorf("ResolveAcceptLanguage(fallback) = %q, want %q", got, LocaleEn)
	}
}

func TestResolveCLIPrecedence(t *testing.T) {
	envMap := map[string]string{
		"TANPOPO_LANG": "ja",
		"LC_ALL":       "fr_FR.UTF-8",
		"LANG":         "de_DE.UTF-8",
	}
	lookup := func(k string) (string, bool) {
		v, ok := envMap[k]
		return v, ok
	}

	// Explicit flag wins over env.
	loc, err := ResolveCLI("vi", lookup)
	if err != nil || loc != LocaleVi {
		t.Fatalf("ResolveCLI(vi) = (%q, %v), want (vi, nil)", loc, err)
	}

	// "auto" uses TANPOPO_LANG first.
	loc, err = ResolveCLI("auto", lookup)
	if err != nil || loc != LocaleJa {
		t.Fatalf("ResolveCLI(auto) = (%q, %v), want (ja, nil)", loc, err)
	}

	// Without TANPOPO_LANG, LC_ALL wins over LANG.
	delete(envMap, "TANPOPO_LANG")
	loc, err = ResolveCLI("", lookup)
	if err != nil || loc != LocaleFr {
		t.Fatalf("ResolveCLI('') = (%q, %v), want (fr, nil)", loc, err)
	}

	// Unsupported explicit flag returns an error.
	if _, err := ResolveCLI("zz-ZZ", lookup); err == nil {
		t.Fatal("ResolveCLI(zz-ZZ) expected error, got nil")
	}

	// Empty env falls back to English.
	loc, err = ResolveCLI("", func(string) (string, bool) { return "", false })
	if err != nil || loc != LocaleEn {
		t.Fatalf("ResolveCLI(empty env) = (%q, %v), want (en, nil)", loc, err)
	}
}

func TestAdminErrorAndAlertLocalization(t *testing.T) {
	viMsg := AdminErrorMessage(LocaleVi, "unauthenticated", "fallback")
	if viMsg == "fallback" || !strings.Contains(viMsg, "đăng nhập") {
		t.Errorf("AdminErrorMessage(vi, unauthenticated) = %q", viMsg)
	}
	// English preserves explicit contextual fallback when provided.
	enMsg := AdminErrorMessage(LocaleEn, "invalid_request", "specific english detail")
	if enMsg != "specific english detail" {
		t.Errorf("AdminErrorMessage(en, fallback) = %q, want specific english detail", enMsg)
	}

	alertJa := AlertSummary(LocaleJa, "ALT-BUDGET-80", "fallback", "scope", "PROJECT", "id", "proj_1")
	if !strings.Contains(alertJa, "PROJECT") || !strings.Contains(alertJa, "proj_1") || !strings.Contains(alertJa, "80%") {
		t.Errorf("AlertSummary(ja, ALT-BUDGET-80) = %q", alertJa)
	}
}
