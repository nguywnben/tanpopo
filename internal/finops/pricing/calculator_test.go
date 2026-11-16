// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package pricing_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/finops/pricing"
)

// TestNoFloatingPointInFinops enforces NEG-1 across all Go files in internal/finops/...
func TestNoFloatingPointInFinops(t *testing.T) {
	finopsRoot := filepath.Join("..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(finopsRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		fileAST, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		ast.Inspect(fileAST, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				if node.Name == "float32" || node.Name == "float64" {
					pos := fset.Position(node.Pos())
					t.Errorf("NEG-1 VIOLATION: forbidden float type %q at %s:%d", node.Name, pos.Filename, pos.Line)
				}
			case *ast.BasicLit:
				if node.Kind == token.FLOAT {
					pos := fset.Position(node.Pos())
					t.Errorf("NEG-1 VIOLATION: forbidden float literal %q at %s:%d", node.Value, pos.Filename, pos.Line)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/finops: %v", err)
	}
}

func TestSixDimensionsAndCeilingRounding(t *testing.T) {
	// Rates per 1M tokens:
	//   input:      $2.50 / 1M = 2_500_000_000 nano-USD
	//   output:    $10.00 / 1M = 10_000_000_000 nano-USD
	//   cache_read: $0.25 / 1M = 250_000_000 nano-USD
	//   cache_write:$3.125/ 1M = 3_125_000_000 nano-USD
	//   reasoning: $15.00 / 1M = 15_000_000_000 nano-USD
	rate := pricing.RateCard{
		InputPer1MNanoUSD:        2_500_000_000,
		OutputPer1MNanoUSD:       10_000_000_000,
		CacheReadPer1MNanoUSD:    250_000_000,
		CacheWritePer1MNanoUSD:   3_125_000_000,
		ReasoningPer1MNanoUSD:    15_000_000_000,
		ImagePerUnitNanoUSD:      1_200_000, // 1,200 micro-USD per image
		ImagePer1MPixelsNanoUSD:  500_000_000,
		AudioInputPerSecNanoUSD:  1_000_000, // 1,000 micro-USD per audio input sec
		AudioOutputPerSecNanoUSD: 2_000_000, // 2,000 micro-USD per audio output sec
		DocumentPageNanoUSD:      500_000,   // 500 micro-USD per document page
		RerankUnitNanoUSD:        200_000,   // 200 micro-USD per rerank unit
		BillingMode:              pricing.BillingModeCommercialAPI,
	}

	res, err := pricing.Calculate(rate, pricing.UsageInput{
		InputTokens:      1_000,
		OutputTokens:     500,
		CacheReadTokens:  400,
		CacheWriteTokens: 200,
		ReasoningTokens:  100,
		Media: pricing.MediaUsage{
			ImageCount:        2,
			ImagePixels:       500_000, // 0.5M pixels -> 250_000_000 nano-USD
			AudioInputMillis:  1_500,   // 1.5 sec -> 1_500_000 nano-USD
			AudioOutputMillis: 500,     // 0.5 sec -> 1_000_000 nano-USD
			DocumentPages:     3,       // 3 * 500_000 = 1_500_000 nano-USD
			RerankSearchUnits: 1,       // 200_000 nano-USD
		},
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil {
		t.Fatalf("Calculate failed: %v", err)
	}

	// Token + Pixel sum / 1M:
	//   input: 1000 * 2.5B = 2.5e12 -> 2_500_000
	//   output: 500 * 10B = 5e12 -> 5_000_000
	//   cache_read: 400 * 0.25B = 1e11 -> 100_000
	//   cache_write: 200 * 3.125B = 6.25e11 -> 625_000
	//   reasoning: 100 * 15B = 1.5e12 -> 1_500_000
	//   pixels: 500k * 500M = 2.5e14 -> 250_000_000
	// Per-1M total = 259_725_000 nano-USD
	// Direct media = 2_400_000 + 1_500_000 + 1_000_000 + 1_500_000 + 200_000 = 6_600_000 nano-USD
	// Total nano = 266_325_000 -> micro-USD = 266_325
	if res.PerMillionNano != 259_725_000 {
		t.Fatalf("expected PerMillionNano=259725000, got %d", res.PerMillionNano)
	}
	if res.MediaNanoUSD != 6_600_000 {
		t.Fatalf("expected MediaNanoUSD=6600000, got %d", res.MediaNanoUSD)
	}
	if res.TotalMicroUSD != 266_325 {
		t.Fatalf("expected TotalMicroUSD=266325, got %d", res.TotalMicroUSD)
	}
	if res.Accuracy != pricing.AccuracyExact {
		t.Fatalf("expected accuracy=EXACT, got %q", res.Accuracy)
	}
}

func TestSubMicroUSDCeilingAndLargeToken128BitSafety(t *testing.T) {
	// 1 input token at $0.01 / 1M (10_000_000 nano/1M -> 10 nano/token):
	// TotalNanoUSD = 10 -> must round UP to 1 micro-USD (never truncate to 0!).
	subMicro, err := pricing.Calculate(pricing.RateCard{
		InputPer1MNanoUSD: 10_000_000,
	}, pricing.UsageInput{
		InputTokens:          1,
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil {
		t.Fatalf("Calculate sub-micro: %v", err)
	}
	if subMicro.TotalNanoUSD != 10 || subMicro.TotalMicroUSD != 1 {
		t.Fatalf("expected 10 nano / ceil 1 micro, got %+v", subMicro)
	}

	// Product tokens * rate exceeds MaxInt64 before / 1_000_000 (2_000_000_000 * 10_000_000_000 = 2e19 > 9.22e18):
	// 128-bit intermediate math must succeed accurately without false overflow!
	huge, err := pricing.Calculate(pricing.RateCard{
		InputPer1MNanoUSD: 10_000_000_000, // $10 / 1M
	}, pricing.UsageInput{
		InputTokens:          2_000_000_000, // 2B tokens -> $20,000 -> 20_000_000_000_000 nano -> 20_000_000_000 micro
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil {
		t.Fatalf("large 128-bit calculation overflowed prematurely: %v", err)
	}
	if huge.TotalMicroUSD != 20_000_000_000 {
		t.Fatalf("expected 20_000_000_000 micro-USD, got %d", huge.TotalMicroUSD)
	}

	// True final int64 overflow must still error explicitly.
	_, err = pricing.Calculate(pricing.RateCard{
		InputPer1MNanoUSD: math.MaxInt64,
	}, pricing.UsageInput{
		InputTokens:          2_000_000_000,
		UpstreamUsagePresent: true,
	})
	if !errors.Is(err, pricing.ErrArithmeticOverflow) {
		t.Fatalf("expected ErrArithmeticOverflow, got %v", err)
	}
}

func TestFMEA04AccuracyBadgesAndUnmetered(t *testing.T) {
	commercialRate := pricing.RateCard{
		InputPer1MNanoUSD:  1_000_000_000,
		OutputPer1MNanoUSD: 2_000_000_000,
	}

	// Case 1: Aborted SSE stream with missing terminal usage chunk -> settles emitted output bytes as ESTIMATED.
	aborted, err := pricing.Calculate(commercialRate, pricing.UsageInput{
		UpstreamUsagePresent: false,
		FallbackTextBytes:    30, // 10 tokens -> 10_000 nano
		FallbackOutputBytes:  45, // 15 tokens -> 30_000 nano
	})
	if err != nil {
		t.Fatalf("Calculate aborted stream: %v", err)
	}
	if aborted.Accuracy != pricing.AccuracyEstimated {
		t.Fatalf("expected ESTIMATED on missing usage chunk, got %q", aborted.Accuracy)
	}
	if aborted.TotalMicroUSD != 40 {
		t.Fatalf("expected 40 micro-USD, got %d", aborted.TotalMicroUSD)
	}

	// Case 2: Bogus upstream usage claiming EXACT with 0 tokens despite non-empty payloads -> corrected to ESTIMATED.
	bogusExact, err := pricing.Calculate(commercialRate, pricing.UsageInput{
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
		FallbackTextBytes:    12,
		FallbackOutputBytes:  15,
	})
	if err != nil {
		t.Fatalf("Calculate bogus exact: %v", err)
	}
	if bogusExact.Accuracy != pricing.AccuracyEstimated || bogusExact.TotalMicroUSD == 0 {
		t.Fatalf("expected FMEA-04 ESTIMATED non-zero recovery, got %+v", bogusExact)
	}

	// Case 3: Unpriced self-hosted model -> 0 micro-USD + UNMETERED_FLAGGED + UnmeteredFlagged=true.
	unpriced, err := pricing.Calculate(pricing.RateCard{
		BillingMode: pricing.BillingModeSelfHostedInfra,
	}, pricing.UsageInput{
		InputTokens:          500,
		OutputTokens:         200,
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil {
		t.Fatalf("Calculate unpriced: %v", err)
	}
	if unpriced.TotalMicroUSD != 0 || unpriced.Accuracy != pricing.AccuracyUnmeteredFlagged || !unpriced.UnmeteredFlagged {
		t.Fatalf("expected UNMETERED_FLAGGED with 0 cost, got %+v", unpriced)
	}

	// Case 4: Self-hosted model explicitly configured with SelfHostedZeroRate=true -> 0 micro-USD WITHOUT unmetered flag.
	zeroRated, err := pricing.Calculate(pricing.RateCard{
		BillingMode:        pricing.BillingModeSelfHostedInfra,
		SelfHostedZeroRate: true,
	}, pricing.UsageInput{
		InputTokens:          500,
		OutputTokens:         200,
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
	})
	if err != nil {
		t.Fatalf("Calculate zeroRated: %v", err)
	}
	if zeroRated.TotalMicroUSD != 0 || zeroRated.Accuracy != pricing.AccuracyExact || zeroRated.UnmeteredFlagged {
		t.Fatalf("expected explicit zero-rate self-hosted EXACT without unmetered flag, got %+v", zeroRated)
	}
}

func TestParseMediaUsageJSONWithoutFloats(t *testing.T) {
	raw := `{"image_count":2,"image_pixels":1024,"audio_input_seconds":"1.25","audio_output_seconds":2,"document_pages":4,"rerank_search_units":3}`
	m, err := pricing.ParseMediaUsageJSON(raw)
	if err != nil {
		t.Fatalf("ParseMediaUsageJSON failed: %v", err)
	}
	if m.ImageCount != 2 || m.ImagePixels != 1024 || m.AudioInputMillis != 1250 || m.AudioOutputMillis != 2000 || m.DocumentPages != 4 || m.RerankSearchUnits != 3 {
		t.Fatalf("unexpected decoded MediaUsage: %+v", m)
	}
}
