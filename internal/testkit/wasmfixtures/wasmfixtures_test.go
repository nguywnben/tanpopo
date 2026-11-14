// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package wasmfixtures

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	watMemoryRe = regexp.MustCompile(`\(memory\s+\(export\s+"([^"]+)"\)\s+(\d+)(?:\s+(\d+))?\)`)
	watExportRe = regexp.MustCompile(`\(func\s+\(export\s+"([^"]+)"\)`)
	watImportRe = regexp.MustCompile(`\(import\s+"([^"]+)"\s+"([^"]+)"`)
)

func TestAllCanonicalWasmFixturesValidAndAuditable(t *testing.T) {
	t.Parallel()

	all, err := All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 canonical fixtures, got %d", len(all))
	}

	direct := map[string][]byte{
		NameEchoTransform:        EchoTransformWASM(),
		NameInfiniteLoop:         InfiniteLoopWASM(),
		NameOOM64MiB:             OOM64MiBWASM(),
		NameUnreachableTrap:      UnreachableTrapWASM(),
		NameUnapprovedWASIImport: UnapprovedWASIImportWASM(),
	}

	for _, f := range all {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(f.WAT, "(module") {
				t.Fatalf("fixture %s missing S-expression (module WAT source", f.Name)
			}
			if len(f.SHA256) != 64 {
				t.Fatalf("fixture %s invalid SHA256 %q", f.Name, f.SHA256)
			}
			if !bytes.Equal(f.WASM(), direct[f.Name]) {
				t.Fatalf("direct getter mismatch for %s", f.Name)
			}

			// Mutating returned slice never corrupts embedded fixture state.
			mut := f.WASM()
			mut[0] ^= 0xff
			if bytes.Equal(mut, f.WASM()) {
				t.Fatalf("expected defensive copy from WASM()")
			}

			gotByName, err := Get(f.Name + ".wasm")
			if err != nil || !bytes.Equal(gotByName.WASM(), f.WASM()) {
				t.Fatalf("Get(%s.wasm) mismatch: %v", f.Name, err)
			}

			summary, err := ParseModuleSummary(f.WASM())
			if err != nil {
				t.Fatalf("ParseModuleSummary(%s): %v", f.Name, err)
			}
			if summary.CodeFuncCount == 0 {
				t.Fatalf("fixture %s expected >= 1 defined function", f.Name)
			}

			hasTransform := false
			hasGrow64m := false
			for _, exp := range summary.Exports {
				if exp.Name == "transform" {
					hasTransform = true
				}
				if exp.Name == "grow_64m" {
					hasGrow64m = true
				}
			}
			if !hasTransform {
				t.Fatalf("fixture %s missing exported transform entrypoint", f.Name)
			}

			// Cross-check WAT declarations against parsed WASM binary metadata.
			memMatch := watMemoryRe.FindStringSubmatch(f.WAT)
			if memMatch == nil {
				t.Fatalf("fixture %s WAT missing canonical memory export declaration", f.Name)
			}
			watMin, _ := strconv.ParseUint(memMatch[2], 10, 32)
			if uint32(watMin) != summary.MemoryMin {
				t.Fatalf("fixture %s memory min mismatch: wat=%d wasm=%d", f.Name, watMin, summary.MemoryMin)
			}
			if memMatch[3] != "" {
				watMax, _ := strconv.ParseUint(memMatch[3], 10, 32)
				if !summary.HasMemoryMax || uint32(watMax) != summary.MemoryMax {
					t.Fatalf("fixture %s memory max mismatch: wat=%d wasm=%d hasMax=%v", f.Name, watMax, summary.MemoryMax, summary.HasMemoryMax)
				}
			} else if summary.HasMemoryMax {
				t.Fatalf("fixture %s WAT unbounded memory mismatch against wasm max=%d", f.Name, summary.MemoryMax)
			}

			wasmExports := make(map[string]bool, len(summary.Exports))
			for _, exp := range summary.Exports {
				wasmExports[exp.Name] = true
			}
			for _, em := range watExportRe.FindAllStringSubmatch(f.WAT, -1) {
				if !wasmExports[em[1]] {
					t.Fatalf("fixture %s WAT exported func %q missing in WASM binary exports", f.Name, em[1])
				}
			}
			watImports := watImportRe.FindAllStringSubmatch(f.WAT, -1)
			if len(watImports) != len(summary.Imports) {
				t.Fatalf("fixture %s import count mismatch: wat=%d wasm=%d", f.Name, len(watImports), len(summary.Imports))
			}
			for idx, im := range watImports {
				if summary.Imports[idx].Module != im[1] || summary.Imports[idx].Name != im[2] {
					t.Fatalf("fixture %s import[%d] mismatch: wat=(%s,%s) wasm=(%s,%s)", f.Name, idx, im[1], im[2], summary.Imports[idx].Module, summary.Imports[idx].Name)
				}
			}

			switch f.Name {
			case NameOOM64MiB:
				if summary.HasMemoryMax || !hasGrow64m {
					t.Fatalf("oom_64mib expected unbounded guest memory + grow_64m export, got hasMax=%v grow64m=%v", summary.HasMemoryMax, hasGrow64m)
				}
			case NameUnapprovedWASIImport:
				if len(summary.Imports) != 1 || summary.Imports[0].Module != "wasi_snapshot_preview1" || summary.Imports[0].Name != "path_open" {
					t.Fatalf("unapproved_wasi_import unexpected imports: %+v", summary.Imports)
				}
			default:
				if len(summary.Imports) != 0 || !summary.HasMemoryMax || summary.MemoryMax != 256 {
					t.Fatalf("%s expected 0 imports and 256 max pages, got imports=%d hasMax=%v max=%d", f.Name, len(summary.Imports), summary.HasMemoryMax, summary.MemoryMax)
				}
			}
		})
	}
}

