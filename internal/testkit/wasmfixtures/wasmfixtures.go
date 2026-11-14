// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package wasmfixtures embeds auditable .wat sources paired with pre-assembled
// WebAssembly 1.0 binary fixtures for hermetic Wasm sandbox testing across
// Linux, macOS, and Windows without requiring host TinyGo or wat2wasm.
package wasmfixtures

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
)

//go:embed fixtures/*.wat fixtures/*.wasm
var fixtureFS embed.FS

// Canonical fixture base names matching TP-IMP-0006.
const (
	NameEchoTransform        = "echo_transform"
	NameInfiniteLoop         = "infinite_loop"
	NameOOM64MiB             = "oom_64mib"
	NameUnreachableTrap      = "unreachable_trap"
	NameUnapprovedWASIImport = "unapproved_wasi_import"
)

var orderedNames = []string{
	NameEchoTransform,
	NameInfiniteLoop,
	NameOOM64MiB,
	NameUnreachableTrap,
	NameUnapprovedWASIImport,
}

// Fixture holds a named adversarial or reference WebAssembly test module paired
// with its human-auditable WAT source.
type Fixture struct {
	Name   string
	WAT    string
	wasm   []byte
	SHA256 string
}

// WASM returns a fresh copy of the compiled WebAssembly module bytes.
func (f Fixture) WASM() []byte {
	out := make([]byte, len(f.wasm))
	copy(out, f.wasm)
	return out
}

func loadFixture(name string) (Fixture, error) {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".wasm"), ".wat")
	watRaw, err := fixtureFS.ReadFile("fixtures/" + base + ".wat")
	if err != nil {
		return Fixture{}, fmt.Errorf("read wat fixture %q: %w", base, err)
	}
	wasmRaw, err := fixtureFS.ReadFile("fixtures/" + base + ".wasm")
	if err != nil {
		return Fixture{}, fmt.Errorf("read wasm fixture %q: %w", base, err)
	}
	sum := sha256.Sum256(wasmRaw)
	return Fixture{
		Name:   base,
		WAT:    string(watRaw),
		wasm:   wasmRaw,
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}

