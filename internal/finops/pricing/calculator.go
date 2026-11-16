// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package pricing implements strict zero-float 64-bit integer nano-USD and
// micro-USD billing calculations with 128-bit intermediate overflow checking,
// deterministic ceiling division, multimodal pre-hold estimation (CR-05), and
// FMEA-04 3-tier accuracy badge resolution (TP-IMP-0021).
package pricing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
)

const (
	// NanoPerMicro is the exact scaling factor between nano-USD (10^-9) and micro-USD (10^-6).
	NanoPerMicro int64 = 1_000
	// NanoPerUSD is 1 USD in nano-USD.
	NanoPerUSD int64 = 1_000_000_000
	// MicroPerUSD is 1 USD in micro-USD.
	MicroPerUSD int64 = 1_000_000
	// UnitsPerMillion is the denominator for per-1M token/pixel rate cards.
	UnitsPerMillion int64 = 1_000_000
	// MillisPerSecond converts audio milliseconds into per-second rates cleanly without floats.
	MillisPerSecond int64 = 1_000
	// DefaultHoldOutputTokens is the default output ceiling bound when caller omits max_output_tokens.
	DefaultHoldOutputTokens int64 = 4096

	// AccuracyExact marks settlements derived directly from upstream provider metering.
	AccuracyExact = "EXACT"
	// AccuracyEstimated marks settlements using heuristic or partial-stream metering (FMEA-04).
	AccuracyEstimated = "ESTIMATED"
	// AccuracyUnmeteredFlagged marks settlements on unpriced models needing SCR-05 rate card entry.
	AccuracyUnmeteredFlagged = "UNMETERED_FLAGGED"

	// BillingModeCommercialAPI identifies commercial pay-as-you-go API connections.
	BillingModeCommercialAPI = "COMMERCIAL_API"
	// BillingModeSelfHostedInfra identifies self-operated infrastructure endpoints.
	BillingModeSelfHostedInfra = "SELF_HOSTED_INFRA"
)

var (
	// ErrNegativeValue rejects negative token counts, media counts, or pricing rates.
	ErrNegativeValue = errors.New("pricing: negative token count, media usage, or rate")
	// ErrArithmeticOverflow indicates exact cost exceeded signed 64-bit integer range.
	ErrArithmeticOverflow = errors.New("pricing: 64-bit integer cost overflow")
	// ErrInvalidMediaUsage indicates malformed multimodal usage metadata JSON.
	ErrInvalidMediaUsage = errors.New("pricing: invalid media usage JSON")
)

// RateCard represents a deployment's deterministic integer nano-USD pricing schedule.
type RateCard struct {
	InputPer1MNanoUSD        int64 `json:"input_per_1m_nano_usd"`
	OutputPer1MNanoUSD       int64 `json:"output_per_1m_nano_usd"`
	CacheReadPer1MNanoUSD    int64 `json:"cache_read_per_1m_nano_usd,omitempty"`
	CacheWritePer1MNanoUSD   int64 `json:"cache_write_per_1m_nano_usd,omitempty"`
	ReasoningPer1MNanoUSD    int64 `json:"reasoning_per_1m_nano_usd,omitempty"`
	ImagePerUnitNanoUSD      int64 `json:"image_per_unit_nano_usd,omitempty"`
	ImagePer1MPixelsNanoUSD  int64 `json:"image_per_1m_pixels_nano_usd,omitempty"`
	AudioInputPerSecNanoUSD  int64 `json:"audio_input_per_sec_nano_usd,omitempty"`
	AudioOutputPerSecNanoUSD int64 `json:"audio_output_per_sec_nano_usd,omitempty"`
	DocumentPageNanoUSD      int64 `json:"document_page_nano_usd,omitempty"`
	RerankUnitNanoUSD        int64 `json:"rerank_unit_nano_usd,omitempty"`
	SelfHostedZeroRate       bool  `json:"self_hosted_zero_rate,omitempty"`
	BillingMode              string `json:"billing_mode,omitempty"`
}