func TestWasmFixturesNegativeAndErrorPaths(t *testing.T) {
	t.Parallel()

	if _, err := Get("nonexistent_fixture"); err == nil {
		t.Fatalf("expected Get error for missing fixture")
	}

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"bad magic", []byte("not-wasm-at-all")},
		{"truncated section len", append(append([]byte{}, wasmPreamble...), 0x01, 0x80)},
		{"section overflows buffer", append(append([]byte{}, wasmPreamble...), 0x01, 0x7f, 0x00)},
		{"corrupt import section", append(append([]byte{}, wasmPreamble...), 0x02, 0x01, 0x02)},
		{"corrupt memory section", append(append([]byte{}, wasmPreamble...), 0x05, 0x01, 0x00)},
		{"corrupt export section", append(append([]byte{}, wasmPreamble...), 0x07, 0x01, 0x02)},
		{"corrupt code section", append(append([]byte{}, wasmPreamble...), 0x0a, 0x01, 0x80)},
		{"import name overflow", append(append([]byte{}, wasmPreamble...), 0x02, 0x04, 0x01, 0x01, 'm', 0x05)},
		{"import missing kind", append(append([]byte{}, wasmPreamble...), 0x02, 0x05, 0x01, 0x01, 'm', 0x01, 'f')},
		{"memory missing min", append(append([]byte{}, wasmPreamble...), 0x05, 0x02, 0x01, 0x00)},
		{"memory bad max", append(append([]byte{}, wasmPreamble...), 0x05, 0x04, 0x01, 0x01, 0x02, 0x80)},
		{"export missing kind", append(append([]byte{}, wasmPreamble...), 0x07, 0x03, 0x01, 0x01, 'x')},
		{"export bad index", append(append([]byte{}, wasmPreamble...), 0x07, 0x04, 0x01, 0x01, 'x', 0x00)},
	} {
		if _, err := ParseModuleSummary(tc.data); err == nil {
			t.Fatalf("expected ParseModuleSummary error for %s", tc.name)
		}
	}
}
