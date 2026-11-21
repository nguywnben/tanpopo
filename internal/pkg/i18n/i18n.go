// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package i18n provides pure-Go, offline-embedded localization catalogs and
// BCP-47/POSIX locale resolution for Tanpopo CLI, Admin API, and WebUI errors.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Locale identifies a supported Tanpopo BCP-47 locale tag.
type Locale string

const (
	LocaleZhHans Locale = "zh-Hans"
	LocaleZhHant Locale = "zh-Hant"
	LocaleDe     Locale = "de"
	LocaleEn     Locale = "en"
	LocaleEs     Locale = "es"
	LocaleFr     Locale = "fr"
	LocaleID     Locale = "id"
	LocaleIt     Locale = "it"
	LocaleJa     Locale = "ja"
	LocaleKo     Locale = "ko"
	LocalePt     Locale = "pt"
	LocaleRu     Locale = "ru"
	LocaleTh     Locale = "th"
	LocaleTr     Locale = "tr"
	LocaleVi     Locale = "vi"
)

// orderedLocales defines the canonical 15 supported locales in product order.
var orderedLocales = []Locale{
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

//go:embed locales/*.json
var localeFS embed.FS

var catalogs map[Locale]map[string]string

func init() {
	catalogs = make(map[Locale]map[string]string, len(orderedLocales))
	for _, loc := range orderedLocales {
		path := "locales/" + string(loc) + ".json"
		raw, err := localeFS.ReadFile(path)
		if err != nil {
			continue
		}
		parsed := make(map[string]string)
		if err := json.Unmarshal(raw, &parsed); err != nil {
			panic(fmt.Sprintf("i18n: invalid embedded catalog %s: %v", path, err))
		}
		catalogs[loc] = parsed
	}
	if _, ok := catalogs[LocaleEn]; !ok {
		panic("i18n: embedded English source catalog locales/en.json is missing")
	}
}

// SupportedLocales returns a copy of the 15 supported locales in canonical order.
func SupportedLocales() []Locale {
	out := make([]Locale, len(orderedLocales))
	copy(out, orderedLocales)
	return out
}

// ParseLocale normalizes a BCP-47 or POSIX locale string into a supported Locale.
// It distinguishes Simplified Chinese (zh-Hans, zh-CN, zh-SG) from Traditional
// Chinese (zh-Hant, zh-TW, zh-HK, zh-MO).
func ParseLocale(raw string) (Locale, bool) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "", false
	}
	// Strip POSIX encoding/modifier suffixes such as ".UTF-8" or "@euro".
	if idx := strings.IndexAny(cleaned, ".@"); idx >= 0 {
		cleaned = cleaned[:idx]
	}
	cleaned = strings.ReplaceAll(cleaned, "_", "-")
	lower := strings.ToLower(strings.TrimSpace(cleaned))
	if lower == "" || lower == "c" || lower == "posix" || lower == "auto" {
		return "", false
	}

	parts := strings.Split(lower, "-")
	lang := parts[0]
	if lang == "in" {
		// Legacy ISO 639-1 code for Indonesian.
		return LocaleID, true
	}

	if lang == "zh" {
		for _, sub := range parts[1:] {
			switch sub {
			case "hant", "tw", "hk", "mo":
				return LocaleZhHant, true
			case "hans", "cn", "sg":
				return LocaleZhHans, true
			}
		}
		return LocaleZhHans, true
	}

	switch lang {
	case "de":
		return LocaleDe, true
	case "en":
		return LocaleEn, true
	case "es":
		return LocaleEs, true
	case "fr":
		return LocaleFr, true
	case "id":
		return LocaleID, true
	case "it":
		return LocaleIt, true
	case "ja":
		return LocaleJa, true
	case "ko":
		return LocaleKo, true
	case "pt":
		return LocalePt, true
	case "ru":
		return LocaleRu, true
	case "th":
		return LocaleTh, true
	case "tr":
		return LocaleTr, true
	case "vi":
		return LocaleVi, true
	default:
		return "", false
	}
}

type weightedTag struct {
	tag   string
	q     float64
	order int
}