// RateCardFromSnapshot converts a snapshot PricingNanoUSD and billing mode into a RateCard.
func RateCardFromSnapshot(p snapshot.PricingNanoUSD, billingMode string) RateCard {
	mode := strings.ToUpper(strings.TrimSpace(billingMode))
	if mode != BillingModeSelfHostedInfra {
		mode = BillingModeCommercialAPI
	}
	return RateCard{
		InputPer1MNanoUSD:        p.InputPer1MNanoUSD,
		OutputPer1MNanoUSD:       p.OutputPer1MNanoUSD,
		CacheReadPer1MNanoUSD:    p.CacheReadPer1MNanoUSD,
		CacheWritePer1MNanoUSD:   p.CacheWritePer1MNanoUSD,
		ReasoningPer1MNanoUSD:    p.ReasoningPer1MNanoUSD,
		ImagePerUnitNanoUSD:      p.ImagePerUnitNanoUSD,
		ImagePer1MPixelsNanoUSD:  p.ImagePer1MPixelsNanoUSD,
		AudioInputPerSecNanoUSD:  p.AudioInputPerSecNanoUSD,
		AudioOutputPerSecNanoUSD: p.AudioOutputPerSecNanoUSD,
		DocumentPageNanoUSD:      p.DocumentPageNanoUSD,
		RerankUnitNanoUSD:        p.RerankUnitNanoUSD,
		SelfHostedZeroRate:       mode == BillingModeSelfHostedInfra && p.SelfHostedZeroRate,
		BillingMode:              mode,
	}
}

// HasNumericRates reports whether at least one positive unit rate is configured.
func (r RateCard) HasNumericRates() bool {
	return r.InputPer1MNanoUSD > 0 ||
		r.OutputPer1MNanoUSD > 0 ||
		r.CacheReadPer1MNanoUSD > 0 ||
		r.CacheWritePer1MNanoUSD > 0 ||
		r.ReasoningPer1MNanoUSD > 0 ||
		r.ImagePerUnitNanoUSD > 0 ||
		r.ImagePer1MPixelsNanoUSD > 0 ||
		r.AudioInputPerSecNanoUSD > 0 ||
		r.AudioOutputPerSecNanoUSD > 0 ||
		r.DocumentPageNanoUSD > 0 ||
		r.RerankUnitNanoUSD > 0
}

// HasConfiguredRates reports whether a rate card either sets positive pricing rates
// or explicitly opts into zero-rate internal chargeback via SelfHostedZeroRate on SELF_HOSTED_INFRA.
func (r RateCard) HasConfiguredRates() bool {
	if r.HasNumericRates() {
		return true
	}
	return r.SelfHostedZeroRate && strings.EqualFold(strings.TrimSpace(r.BillingMode), BillingModeSelfHostedInfra)
}

// Validate checks that no pricing rate field is negative.
func (r RateCard) Validate() error {
	if r.InputPer1MNanoUSD < 0 ||
		r.OutputPer1MNanoUSD < 0 ||
		r.CacheReadPer1MNanoUSD < 0 ||
		r.CacheWritePer1MNanoUSD < 0 ||
		r.ReasoningPer1MNanoUSD < 0 ||
		r.ImagePerUnitNanoUSD < 0 ||
		r.ImagePer1MPixelsNanoUSD < 0 ||
		r.AudioInputPerSecNanoUSD < 0 ||
		r.AudioOutputPerSecNanoUSD < 0 ||
		r.DocumentPageNanoUSD < 0 ||
		r.RerankUnitNanoUSD < 0 {
		return ErrNegativeValue
	}
	return nil
}