// All returns all 5 canonical pre-assembled fixtures in deterministic order.
func All() ([]Fixture, error) {
	out := make([]Fixture, 0, len(orderedNames))
	for _, name := range orderedNames {
		f, err := loadFixture(name)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// Get loads a single fixture by base name or filename (e.g. "echo_transform" or "echo_transform.wasm").
func Get(name string) (Fixture, error) {
	return loadFixture(name)
}

func mustBytes(name string) []byte {
	f, err := loadFixture(name)
	if err != nil {
		panic(err)
	}
	return f.WASM()
}

// EchoTransformWASM returns a copy of echo_transform.wasm.
func EchoTransformWASM() []byte { return mustBytes(NameEchoTransform) }

// InfiniteLoopWASM returns a copy of infinite_loop.wasm.
func InfiniteLoopWASM() []byte { return mustBytes(NameInfiniteLoop) }

// OOM64MiBWASM returns a copy of oom_64mib.wasm.
func OOM64MiBWASM() []byte { return mustBytes(NameOOM64MiB) }

// UnreachableTrapWASM returns a copy of unreachable_trap.wasm.
func UnreachableTrapWASM() []byte { return mustBytes(NameUnreachableTrap) }

// UnapprovedWASIImportWASM returns a copy of unapproved_wasi_import.wasm.
func UnapprovedWASIImportWASM() []byte { return mustBytes(NameUnapprovedWASIImport) }

// ImportEntry describes one declared import entry inside a WebAssembly module.
type ImportEntry struct {
	Module string
	Name   string
	Kind   byte
}

// ExportEntry describes one declared export entry inside a WebAssembly module.
type ExportEntry struct {
	Name  string
	Kind  byte
	Index uint32
}

// ModuleSummary captures structural metadata parsed from a WebAssembly 1.0 binary.
type ModuleSummary struct {
	Imports       []ImportEntry
	Exports       []ExportEntry
	HasMemoryMax  bool
	MemoryMin     uint32
	MemoryMax     uint32
	CodeFuncCount uint32
}

var wasmPreamble = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// ParseModuleSummary validates WebAssembly 1.0 header/section framing and extracts
// imports, exports, memory bounds, and function count without external dependencies.
func ParseModuleSummary(data []byte) (ModuleSummary, error) {
	var summary ModuleSummary
	if len(data) < len(wasmPreamble) || !bytes.Equal(data[:len(wasmPreamble)], wasmPreamble) {
		return summary, fmt.Errorf("invalid WebAssembly magic/version preamble")
	}

	pos := len(wasmPreamble)
	for pos < len(data) {
		secID := data[pos]
		pos++
		secLen, nextPos, err := readULEB32(data, pos)
		if err != nil {
			return summary, fmt.Errorf("section %d length: %w", secID, err)
		}
		pos = nextPos
		if pos+int(secLen) > len(data) {
			return summary, fmt.Errorf("section %d length %d exceeds remaining bytes %d", secID, secLen, len(data)-pos)
		}
		secPayload := data[pos : pos+int(secLen)]
		pos += int(secLen)

		switch secID {
		case 2: // Import section
			imports, err := parseImportSection(secPayload)
			if err != nil {
				return summary, err
			}
			summary.Imports = imports
		case 5: // Memory section
			hasMax, minPages, maxPages, err := parseMemorySection(secPayload)
			if err != nil {
				return summary, err
			}
			summary.HasMemoryMax = hasMax
			summary.MemoryMin = minPages
			summary.MemoryMax = maxPages
		case 7: // Export section
			exports, err := parseExportSection(secPayload)
			if err != nil {
				return summary, err
			}
			summary.Exports = exports
		case 10: // Code section
			cnt, _, err := readULEB32(secPayload, 0)
			if err != nil {
				return summary, fmt.Errorf("code count: %w", err)
			}
			summary.CodeFuncCount = cnt
		}
	}
	return summary, nil
}

func readULEB32(b []byte, pos int) (uint32, int, error) {
	var val uint32
	var shift uint
	for i := 0; i < 5; i++ {
		if pos >= len(b) {
			return 0, pos, fmt.Errorf("unexpected EOF reading uleb128")
		}
		cur := b[pos]
		pos++
		val |= uint32(cur&0x7f) << shift
		if cur&0x80 == 0 {
			return val, pos, nil
		}
		shift += 7
	}
	return 0, pos, fmt.Errorf("uleb128 overflow")
}

func readName(b []byte, pos int) (string, int, error) {
	ln, p, err := readULEB32(b, pos)
	if err != nil {
		return "", p, err
	}
	if p+int(ln) > len(b) {
		return "", p, fmt.Errorf("name length %d overflows section", ln)
	}
	return string(b[p : p+int(ln)]), p + int(ln), nil
}

func parseImportSection(sec []byte) ([]ImportEntry, error) {
	count, pos, err := readULEB32(sec, 0)
	if err != nil {
		return nil, fmt.Errorf("import count: %w", err)
	}
	out := make([]ImportEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		modName, next, err := readName(sec, pos)
		if err != nil {
			return nil, fmt.Errorf("import[%d] module: %w", i, err)
		}
		fieldName, next, err := readName(sec, next)
		if err != nil {
			return nil, fmt.Errorf("import[%d] field: %w", i, err)
		}
		if next >= len(sec) {
			return nil, fmt.Errorf("import[%d] missing kind", i)
		}
		kind := sec[next]
		next++
		if kind == 0x00 { // func type index
			_, next, err = readULEB32(sec, next)
			if err != nil {
				return nil, fmt.Errorf("import[%d] typeidx: %w", i, err)
			}
		}
		pos = next
		out = append(out, ImportEntry{Module: modName, Name: fieldName, Kind: kind})
	}
	return out, nil
}

func parseMemorySection(sec []byte) (bool, uint32, uint32, error) {
	count, pos, err := readULEB32(sec, 0)
	if err != nil {
		return false, 0, 0, fmt.Errorf("memory count: %w", err)
	}
	if count == 0 || pos >= len(sec) {
		return false, 0, 0, fmt.Errorf("empty memory section")
	}
	flags := sec[pos]
	pos++
	minPages, pos, err := readULEB32(sec, pos)
	if err != nil {
		return false, 0, 0, fmt.Errorf("memory min: %w", err)
	}
	var maxPages uint32
	hasMax := flags&0x01 != 0
	if hasMax {
		maxPages, _, err = readULEB32(sec, pos)
		if err != nil {
			return false, 0, 0, fmt.Errorf("memory max: %w", err)
		}
	}
	return hasMax, minPages, maxPages, nil
}

func parseExportSection(sec []byte) ([]ExportEntry, error) {
	count, pos, err := readULEB32(sec, 0)
	if err != nil {
		return nil, fmt.Errorf("export count: %w", err)
	}
	out := make([]ExportEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		name, next, err := readName(sec, pos)
		if err != nil {
			return nil, fmt.Errorf("export[%d] name: %w", i, err)
		}
		if next >= len(sec) {
			return nil, fmt.Errorf("export[%d] missing kind", i)
		}
		kind := sec[next]
		next++
		idx, next, err := readULEB32(sec, next)
		if err != nil {
			return nil, fmt.Errorf("export[%d] idx: %w", i, err)
		}
		pos = next
		out = append(out, ExportEntry{Name: name, Kind: kind, Index: idx})
	}
	return out, nil
}