// ResolveAcceptLanguage selects the highest-priority supported locale from an
// HTTP Accept-Language header, falling back to English when none matches.
func ResolveAcceptLanguage(header string) Locale {
	if strings.TrimSpace(header) == "" {
		return LocaleEn
	}
	rawParts := strings.Split(header, ",")
	candidates := make([]weightedTag, 0, len(rawParts))
	for i, part := range rawParts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		segs := strings.Split(item, ";")
		tag := strings.TrimSpace(segs[0])
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, param := range segs[1:] {
			k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = parsed
				}
			}
		}
		if q <= 0 {
			continue
		}
		candidates = append(candidates, weightedTag{tag: tag, q: q, order: i})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q == candidates[j].q {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].q > candidates[j].q
	})
	for _, c := range candidates {
		if loc, ok := ParseLocale(c.tag); ok {
			return loc
		}
	}
	return LocaleEn
}

// ResolveCLI resolves the CLI locale using explicit flag first, then environment
// variables (TANPOPO_LANG, LC_ALL, LC_MESSAGES, LANG), falling back to English.
// An explicit non-empty flag value other than "auto" must be a supported locale.
func ResolveCLI(explicitFlag string, lookupEnv func(string) (string, bool)) (Locale, error) {
	trimmed := strings.TrimSpace(explicitFlag)
	if trimmed != "" && !strings.EqualFold(trimmed, "auto") {
		loc, ok := ParseLocale(trimmed)
		if !ok {
			return LocaleEn, fmt.Errorf("unsupported --lang %q; supported locales: auto, %s", explicitFlag, SupportedLocaleList())
		}
		return loc, nil
	}
	if lookupEnv != nil {
		for _, key := range []string{"TANPOPO_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
			if raw, ok := lookupEnv(key); ok {
				if loc, valid := ParseLocale(raw); valid {
					return loc, nil
				}
			}
		}
	}
	return LocaleEn, nil
}

// SupportedLocaleList returns a comma-separated list of supported locale codes.
func SupportedLocaleList() string {
	names := make([]string, len(orderedLocales))
	for i, loc := range orderedLocales {
		names[i] = string(loc)
	}
	return strings.Join(names, ", ")
}

// T looks up a message key in the requested locale, falling back to English and
// finally the key itself, then interpolates {{name}} placeholders from key/value pairs.
func T(locale Locale, key string, args ...any) string {
	tmpl := lookup(locale, key)
	if tmpl == "" {
		return key
	}
	return interpolate(tmpl, args...)
}

// HasKey reports whether key exists in the given locale's catalog.
func HasKey(locale Locale, key string) bool {
	cat, ok := catalogs[locale]
	if !ok {
		return false
	}
	val, exists := cat[key]
	return exists && strings.TrimSpace(val) != ""
}

// Catalog returns a shallow copy of a locale's embedded message map for verification tests.
func Catalog(locale Locale) (map[string]string, bool) {
	cat, ok := catalogs[locale]
	if !ok {
		return nil, false
	}
	cp := make(map[string]string, len(cat))
	for k, v := range cat {
		cp[k] = v
	}
	return cp, true
}

// AdminErrorMessage returns a localized user-facing message for a canonical Admin API
// error code when a non-English locale is requested (or when the catalog defines a
// richer message), while falling back cleanly to the English message when omitted.
func AdminErrorMessage(locale Locale, code, fallback string) string {
	if locale == "" || locale == LocaleEn {
		if strings.TrimSpace(fallback) != "" {
			return fallback
		}
		if msg := lookup(LocaleEn, "api.error."+code); msg != "" {
			return msg
		}
		return code
	}
	if cat, ok := catalogs[locale]; ok {
		if msg, exists := cat["api.error."+code]; exists && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	if msg := lookup(LocaleEn, "api.error."+code); msg != "" {
		return msg
	}
	return code
}

// AlertSummary returns a localized summary for an active operational alert code.
func AlertSummary(locale Locale, alertCode, fallback string, args ...any) string {
	key := "alert." + alertCode
	if cat, ok := catalogs[locale]; ok {
		if tmpl, exists := cat[key]; exists && strings.TrimSpace(tmpl) != "" {
			return interpolate(tmpl, args...)
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return T(LocaleEn, key, args...)
}

func lookup(locale Locale, key string) string {
	if cat, ok := catalogs[locale]; ok {
		if val, exists := cat[key]; exists && val != "" {
			return val
		}
	}
	if enCat, ok := catalogs[LocaleEn]; ok {
		if val, exists := enCat[key]; exists && val != "" {
			return val
		}
	}
	return ""
}

func interpolate(tmpl string, args ...any) string {
	if len(args) < 2 {
		return tmpl
	}
	out := tmpl
	for i := 0; i+1 < len(args); i += 2 {
		name := fmt.Sprint(args[i])
		val := fmt.Sprint(args[i+1])
		out = strings.ReplaceAll(out, "{{"+name+"}}", val)
	}
	return out
}