// SnapshotJSON encodes the rate card canonically for usage_settlements.applied_pricing_snapshot_json.
func (r RateCard) SnapshotJSON() string {
	b, err := json.Marshal(r)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// MediaUsage captures non-token multimodal consumption strictly with signed 64-bit integers.
// Audio durations are expressed in milliseconds to avoid floating-point types (NEG-1).
type MediaUsage struct {
	ImageCount        int64 `json:"image_count,omitempty"`
	ImagePixels       int64 `json:"image_pixels,omitempty"`
	AudioInputMillis  int64 `json:"audio_input_millis,omitempty"`
	AudioOutputMillis int64 `json:"audio_output_millis,omitempty"`
	DocumentPages     int64 `json:"document_pages,omitempty"`
	RerankSearchUnits int64 `json:"rerank_search_units,omitempty"`
}

// IsZero reports whether all media consumption fields are zero.
func (m MediaUsage) IsZero() bool {
	return m.ImageCount == 0 &&
		m.ImagePixels == 0 &&
		m.AudioInputMillis == 0 &&
		m.AudioOutputMillis == 0 &&
		m.DocumentPages == 0 &&
		m.RerankSearchUnits == 0
}

// Validate verifies all media counts are non-negative.
func (m MediaUsage) Validate() error {
	if m.ImageCount < 0 ||
		m.ImagePixels < 0 ||
		m.AudioInputMillis < 0 ||
		m.AudioOutputMillis < 0 ||
		m.DocumentPages < 0 ||
		m.RerankSearchUnits < 0 {
		return ErrNegativeValue
	}
	return nil
}

// ToJSON deterministically encodes MediaUsage for usage_settlements.media_usage_json.
func (m MediaUsage) ToJSON() string {
	if m.IsZero() {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ParseMediaUsageJSON decodes media usage JSON without using floating-point arithmetic.
// Accepts both audio_*_millis integers and audio_*_seconds decimal strings/numbers.
func ParseMediaUsageJSON(raw string) (MediaUsage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return MediaUsage{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return MediaUsage{}, fmt.Errorf("%w: expected JSON object", ErrInvalidMediaUsage)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return MediaUsage{}, fmt.Errorf("%w: trailing JSON data", ErrInvalidMediaUsage)
	}

	var m MediaUsage
	var err error
	if m.ImageCount, err = parseJSONInt64(obj["image_count"]); err != nil {
		return MediaUsage{}, err
	}
	if m.ImagePixels, err = parseJSONInt64(obj["image_pixels"]); err != nil {
		return MediaUsage{}, err
	}
	if m.DocumentPages, err = parseJSONInt64(obj["document_pages"]); err != nil {
		return MediaUsage{}, err
	}
	if m.RerankSearchUnits, err = parseJSONInt64(obj["rerank_search_units"]); err != nil {
		return MediaUsage{}, err
	}
	if obj["audio_input_millis"] != nil {
		if m.AudioInputMillis, err = parseJSONInt64(obj["audio_input_millis"]); err != nil {
			return MediaUsage{}, err
		}
	} else if obj["audio_input_seconds"] != nil {
		if m.AudioInputMillis, err = parseDecimalSecondsToMillis(obj["audio_input_seconds"]); err != nil {
			return MediaUsage{}, err
		}
	}
	if obj["audio_output_millis"] != nil {
		if m.AudioOutputMillis, err = parseJSONInt64(obj["audio_output_millis"]); err != nil {
			return MediaUsage{}, err
		}
	} else if obj["audio_output_seconds"] != nil {
		if m.AudioOutputMillis, err = parseDecimalSecondsToMillis(obj["audio_output_seconds"]); err != nil {
			return MediaUsage{}, err
		}
	}
	if err := m.Validate(); err != nil {
		return MediaUsage{}, err
	}
	return m, nil
}

func parseJSONInt64(v any) (int64, error) {
	if v == nil {
		return 0, nil
	}
	num, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%w: expected integer", ErrInvalidMediaUsage)
	}
	iv, err := num.Int64()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidMediaUsage, err)
	}
	if iv < 0 {
		return 0, ErrNegativeValue
	}
	return iv, nil
}

func parseDecimalSecondsToMillis(v any) (int64, error) {
	if v == nil {
		return 0, nil
	}
	var s string
	switch t := v.(type) {
	case json.Number:
		s = t.String()
	case string:
		s = strings.TrimSpace(t)
	default:
		return 0, fmt.Errorf("%w: expected numeric or string seconds", ErrInvalidMediaUsage)
	}
	if s == "" {
		return 0, nil
	}
	if strings.HasPrefix(s, "-") {
		return 0, ErrNegativeValue
	}
	if strings.ContainsAny(s, "eE") {
		return 0, fmt.Errorf("%w: scientific notation not permitted", ErrInvalidMediaUsage)
	}
	wholeStr, fracStr, hasDot := strings.Cut(s, ".")
	if wholeStr == "" && fracStr == "" {
		return 0, fmt.Errorf("%w: empty duration", ErrInvalidMediaUsage)
	}
	var whole int64
	for i := 0; i < len(wholeStr); i++ {
		c := wholeStr[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%w: invalid digit in seconds", ErrInvalidMediaUsage)
		}
		if whole > (math.MaxInt64-int64(c-'0'))/10 {
			return 0, ErrArithmeticOverflow
		}
		whole = whole*10 + int64(c-'0')
	}
	if whole > math.MaxInt64/MillisPerSecond {
		return 0, ErrArithmeticOverflow
	}
	millis := whole * MillisPerSecond

	var fracMillis int64
	if hasDot {
		weights := [3]int64{100, 10, 1}
		roundUp := false
		for i := 0; i < len(fracStr); i++ {
			c := fracStr[i]
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("%w: invalid fractional digit in seconds", ErrInvalidMediaUsage)
			}
			d := int64(c - '0')
			if i < 3 {
				fracMillis += d * weights[i]
			} else if d > 0 {
				roundUp = true
			}
		}
		if roundUp {
			fracMillis++
		}
	}
	if millis > math.MaxInt64-fracMillis {
		return 0, ErrArithmeticOverflow
	}
	return millis + fracMillis, nil
}

// UsageInput describes token and multimodal usage passed to the pricing calculator.
type UsageInput struct {
	InputTokens          int64
	OutputTokens         int64
	CacheReadTokens      int64
	CacheWriteTokens     int64
	ReasoningTokens      int64
	Media                MediaUsage
	UpstreamUsagePresent bool
	UpstreamAccuracy     string
	FallbackTextBytes    int64
	FallbackOutputBytes  int64
}

// Validate verifies all non-negative constraints on UsageInput.
func (u UsageInput) Validate() error {
	if u.InputTokens < 0 ||
		u.OutputTokens < 0 ||
		u.CacheReadTokens < 0 ||
		u.CacheWriteTokens < 0 ||
		u.ReasoningTokens < 0 ||
		u.FallbackTextBytes < 0 ||
		u.FallbackOutputBytes < 0 {
		return ErrNegativeValue
	}
	return u.Media.Validate()
}

// CostBreakdown reports deterministic integer nano-USD, micro-USD, effective usage, and accuracy tier.
type CostBreakdown struct {
	TotalNanoUSD     int64
	TotalMicroUSD    int64
	PerMillionNano   int64
	MediaNanoUSD     int64
	EffectiveUsage   UsageInput
	Accuracy         string
	UnmeteredFlagged bool
}

// CeilDivInt64 computes ceil(numerator / denominator) for non-negative int64 values without overflow.
func CeilDivInt64(numerator, denominator int64) (int64, error) {
	if numerator < 0 || denominator <= 0 {
		return 0, ErrNegativeValue
	}
	if numerator == 0 {
		return 0, nil
	}
	q := numerator / denominator
	if numerator%denominator != 0 {
		if q == math.MaxInt64 {
			return 0, ErrArithmeticOverflow
		}
		q++
	}
	return q, nil
}

// EstimateTokensFromUTF8Bytes computes ceil(utf8Bytes / 3) per SPEC-CORE-02 heuristic.
func EstimateTokensFromUTF8Bytes(utf8Bytes int64) int64 {
	if utf8Bytes <= 0 {
		return 0
	}
	q, err := CeilDivInt64(utf8Bytes, 3)
	if err != nil {
		return math.MaxInt64
	}
	return q
}

// EstimatePreHold computes the conservative worst-case pre-request budget hold (CR-05)
// using text-only token estimate, bounded output ceiling, and multimodal unit counts.
func EstimatePreHold(
	rate RateCard,
	inputTextBytes int64,
	maxOutputTokens int64,
	defaultHoldOutputTokens int64,
	media MediaUsage,
) (CostBreakdown, error) {
	if inputTextBytes < 0 || maxOutputTokens < 0 || defaultHoldOutputTokens < 0 {
		return CostBreakdown{}, ErrNegativeValue
	}
	boundOut := maxOutputTokens
	if boundOut == 0 {
		if defaultHoldOutputTokens > 0 {
			boundOut = defaultHoldOutputTokens
		} else {
			boundOut = DefaultHoldOutputTokens
		}
	}
	inTok := EstimateTokensFromUTF8Bytes(inputTextBytes)
	return Calculate(rate, UsageInput{
		InputTokens:          inTok,
		OutputTokens:         boundOut,
		Media:                media,
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     AccuracyEstimated,
	})
}

// Calculate computes exact 64-bit integer cost with 128-bit intermediate accumulation,
// deterministic ceiling rounding to micro-USD, and FMEA-04 accuracy badge enforcement.
func Calculate(rate RateCard, usage UsageInput) (CostBreakdown, error) {
	if err := rate.Validate(); err != nil {
		return CostBreakdown{}, err
	}
	if err := usage.Validate(); err != nil {
		return CostBreakdown{}, err
	}

	effective, accuracy, unmetered := resolveEffectiveUsageAndAccuracy(rate, usage)
	if unmetered {
		return CostBreakdown{
			TotalNanoUSD:     0,
			TotalMicroUSD:    0,
			EffectiveUsage:   effective,
			Accuracy:         AccuracyUnmeteredFlagged,
			UnmeteredFlagged: true,
		}, nil
	}

	per1MNano, err := computePerMillionCeiling(rate, effective)
	if err != nil {
		return CostBreakdown{}, err
	}
	mediaNano, err := computeDirectMediaNano(rate, effective.Media)
	if err != nil {
		return CostBreakdown{}, err
	}
	if per1MNano > math.MaxInt64-mediaNano {
		return CostBreakdown{}, ErrArithmeticOverflow
	}
	totalNano := per1MNano + mediaNano
	totalMicros, err := CeilDivInt64(totalNano, NanoPerMicro)
	if err != nil {
		return CostBreakdown{}, err
	}

	return CostBreakdown{
		TotalNanoUSD:     totalNano,
		TotalMicroUSD:    totalMicros,
		PerMillionNano:   per1MNano,
		MediaNanoUSD:     mediaNano,
		EffectiveUsage:   effective,
		Accuracy:         accuracy,
		UnmeteredFlagged: false,
	}, nil
}

func resolveEffectiveUsageAndAccuracy(rate RateCard, in UsageInput) (UsageInput, string, bool) {
	effective := in

	if !rate.HasConfiguredRates() {
		// Unpriced commercial or self-hosted deployment -> UNMETERED_FLAGGED (0 micro-USD + ALT-UNMETERED-SPEND).
		if !in.UpstreamUsagePresent {
			if effective.InputTokens == 0 && in.FallbackTextBytes > 0 {
				effective.InputTokens = EstimateTokensFromUTF8Bytes(in.FallbackTextBytes)
			}
			if effective.OutputTokens == 0 && in.FallbackOutputBytes > 0 {
				effective.OutputTokens = EstimateTokensFromUTF8Bytes(in.FallbackOutputBytes)
			}
		}
		return effective, AccuracyUnmeteredFlagged, true
	}

	normalizedUpstreamAcc := normalizeAccuracy(in.UpstreamAccuracy)
	allTextTokensZero := in.InputTokens == 0 &&
		in.OutputTokens == 0 &&
		in.CacheReadTokens == 0 &&
		in.CacheWriteTokens == 0 &&
		in.ReasoningTokens == 0

	if !in.UpstreamUsagePresent {
		if effective.InputTokens == 0 && in.FallbackTextBytes > 0 {
			effective.InputTokens = EstimateTokensFromUTF8Bytes(in.FallbackTextBytes)
		}
		if effective.OutputTokens == 0 && in.FallbackOutputBytes > 0 {
			effective.OutputTokens = EstimateTokensFromUTF8Bytes(in.FallbackOutputBytes)
		}
		return effective, AccuracyEstimated, false
	}

	// FMEA-04 invariant: never stamp EXACT=0 when non-empty prompt/output payloads exist
	// (including multimodal requests whose media units are non-zero while text tokens are zero).
	if allTextTokensZero && (in.FallbackTextBytes > 0 || in.FallbackOutputBytes > 0) {
		if in.FallbackTextBytes > 0 {
			effective.InputTokens = EstimateTokensFromUTF8Bytes(in.FallbackTextBytes)
		}
		if in.FallbackOutputBytes > 0 {
			effective.OutputTokens = EstimateTokensFromUTF8Bytes(in.FallbackOutputBytes)
		}
		return effective, AccuracyEstimated, false
	}

	if normalizedUpstreamAcc == AccuracyEstimated || normalizedUpstreamAcc == AccuracyUnmeteredFlagged {
		if effective.InputTokens == 0 && effective.CacheReadTokens == 0 && effective.CacheWriteTokens == 0 && in.FallbackTextBytes > 0 {
			effective.InputTokens = EstimateTokensFromUTF8Bytes(in.FallbackTextBytes)
		}
		if effective.OutputTokens == 0 && effective.ReasoningTokens == 0 && in.FallbackOutputBytes > 0 {
			effective.OutputTokens = EstimateTokensFromUTF8Bytes(in.FallbackOutputBytes)
		}
		return effective, AccuracyEstimated, false
	}
	return effective, AccuracyExact, false
}

func normalizeAccuracy(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case AccuracyExact:
		return AccuracyExact
	case AccuracyEstimated:
		return AccuracyEstimated
	case AccuracyUnmeteredFlagged:
		return AccuracyUnmeteredFlagged
	default:
		return ""
	}
}

type u128 struct {
	hi uint64
	lo uint64
}

func addMulU128(acc u128, a, b int64) (u128, error) {
	if a < 0 || b < 0 {
		return u128{}, ErrNegativeValue
	}
	if a == 0 || b == 0 {
		return acc, nil
	}
	pHi, pLo := bits.Mul64(uint64(a), uint64(b))
	lo, carry := bits.Add64(acc.lo, pLo, 0)
	hi, over := bits.Add64(acc.hi, pHi, carry)
	if over != 0 {
		return u128{}, ErrArithmeticOverflow
	}
	return u128{hi: hi, lo: lo}, nil
}

func ceilDivU128ByUint64(val u128, den uint64) (int64, error) {
	if den == 0 {
		return 0, ErrNegativeValue
	}
	if val.hi == 0 && val.lo == 0 {
		return 0, nil
	}
	if val.hi >= den {
		return 0, ErrArithmeticOverflow
	}
	q, rem := bits.Div64(val.hi, val.lo, den)
	if rem > 0 {
		if q == ^uint64(0) {
			return 0, ErrArithmeticOverflow
		}
		q++
	}
	if q > uint64(math.MaxInt64) {
		return 0, ErrArithmeticOverflow
	}
	return int64(q), nil
}

func computePerMillionCeiling(rate RateCard, u UsageInput) (int64, error) {
	var acc u128
	var err error
	if acc, err = addMulU128(acc, u.InputTokens, rate.InputPer1MNanoUSD); err != nil {
		return 0, err
	}
	if acc, err = addMulU128(acc, u.OutputTokens, rate.OutputPer1MNanoUSD); err != nil {
		return 0, err
	}
	if acc, err = addMulU128(acc, u.CacheReadTokens, rate.CacheReadPer1MNanoUSD); err != nil {
		return 0, err
	}
	if acc, err = addMulU128(acc, u.CacheWriteTokens, rate.CacheWritePer1MNanoUSD); err != nil {
		return 0, err
	}
	if acc, err = addMulU128(acc, u.ReasoningTokens, rate.ReasoningPer1MNanoUSD); err != nil {
		return 0, err
	}
	if acc, err = addMulU128(acc, u.Media.ImagePixels, rate.ImagePer1MPixelsNanoUSD); err != nil {
		return 0, err
	}
	return ceilDivU128ByUint64(acc, uint64(UnitsPerMillion))
}

func computeDirectMediaNano(rate RateCard, m MediaUsage) (int64, error) {
	var unitAcc u128
	var err error
	if unitAcc, err = addMulU128(unitAcc, m.ImageCount, rate.ImagePerUnitNanoUSD); err != nil {
		return 0, err
	}
	if unitAcc, err = addMulU128(unitAcc, m.DocumentPages, rate.DocumentPageNanoUSD); err != nil {
		return 0, err
	}
	if unitAcc, err = addMulU128(unitAcc, m.RerankSearchUnits, rate.RerankUnitNanoUSD); err != nil {
		return 0, err
	}
	if unitAcc.hi != 0 || unitAcc.lo > uint64(math.MaxInt64) {
		return 0, ErrArithmeticOverflow
	}
	unitTotal := int64(unitAcc.lo)

	var audioAcc u128
	if audioAcc, err = addMulU128(audioAcc, m.AudioInputMillis, rate.AudioInputPerSecNanoUSD); err != nil {
		return 0, err
	}
	if audioAcc, err = addMulU128(audioAcc, m.AudioOutputMillis, rate.AudioOutputPerSecNanoUSD); err != nil {
		return 0, err
	}
	audioNano, err := ceilDivU128ByUint64(audioAcc, uint64(MillisPerSecond))
	if err != nil {
		return 0, err
	}
	if unitTotal > math.MaxInt64-audioNano {
		return 0, ErrArithmeticOverflow
	}
	return unitTotal + audioNano, nil
}
