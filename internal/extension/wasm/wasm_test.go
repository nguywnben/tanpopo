// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package wasm_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
	"github.com/nguywnben/tanpopo/internal/testkit/wasmfixtures"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

// --- Binary WebAssembly v1 builders for hermetic contract tests ---

func uleb128(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			break
		}
	}
	return out
}

func wasmString(s string) []byte {
	out := uleb128(uint32(len(s)))
	return append(out, s...)
}

func wasmSection(id byte, payload []byte) []byte {
	out := []byte{id}
	out = append(out, uleb128(uint32(len(payload)))...)
	return append(out, payload...)
}

// buildImportRejectWASM builds a module importing (mod, fn) -> () so NEG-2 can verify
// default-deny rejection across filesystem, environment, and socket WASI syscalls.
func buildImportRejectWASM(modName, fnName string) []byte {
	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	// Type 0: () -> ()
	out = append(out, wasmSection(1, []byte{0x01, 0x60, 0x00, 0x00})...)
	// Import section
	var imp []byte
	imp = append(imp, 0x01)
	imp = append(imp, wasmString(modName)...)
	imp = append(imp, wasmString(fnName)...)
	imp = append(imp, 0x00, 0x00) // func index type 0
	out = append(out, wasmSection(2, imp)...)
	// Export memory
	out = append(out, wasmSection(5, []byte{0x01, 0x01, 0x01, 0x80, 0x02})...)
	var exp []byte
	exp = append(exp, 0x01)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	out = append(out, wasmSection(7, exp)...)
	return out
}

// buildMultiMethodCanonicalWASM compiles a Wasm module exporting all canonical ExtensionRuntime
// entrypoints, each returning a distinct JSON payload stored in linear memory data segments.
func buildMultiMethodCanonicalWASM(responses map[string][]byte) []byte {
	type segment struct {
		name   string
		offset uint32
		data   []byte
	}
	names := make([]string, 0, len(responses))
	for k := range responses {
		names = append(names, k)
	}
	// deterministic order
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

	cursor := uint32(65536)
	segs := make([]segment, 0, len(names))
	for _, n := range names {
		payload := responses[n]
		segs = append(segs, segment{name: n, offset: cursor, data: payload})
		cursor += uint32(len(payload)) + 16
	}
	minPages := uint32((cursor / wasm.WasmPageBytes) + 1)
	if minPages < 2 {
		minPages = 2
	}

	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	// Type 0: (i32, i32) -> i64
	out = append(out, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	// Function section: N funcs of type 0
	var fnSec []byte
	fnSec = append(fnSec, uleb128(uint32(len(segs)))...)
	for range segs {
		fnSec = append(fnSec, 0x00)
	}
	out = append(out, wasmSection(3, fnSec)...)
	// Memory: limits min=minPages max=256
	var memSec []byte
	memSec = append(memSec, 0x01, 0x01)
	memSec = append(memSec, uleb128(minPages)...)
	memSec = append(memSec, uleb128(256)...)
	out = append(out, wasmSection(5, memSec)...)
	// Export: "memory" + all functions
	var expSec []byte
	expSec = append(expSec, uleb128(uint32(1+len(segs)))...)
	expSec = append(expSec, wasmString("memory")...)
	expSec = append(expSec, 0x02, 0x00)
	for i, s := range segs {
		expSec = append(expSec, wasmString(s.name)...)
		expSec = append(expSec, 0x00)
		expSec = append(expSec, uleb128(uint32(i))...)
	}
	out = append(out, wasmSection(7, expSec)...)

	// Code section: each func returns ((uint64(offset) << 32) | uint64(len))
	var codeSec []byte
	codeSec = append(codeSec, uleb128(uint32(len(segs)))...)
	for _, s := range segs {
		packed := (uint64(s.offset) << 32) | uint64(uint32(len(s.data)))
		var body []byte
		body = append(body, 0x00) // 0 locals
		body = append(body, 0x42) // i64.const sleb128 (encode up to unsigned 64-bit safely)
		body = append(body, sleb128Int64(int64(packed))...)
		body = append(body, 0x0b)
		codeSec = append(codeSec, uleb128(uint32(len(body)))...)
		codeSec = append(codeSec, body...)
	}
	out = append(out, wasmSection(10, codeSec)...)

	// Data section: active segments initialized cleanly on each slot instantiation
	var dataSec []byte
	dataSec = append(dataSec, uleb128(uint32(len(segs)))...)
	for _, s := range segs {
		dataSec = append(dataSec, 0x00) // memory index 0 active
		dataSec = append(dataSec, 0x41) // i32.const
		dataSec = append(dataSec, sleb128Int64(int64(s.offset))...)
		dataSec = append(dataSec, 0x0b) // end expr
		dataSec = append(dataSec, uleb128(uint32(len(s.data)))...)
		dataSec = append(dataSec, s.data...)
	}
	out = append(out, wasmSection(11, dataSec)...)
	return out
}

func sleb128Int64(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		more := !((v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0))
		if more {
			b |= 0x80
		}
		out = append(out, b)
		if !more {
			break
		}
	}
	return out
}

// buildPackedEchoReaderWASM compiles a guest where `transform(ptr, len)` reads uint64 from
// mem[ptr:ptr+8] and returns it directly (used for FZ-05 ABI pointer boundary fuzzing).
func buildPackedReturnReaderWASM() []byte {
	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	out = append(out, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	out = append(out, wasmSection(3, []byte{0x01, 0x00})...)
	out = append(out, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x80, 0x02})...)
	var exp []byte
	exp = append(exp, 0x02)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wasmString("transform")...)
	exp = append(exp, 0x00, 0x00)
	out = append(out, wasmSection(7, exp)...)
	// func body: local.get 0 -> i64.load align=0 offset=0 -> end
	body := []byte{0x00, 0x20, 0x00, 0x29, 0x00, 0x00, 0x0b}
	var code []byte
	code = append(code, 0x01)
	code = append(code, uleb128(uint32(len(body)))...)
	code = append(code, body...)
	out = append(out, wasmSection(10, code)...)
	return out
}

// buildScratchWriterWASM compiles a guest that writes input bytes into scratch offset 90000,
// traps after dirtying scratch if input starts with '!', and returns mem[90000:90032] on '?'.
func buildScratchWriterWASM() []byte {
	scratchLEB := sleb128Int64(90000)
	var body []byte
	body = append(body, 0x00) // 0 locals
	body = append(body,
		0x20, 0x01, 0x41, 0x00, 0x4b, // local.get 1 > 0 (u)
		0x04, 0x40,
		0x20, 0x00, 0x2d, 0x00, 0x00, // i32.load8_u (ptr)
		0x41, 0x3f, 0x46, // == '?'
		0x04, 0x40,
		0x42,
	)
	body = append(body, scratchLEB...)
	body = append(body,
		0x42, 0x20, 0x86, // i64.const 90000 << 32
		0x42, 0x20, 0x84, // | 32
		0x0f, // return
		0x0b, // end if == '?'
		0x41,
	)
	body = append(body, scratchLEB...) // i32.const 90000
	body = append(body,
		0x20, 0x00, // src = ptr
		0x20, 0x01, // len
		0xfc, 0x0a, 0x00, 0x00, // memory.copy
		0x20, 0x00, 0x2d, 0x00, 0x00,
		0x41, 0x21, 0x46, // == '!'
		0x04, 0x40,
		0x00,                   // unreachable trap after dirtying scratch
		0x0b,                   // end if == '!'
		0x0b,                   // end if len > 0
		0x41, 0x80, 0x80, 0x04, // dst = 65536
		0x20, 0x00,
		0x20, 0x01,
		0xfc, 0x0a, 0x00, 0x00,
		0x42, 0x80, 0x80, 0x04, 0x42, 0x20, 0x86,
		0x20, 0x01, 0xad, 0x84,
		0x0b,
	)
	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	out = append(out, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	out = append(out, wasmSection(3, []byte{0x01, 0x00})...)
	out = append(out, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x80, 0x02})...)
	var exp []byte
	exp = append(exp, 0x02)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wasmString("transform")...)
	exp = append(exp, 0x00, 0x00)
	out = append(out, wasmSection(7, exp)...)
	var code []byte
	code = append(code, 0x01)
	code = append(code, uleb128(uint32(len(body)))...)
	code = append(code, body...)
	out = append(out, wasmSection(10, code)...)
	return out
}

// buildHostEgressBridgeWASM imports tanpopo_host_v1.blind_http_egress(reqPtr, reqLen, respPtr, respCap)->i32
// and calls it inside exported `transform(ptr, len)->i64`, writing output into offset 65536.
func buildHostEgressBridgeWASM() []byte {
	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	// Type 0: (i32,i32,i32,i32)->i32 ; Type 1: (i32,i32)->i64
	typeSec := []byte{
		0x02,
		0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
	}
	out = append(out, wasmSection(1, typeSec)...)
	// Import func 0: tanpopo_host_v1.blind_http_egress type 0
	var imp []byte
	imp = append(imp, 0x01)
	imp = append(imp, wasmString(wasm.HostModuleName)...)
	imp = append(imp, wasmString(wasm.HostFuncBlindHTTPEgress)...)
	imp = append(imp, 0x00, 0x00)
	out = append(out, wasmSection(2, imp)...)
	// Func 1: defined func type 1
	out = append(out, wasmSection(3, []byte{0x01, 0x01})...)
	// Memory min=2 max=256
	out = append(out, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x80, 0x02})...)
	// Export "memory" and "transform" (func index 1!)
	var exp []byte
	exp = append(exp, 0x02)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wasmString("transform")...)
	exp = append(exp, 0x00, 0x01)
	out = append(out, wasmSection(7, exp)...)

	// Body: local 0 = n; n = call 0(ptr, len, 65536, 32768); if n < 0 { trap }; return (65536 << 32) | n
	body := []byte{
		0x01, 0x01, 0x7f, // 1 local: i32
		0x20, 0x00, // local.get ptr
		0x20, 0x01, // local.get len
		0x41, 0x80, 0x80, 0x04, // respPtr = 65536
		0x41, 0x80, 0x80, 0x02, // respCap = 32768
		0x10, 0x00, // call import 0
		0x22, 0x02, // local.tee 2 (n)
		0x41, 0x00, // 0
		0x48,       // i32.lt_s (n < 0)
		0x04, 0x40, // if
		0x00,                                     //   unreachable
		0x0b,                                     // end if
		0x42, 0x80, 0x80, 0x04, 0x42, 0x20, 0x86, // i64.const 65536 << 32
		0x20, 0x02, 0xad, // i64.extend_i32_u(n)
		0x84, // i64.or
		0x0b,
	}
	var code []byte
	code = append(code, 0x01)
	code = append(code, uleb128(uint32(len(body)))...)
	code = append(code, body...)
	out = append(out, wasmSection(10, code)...)
	return out
}

// --- Test Harness Helpers ---

type memAuditSink struct {
	mu     sync.Mutex
	events []storageport.AuditEvent
}

func (s *memAuditSink) Append(_ context.Context, ev storageport.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}

func (s *memAuditSink) countType(t string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.EventType == t {
			n++
		}
	}
	return n
}

func (s *memAuditSink) snapshot() []storageport.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]storageport.AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

// sqliteAuditSink writes directly into physical SQLite audit_events so schema CHECK constraints
// (such as audit_events.entry_point) are exercised end-to-end.
type sqliteAuditSink struct {
	mu     sync.Mutex
	db     *sql.DB
	seq    int64
	events []storageport.AuditEvent
}

func newSQLiteAuditSink(t *testing.T, db *sql.DB) *sqliteAuditSink {
	t.Helper()
	// Canonical audit_events DDL matching db/spec-check/sqlite_schema.sql (TP-IMP-0023 owns full hash-chaining).
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_events (
			id            VARCHAR(40) NOT NULL,
			created_at_ms BIGINT NOT NULL,
			org_id        VARCHAR(40),
			chain_key     VARCHAR(40) NOT NULL,
			seq_no        BIGINT NOT NULL CHECK (seq_no >= 1),
			actor_kind    VARCHAR(24) NOT NULL,
			actor_id      VARCHAR(64) NOT NULL,
			entry_point   VARCHAR(24) NOT NULL
			              CHECK (entry_point IN ('admin_api', 'cli', 'gitops_boot', 'system_reaper', 'data_plane_guard')),
			event_type    VARCHAR(64) NOT NULL,
			target_type   VARCHAR(32) NOT NULL,
			target_id     VARCHAR(64) NOT NULL,
			request_id    VARCHAR(40),
			metadata_json TEXT NOT NULL,
			prev_hash     CHAR(64) NOT NULL,
			entry_hash    CHAR(64) NOT NULL,
			CHECK ((org_id IS NULL AND chain_key = 'SYSTEM') OR (org_id IS NOT NULL AND chain_key = org_id)),
			PRIMARY KEY (chain_key, seq_no, created_at_ms),
			UNIQUE (chain_key, seq_no),
			UNIQUE (id, created_at_ms)
		)`); err != nil {
		t.Fatalf("create audit_events table: %v", err)
	}
	return &sqliteAuditSink{db: db}
}

func (s *sqliteAuditSink) Append(ctx context.Context, ev storageport.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	chainKey := "SYSTEM"
	var orgArg any
	if ev.OrgID != nil && *ev.OrgID != "" {
		chainKey = *ev.OrgID
		orgArg = *ev.OrgID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, created_at_ms, org_id, chain_key, seq_no,
			actor_kind, actor_id, entry_point, event_type,
			target_type, target_id, request_id, metadata_json,
			prev_hash, entry_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.CreatedAtMs, orgArg, chainKey, s.seq,
		ev.ActorKind, ev.ActorID, ev.EntryPoint, ev.EventType,
		ev.TargetType, ev.TargetID, ev.RequestID, ev.MetadataJSON,
		strings.Repeat("0", 64), strings.Repeat("a", 64),
	)
	if err != nil {
		return err
	}
	s.events = append(s.events, ev)
	return nil
}

func (s *sqliteAuditSink) countType(t string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.EventType == t {
			n++
		}
	}
	return n
}

type staticSlotResolver struct {
	mu       sync.Mutex
	slots    map[string]*broker.ResolvedSlot
	policies map[string]*egress.Policy
}

func (r *staticSlotResolver) put(s *broker.ResolvedSlot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots[s.OrgID+"|"+s.ConnectionID+"|"+s.SlotName] = s
	if r.policies == nil {
		r.policies = make(map[string]*egress.Policy)
	}
	r.policies[s.OrgID+"|"+s.ConnectionID] = &egress.Policy{
		AllowedHosts:   append([]string(nil), s.AllowedHosts...),
		AllowPrivateIP: s.AllowPrivateIP,
		AllowedCIDRs:   append([]string(nil), s.AllowedCIDRs...),
		AllowedPorts:   append([]int(nil), s.AllowedPorts...),
		RequireTLS:     s.RequireTLS,
	}
}

func (r *staticSlotResolver) ResolveSlot(_ context.Context, orgID, connID, slotName string) (*broker.ResolvedSlot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.slots[orgID+"|"+connID+"|"+slotName]
	if !ok {
		return nil, errTestSlotNotFound
	}
	return s, nil
}

func (r *staticSlotResolver) ResolveConnectionPolicy(_ context.Context, orgID, connID string) (*egress.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[orgID+"|"+connID]
	if !ok {
		return nil, errTestSlotNotFound
	}
	cp := *p
	return &cp, nil
}

func buildTestTPKG(t *testing.T, files map[string][]byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, body := range files {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(body)),
			ModTime: time.Unix(0, 0).UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s): %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

var errTestSlotNotFound = errors.New("test: credential slot not found")

func countSandboxGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	text := string(buf[:n])
	blocks := strings.Split(text, "\n\n")
	cnt := 0
	for _, b := range blocks {
		if strings.Contains(b, "internal/extension/wasm.") && !strings.Contains(b, "countSandboxGoroutines") {
			cnt++
		}
	}
	return cnt
}

// --- Unit, Acceptance & Negative Tests ---

func TestWasmAC1ConcurrentTransformAndZeroSecret(t *testing.T) {
	// Sequential: 100 concurrent streams avoid intra-package CPU contention under -race on slower runners.
	ctx := context.Background()

	secret := canary.DefaultCanarySecret()
	scanner := canary.NewScanner()

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:          wasmfixtures.EchoTransformWASM(),
		PoolSize:           16,
		MemoryLimitPages:   wasm.DefaultMemoryLimitPages,
		CallTimeout:        2 * time.Second,
		StreamChunkTimeout: wasm.MaxAllowedStreamChunkTimeout,
		ABIMode:            wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{
			Method:    http.MethodPost,
			TargetURL: "https://api.acme.example/v1/chat/completions",
			CredentialInjections: []port.CredentialInjectInstruction{
				{
					SecretSlotName: "api_key",
					TargetLocation: port.CredentialTargetLocationHeader,
					FieldName:      "Authorization",
					ValuePrefix:    "Bearer ",
				},
			},
		},
		AllowedHosts:        []string{"api.acme.example"},
		ApprovedSecretSlots: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	const streams = 100
	const chunks = 50
	var wg sync.WaitGroup
	errCh := make(chan error, streams)

	for s := 0; s < streams; s++ {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqPayload := fmt.Appendf(nil, `{"stream_id":%d,"model":"acme-chat"}`, s)
			spec, tErr := rt.TransformRequest(ctx, port.TransformRequestInput{
				RequestID:             fmt.Sprintf("req_%d", s),
				NormalizedRequestJSON: reqPayload,
				SecretSlotRefs:        map[string]string{"api_key": "sec_dummy_ref"},
			})
			if tErr != nil {
				errCh <- fmt.Errorf("stream %d TransformRequest: %w", s, tErr)
				return
			}
			if !bytes.Equal(spec.BodyBytes, reqPayload) {
				errCh <- fmt.Errorf("stream %d body mismatch: got %q want %q", s, spec.BodyBytes, reqPayload)
				return
			}
			if len(scanner.ScanString("body", string(spec.BodyBytes))) > 0 {
				errCh <- fmt.Errorf("stream %d leaked canary secret in body", s)
				return
			}

			for c := 0; c < chunks; c++ {
				wantFrame := fmt.Appendf(nil, "data: {\"s\":%d,\"c\":%d}\n\n", s, c)
				out, cErr := rt.TransformStreamChunk(ctx, port.StreamChunkInput{
					RequestID:        fmt.Sprintf("req_%d", s),
					RawUpstreamChunk: wantFrame,
					IsEOF:            c == chunks-1,
				})
				if cErr != nil {
					errCh <- fmt.Errorf("stream %d chunk %d: %w", s, c, cErr)
					return
				}
				if len(out.NormalizedSSEFrames) != 1 || !bytes.Equal(out.NormalizedSSEFrames[0], wantFrame) {
					errCh <- fmt.Errorf("stream %d chunk %d frame mismatch", s, c)
					return
				}
				if out.StreamCompleted != (c == chunks-1) {
					errCh <- fmt.Errorf("stream %d chunk %d eof mismatch", s, c)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}

	if err := rt.InspectLinearMemory(func(mem []byte) bool {
		return bytes.Contains(mem, []byte(secret)) || bytes.Contains(mem, []byte("stream_id"))
	}); err != nil {
		t.Fatalf("linear memory hygiene violation: %v", err)
	}
}

func TestWasmAC2InFlightCancellation(t *testing.T) {
	// Sequential: timing-sensitive cancellation latency check avoids intra-package CPU contention under -race.
	ctx := context.Background()

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.InfiniteLoopWASM(),
		PoolSize:         2,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages,
		CallTimeout:      5 * time.Second,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{
			Method:    http.MethodPost,
			TargetURL: "https://api.acme.example/v1/chat/completions",
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	// Case A: parentCtx canceled at 10ms under a 5s watchdog -> aborts < 100ms with context.Canceled.
	parentCtx, cancel := context.WithCancel(ctx)
	time.AfterFunc(10*time.Millisecond, cancel)
	start := time.Now()
	_, callErr := rt.TransformRequest(parentCtx, port.TransformRequestInput{
		RequestID:             "req_cancel_ctx",
		NormalizedRequestJSON: []byte(`{"hi":1}`),
	})
	elapsed := time.Since(start)
	if !errors.Is(callErr, context.Canceled) {
		t.Fatalf("expected context.Canceled on parentCtx cancel, got %v", callErr)
	}
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("expected immediate cancellation (<100ms target, 250ms -race ci guard vs 5s watchdog), took %v", elapsed)
	}

	// Case B: explicit CancelExecution RPC terminates active call immediately and returns Released=true.
	doneCh := make(chan error, 1)
	go func() {
		_, e := rt.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_rpc_cancel",
			NormalizedRequestJSON: []byte(`{"hi":2}`),
		})
		doneCh <- e
	}()
	deadline := time.Now().Add(time.Second)
	released := false
	for time.Now().Before(deadline) {
		ack, aErr := rt.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: "req_rpc_cancel", Reason: "client_abort"})
		if aErr != nil {
			t.Fatalf("CancelExecution: %v", aErr)
		}
		if ack.Released {
			released = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !released {
		t.Fatalf("expected CancelExecution to abort active request")
	}
	if err := <-doneCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from CancelExecution, got %v", err)
	}

	st := rt.Stats()
	if st.Available != 2 || st.ReplacedCount != 2 {
		t.Fatalf("expected full pool capacity=2 available and replaced=2, got %+v", st)
	}
}

func TestWasmAC3TaintedModuleReplacementAndPoolRecovery(t *testing.T) {
	// Sequential: verifies process-wide zero leaked wasm goroutines after Close().
	ctx := context.Background()

	const poolSize = 4
	runBurst := func(wasmBytes []byte, timeout time.Duration, cancelAfter time.Duration, faults int) {
		rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
			WasmBytes:        wasmBytes,
			PoolSize:         poolSize,
			MemoryLimitPages: wasm.DefaultMemoryLimitPages,
			CallTimeout:      timeout,
			WatchdogInterval: 5 * time.Millisecond,
			ABIMode:          wasm.ABIModeSliceTransform,
			AllowedHosts:     []string{"api.acme.example"},
			SliceTransform: &wasm.SliceTransformConfig{
				Method:    http.MethodPost,
				TargetURL: "https://api.acme.example/v1/chat/completions",
			},
		})
		if err != nil {
			t.Fatalf("NewRuntime: %v", err)
		}
		for i := 0; i < faults; i++ {
			cCtx := ctx
			var cFunc context.CancelFunc
			if cancelAfter > 0 {
				cCtx, cFunc = context.WithCancel(ctx)
				time.AfterFunc(cancelAfter, cFunc)
			}
			_, err := rt.TransformRawSlice(cCtx, fmt.Sprintf("fault_%d", i), []byte("payload"))
			if err == nil {
				t.Fatalf("expected fault %d to fail", i)
			}
			if cFunc != nil {
				cFunc()
			}
		}
		st := rt.Stats()
		if st.Available != poolSize || st.Capacity != poolSize || st.ReplacedCount != int64(faults) {
			t.Fatalf("pool failed full replacement: want avail=%d replaced=%d, got %+v", poolSize, faults, st)
		}
		if err := rt.Close(ctx); err != nil {
			t.Fatalf("rt.Close: %v", err)
		}
	}

	// >= 3N consecutive mixed faults across cancel, watchdog timeout, unreachable trap, and 64MiB OOM.
	runBurst(wasmfixtures.InfiniteLoopWASM(), 2*time.Second, 5*time.Millisecond, poolSize)
	runBurst(wasmfixtures.InfiniteLoopWASM(), 25*time.Millisecond, 0, poolSize)
	runBurst(wasmfixtures.UnreachableTrapWASM(), 200*time.Millisecond, 0, poolSize)
	runBurst(wasmfixtures.OOM64MiBWASM(), 200*time.Millisecond, 0, poolSize)

	// Healthy pool subjected to runtime memory-limit cap rejects > 256 pages at config time AND recovers 2N healthy calls.
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		MemoryLimitPages: 512,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected > 256 pages rejection without extended permission, got %v", err)
	}

	healthyRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		PoolSize:         poolSize,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages,
		CallTimeout:      200 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime healthy: %v", err)
	}
	for i := 0; i < 2*poolSize; i++ {
		out, err := healthyRT.TransformRawSlice(ctx, fmt.Sprintf("ok_%d", i), []byte("healthy-bytes"))
		if err != nil || string(out) != "healthy-bytes" {
			t.Fatalf("healthy call %d failed: out=%q err=%v", i, out, err)
		}
	}
	if err := healthyRT.Close(ctx); err != nil {
		t.Fatalf("healthyRT.Close: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if leaked := countSandboxGoroutines(); leaked != 0 {
		t.Fatalf("expected 0 leaked wasm goroutines after Close(), found %d", leaked)
	}
}

func TestWasmNEG1WatchdogOOMAndQuarantine(t *testing.T) {
	// Sequential: watchdog timeout latency check avoids intra-package CPU contention under -race.
	ctx := context.Background()

	// 1. Watchdog timeout returns 502 extension_execution_fault and replaces tainted module.
	loopRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.InfiniteLoopWASM(),
		PoolSize:         1,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages,
		CallTimeout:      25 * time.Millisecond,
		WatchdogInterval: 5 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime loop: %v", err)
	}
	defer loopRT.Close(ctx)

	start := time.Now()
	_, loopErr := loopRT.TransformRawSlice(ctx, "req_loop", []byte("x"))
	elapsed := time.Since(start)
	if !errors.Is(loopErr, port.ErrExecutionFault) {
		t.Fatalf("expected port.ErrExecutionFault on watchdog timeout, got %v", loopErr)
	}
	var fe *wasm.ExecutionFaultError
	if !errors.As(loopErr, &fe) || fe.HTTPStatus() != 502 || fe.CanonicalCode() != "extension_execution_fault" || fe.Kind != wasm.FaultKindTimeout {
		t.Fatalf("expected 502 extension_execution_fault timeout error, got %#v", fe)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("expected watchdog interrupt <= 200ms, took %v", elapsed)
	}

	// 2. OOM 64 MiB growth denied cleanly at 16 MiB cap without crashing host.
	oomRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.OOM64MiBWASM(),
		PoolSize:         1,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages,
		CallTimeout:      200 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime oom: %v", err)
	}
	defer oomRT.Close(ctx)
	_, oomErr := oomRT.TransformRawSlice(ctx, "req_oom", []byte("x"))
	if !errors.Is(oomErr, port.ErrExecutionFault) {
		t.Fatalf("expected port.ErrExecutionFault on 64 MiB growth denial, got %v", oomErr)
	}

	// 3. > 20 unreachable traps within 60s (FakeClock + real SQLite LifecycleService) auto-quarantines installation & isolates healthy sibling.
	tmp := t.TempDir()
	fc := clock.NewFakeClock(clock.DefaultFakeStart())
	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "faultguard.db"),
		NowMs:  func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()

	audit := newSQLiteAuditSink(t, st.RawDB())
	if err := audit.Append(ctx, storageport.AuditEvent{
		ID: "aud_bad_ep", CreatedAtMs: 1, ActorKind: "SYSTEM", ActorID: "test",
		EntryPoint: "wasm_fault_guard", EventType: "BAD", TargetType: "extension_installation", TargetID: "ins_1", MetadataJSON: "{}",
	}); err == nil {
		t.Fatalf("expected physical SQLite CHECK constraint rejection for invalid entry_point")
	}
	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	svc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           st.RawDB(),
		Audit:        audit,
		PluginsDir:   filepath.Join(tmp, "plugins"),
		LockfilePath: filepath.Join(tmp, "tanpopo.lock"),
		Keyring:      installer.TrustedKeyring{RootPublicKeys: map[string]ed25519.PublicKey{"root": rootPub}},
		NowMs:        func() int64 { return fc.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewLifecycleService: %v", err)
	}

	opID := idgen.MustNew(idgen.PrefixPrincipal)
	nowMs := fc.Now().UnixMilli()
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES (?, NULL, 'HUMAN_LOCAL', 'op@fg.local', 'ACTIVE', 1, ?, ?)`, opID, nowMs, nowMs); err != nil {
		t.Fatalf("insert operator principal: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES (?, NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`, "rb_fg_op", opID, nowMs); err != nil {
		t.Fatalf("insert operator role: %v", err)
	}

	trapBytes := wasmfixtures.UnreachableTrapWASM()
	trapSum := sha256.Sum256(trapBytes)
	trapManifest := fmt.Sprintf(`apiVersion: "tanpopo.dev/extension/v1alpha1"
kind: "ExtensionManifest"
metadata:
  id: "@tanpopo/faulty-adapter"
  name: "Faulty Wasm Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
  sha256: %q
  quotas:
    maxMemoryMib: 16
    maxCpuMsPerCall: 50
    maxStreamChunkCpuMs: 50
capabilities:
  - type: "provider_adapter"
    adapterSlug: "faulty"
    supportedDialects: ["openai_compatible"]
    authMethods:
      - id: "none"
        mode: "none"
permissions:
  networkEgress:
    allowedHosts:
      - "api.acme.example"
    allowPrivateIp: false
    requireTls: true
  hooks:
    - "provider:transform_request"
`, hex.EncodeToString(trapSum[:]))

	// Pack tar.gz using installer helper style
	tpkgBytes, tpkgSHA := buildTestTPKG(t, map[string][]byte{
		"manifest.yaml":     []byte(trapManifest),
		"dist/adapter.wasm": trapBytes,
	})
	gitSHA := "0123456789abcdef0123456789abcdef01234567"
	sig := ed25519.Sign(rootPriv, installer.SignatureSigningPayload("@tanpopo/faulty-adapter", "1.0.0", tpkgSHA))
	staged, err := svc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(tpkgBytes),
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindGit,
			URI:            "https://github.com/acme/faulty-adapter.git",
			Ref:            gitSHA,
			ExpectedSHA256: tpkgSHA,
		},
		Signature: installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:   opID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall: %v", err)
	}
	if _, err := svc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: opID,
		ActivateImmediately: true,
	}); err != nil {
		t.Fatalf("ApproveAndActivate: %v", err)
	}

	var alerts atomic.Int64
	fg := wasm.NewFaultGuard(wasm.FaultGuardConfig{
		Now:        fc.Now,
		Quarantine: svc.QuarantineInstallation,
		AuditSink:  audit,
		OnError: func(_ context.Context, err error) {
			t.Errorf("unexpected FaultGuard error: %v", err)
		},
		OnAlert: func(_ context.Context, ev wasm.AlertEvent) {
			if ev.Code == wasm.AlertCodePluginTrap && ev.InstallationID == staged.Installation.InstallationID {
				alerts.Add(1)
			}
		},
	})

	faultyRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      trapBytes,
		InstallationID: staged.Installation.InstallationID,
		PoolSize:       2,
		CallTimeout:    100 * time.Millisecond,
		FaultGuard:     fg,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime faulty: %v", err)
	}
	defer faultyRT.Close(ctx)

	siblingRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		InstallationID: "ins_healthy_sibling",
		PoolSize:       2,
		CallTimeout:    100 * time.Millisecond,
		FaultGuard:     fg,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime sibling: %v", err)
	}
	defer siblingRT.Close(ctx)

	// 20 faults within 60s must NOT quarantine yet (threshold is > 20).
	for i := 0; i < 20; i++ {
		_, _ = faultyRT.TransformRawSlice(ctx, fmt.Sprintf("t_%d", i), []byte("x"))
		fc.Advance(2 * time.Second) // 20 * 2s = 40s elapsed (< 60s)
	}
	if fg.IsQuarantined(staged.Installation.InstallationID) {
		t.Fatalf("expected installation NOT to be quarantined at fault #20")
	}

	// Fault #21 within 60s crosses > 20 -> triggers QUARANTINED + ALT-PLUGIN-TRAP.
	_, _ = faultyRT.TransformRawSlice(ctx, "t_20", []byte("x"))
	if !fg.IsQuarantined(staged.Installation.InstallationID) {
		t.Fatalf("expected FaultGuard quarantine at fault #21 within 60s")
	}
	if alerts.Load() != 1 || audit.countType(wasm.AlertCodePluginTrap) != 1 {
		t.Fatalf("expected 1 ALT-PLUGIN-TRAP alert + audit event, got alerts=%d audit=%d", alerts.Load(), audit.countType(wasm.AlertCodePluginTrap))
	}

	// Subsequent calls fail closed immediately as quarantined, while DB SM-01 row == QUARANTINED.
	_, postErr := faultyRT.TransformRawSlice(ctx, "t_after", []byte("x"))
	var qfe *wasm.ExecutionFaultError
	if !errors.As(postErr, &qfe) || qfe.Kind != wasm.FaultKindQuarantined {
		t.Fatalf("expected FaultKindQuarantined after quarantine, got %v", postErr)
	}
	inst, err := svc.GetInstallation(ctx, staged.Installation.InstallationID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if inst.State != installer.StateQuarantined {
		t.Fatalf("expected DB SM-01 state QUARANTINED, got %s", inst.State)
	}

	// Healthy sibling continues serving 100% uninterrupted.
	if out, err := siblingRT.TransformRawSlice(ctx, "sib_ok", []byte("sibling-ok")); err != nil || string(out) != "sibling-ok" {
		t.Fatalf("expected healthy sibling unaffected, out=%q err=%v", out, err)
	}

	// Sliding window expiry check: 20 faults, advance > 60s, next fault stays at 1 (no quarantine).
	fc2 := clock.NewFakeClock(clock.DefaultFakeStart())
	fgWin := wasm.NewFaultGuard(wasm.FaultGuardConfig{Now: fc2.Now})
	for i := 0; i < 20; i++ {
		fgWin.RecordFault(ctx, "ins_win", wasm.FaultKindTrap, "trap")
	}
	fc2.Advance(61 * time.Second)
	if triggered := fgWin.RecordFault(ctx, "ins_win", wasm.FaultKindTrap, "trap"); triggered || fgWin.WindowCount("ins_win") != 1 {
		t.Fatalf("expected sliding window expiry after 61s, triggered=%v count=%d", triggered, fgWin.WindowCount("ins_win"))
	}
}

func TestWasmNEG2UnapprovedWASIImportRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name string
		wasm []byte
	}{
		{"fixture_wasi_path_open", wasmfixtures.UnapprovedWASIImportWASM()},
		{"wasi_environ_get", buildImportRejectWASM("wasi_snapshot_preview1", "environ_get")},
		{"wasi_sock_open", buildImportRejectWASM("wasi_snapshot_preview1", "sock_open")},
		{"arbitrary_host_module", buildImportRejectWASM("env", "host_exec")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: tc.wasm})
			if !errors.Is(err, wasm.ErrUnapprovedWASIImport) || !strings.Contains(err.Error(), "unapproved_wasi_import") {
				t.Fatalf("expected unapproved_wasi_import rejection for %s, got %v", tc.name, err)
			}
		})
	}
}

func TestWasmNEG3ZeroCrossRequestMemoryBleed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      buildScratchWriterWASM(),
		PoolSize:       1, // Force Request B onto exact same physical pool slot as Request A
		CallTimeout:    200 * time.Millisecond,
		AllowedHosts:   []string{"api.acme.example"},
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	// 1. Org 1 writes sensitive payload outside returned output slice and succeeds.
	org1Secret := "ORG1_TENANT_PRIVATE_DATA_1234567"
	if _, err := rt.TransformRawSlice(ctx, "org1_ok", []byte(org1Secret)); err != nil {
		t.Fatalf("org1 call failed: %v", err)
	}
	// Org 2 probes scratch offset 90000 with '?': must read all-zero bytes, 0 Tenant 1 bytes.
	probeA, err := rt.TransformRawSlice(ctx, "org2_probe_1", []byte("?probe"))
	if err != nil {
		t.Fatalf("org2 probe 1 failed: %v", err)
	}
	if bytes.Contains(probeA, []byte("ORG1")) || !bytes.Equal(probeA, make([]byte, 32)) {
		t.Fatalf("cross-request memory bleed after clean return: %q (%x)", probeA, probeA)
	}

	// 2. Org 1 writes sensitive payload to scratch offset 90000 AND traps mid-call ('!').
	if _, err := rt.TransformRawSlice(ctx, "org1_trap", []byte("!ORG1_TRAPPED_TENANT_SECRET_9999")); err == nil {
		t.Fatalf("expected trap on '!'")
	}
	probeB, err := rt.TransformRawSlice(ctx, "org2_probe_2", []byte("?probe"))
	if err != nil {
		t.Fatalf("org2 probe 2 failed: %v", err)
	}
	if bytes.Contains(probeB, []byte("ORG1")) || !bytes.Equal(probeB, make([]byte, 32)) {
		t.Fatalf("cross-request memory bleed after trap: %q (%x)", probeB, probeB)
	}
}

func TestWasmCT16BlindBrokerZeroCanarySecretInMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fc := clock.NewFakeClock(clock.DefaultFakeStart())

	// Spin up local FakeLLM requiring exact Bearer canary secret and injecting Set-Cookie header.
	var force401 atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "upstream_sid", Value: "must_strip"})
		auth := r.Header.Get("Authorization")
		if force401.Load() || auth != "Bearer "+canary.DefaultCanarySecret() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"error":{"message":"bad auth %s"}}`, auth)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"provider":"acme"}`))
	}))
	defer srv.Close()

	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "ct16.db"), NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()

	kekBytes := make([]byte, vault.KeySizeBytes)
	_, _ = rand.Read(kekBytes)
	kek, err := vault.NewStaticKEKProvider("kek_wasm_ct16", kekBytes)
	clear(kekBytes)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()

	vs, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         func() int64 { return fc.Now().UnixMilli() },
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vs.Close()

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "wasm-ct16", DisplayName: "Wasm CT16"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	connID := idgen.MustNew(idgen.PrefixConnection)
	aud := srv.URL
	meta, err := vs.SealSecret(ctx, vault.SealSecretParams{
		OrgID:         org.ID,
		Name:          "acme_key",
		SecretKind:    vault.SecretKindAPIKey,
		BoundAudience: &aud,
		Plaintext:     []byte(canary.DefaultCanarySecret()),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}

	addrHost := strings.TrimPrefix(srv.URL, "http://")
	resolver := &staticSlotResolver{slots: map[string]*broker.ResolvedSlot{}}
	resolver.put(&broker.ResolvedSlot{
		OrgID:          org.ID,
		ConnectionID:   connID,
		SlotName:       "api_key",
		SecretID:       meta.ID,
		BoundAudience:  &aud,
		AllowedHosts:   []string{addrHost},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
	})
	audit := &memAuditSink{}
	br, err := broker.NewBlindBroker(broker.Config{
		Resolver:       resolver,
		PolicyResolver: resolver,
		Vault:          vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          audit,
		NowMs:          func() int64 { return fc.Now().UnixMilli() },
		ControlHook:    netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker: %v", err)
	}

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:            buildHostEgressBridgeWASM(),
		PoolSize:             2,
		CallTimeout:          time.Second,
		AllowHostBlindEgress: true,
		BlindBroker:          br,
		AllowedHosts:         []string{addrHost},
		AllowPrivateIP:       true,
		AllowedCIDRs:         []string{"127.0.0.1/32"},
		AllowInsecureHTTP:    true,
		ApprovedSecretSlots:  []string{"api_key"},
		ABIMode:              wasm.ABIModeSliceTransform,
		SliceTransform:       &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: srv.URL + "/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime egress bridge: %v", err)
	}
	defer rt.Close(ctx)

	scopedCtx := wasm.WithEgressScope(ctx, wasm.EgressScope{
		OrgID:        org.ID,
		ConnectionID: connID,
		ActorKind:    "VIRTUAL_KEY",
		ActorID:      "vky_test",
		EntryPoint:   "data_plane_guard",
		RequestID:    "req_ct16",
	})

	// 1. Authorized blind egress succeeds; Set-Cookie stripped; 0 canary secret bytes in response or Wasm memory.
	reqJSON, _ := json.Marshal(map[string]any{
		"method":     "POST",
		"target_url": srv.URL + "/v1/chat/completions",
		"body_bytes": []byte(`{"prompt":"hi"}`),
		"credential_injections": []port.CredentialInjectInstruction{
			{
				SecretSlotName: "api_key",
				TargetLocation: port.CredentialTargetLocationHeader,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
	})
	out, err := rt.TransformRawSlice(scopedCtx, "req_ct16", reqJSON)
	if err != nil {
		t.Fatalf("authorized host egress failed: %v", err)
	}
	scanner := canary.NewScanner()
	if len(scanner.ScanString("egress-ok", string(out))) > 0 || strings.Contains(strings.ToLower(string(out)), "set-cookie") {
		t.Fatalf("secret or Set-Cookie leaked to Wasm guest response: %s", out)
	}
	if !strings.Contains(string(out), `"status_code":200`) {
		t.Fatalf("unexpected guest egress response: %s", out)
	}

	// 2. Echoed 401 secret from upstream is scrubbed before crossing into Wasm guest.
	force401.Store(true)
	out401, err := rt.TransformRawSlice(scopedCtx, "req_ct16_401", reqJSON)
	if err != nil {
		t.Fatalf("expected 401 envelope delivered cleanly scrubbed to guest, got %v", err)
	}
	if len(scanner.ScanString("egress-401", string(out401))) > 0 {
		t.Fatalf("401 echoed canary secret reached guest: %s", out401)
	}
	if err := rt.InspectLinearMemory(func(mem []byte) bool {
		return bytes.Contains(mem, []byte(canary.DefaultCanarySecret()))
	}); err != nil {
		t.Fatalf("canary secret detected inside Wasm linear memory: %v", err)
	}
	force401.Store(false)

	// 3. Cookie injection & unapproved host attempts from Wasm fail closed (-2 -> guest trap).
	cookieAttempt, _ := json.Marshal(map[string]any{
		"method":     "POST",
		"target_url": srv.URL + "/v1/chat/completions",
		"credential_injections": []port.CredentialInjectInstruction{
			{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Cookie"},
		},
	})
	if _, err := rt.TransformRawSlice(scopedCtx, "req_cookie", cookieAttempt); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected fail-closed Cookie rejection, got %v", err)
	}
}

func TestWasmCT17HundredConcurrentHealthyWithAdversarialFaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	healthyRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		PoolSize:       16,
		CallTimeout:    2 * time.Second,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime healthy: %v", err)
	}
	defer healthyRT.Close(ctx)

	loopRT, _ := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.InfiniteLoopWASM(),
		PoolSize:         4,
		CallTimeout:      25 * time.Millisecond,
		WatchdogInterval: 5 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	defer loopRT.Close(ctx)

	oomRT, _ := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.OOM64MiBWASM(),
		PoolSize:       4,
		CallTimeout:    100 * time.Millisecond,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	defer oomRT.Close(ctx)

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := fmt.Appendf(nil, "healthy-ct17-stream-%d", i)
			out, hErr := healthyRT.TransformRawSlice(ctx, fmt.Sprintf("h_%d", i), payload)
			if hErr != nil || !bytes.Equal(out, payload) {
				errCh <- fmt.Errorf("healthy stream %d failed: %v", i, hErr)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			_, _ = loopRT.TransformRawSlice(ctx, fmt.Sprintf("loop_%d", idx), []byte("x"))
		}(i)
		go func(idx int) {
			defer wg.Done()
			_, _ = oomRT.TransformRawSlice(ctx, fmt.Sprintf("oom_%d", idx), []byte("x"))
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}
}

func TestWasmStrictExtensionRuntimeAllTenRPCsAndKillSwitch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	probeResp, _ := json.Marshal(port.UpstreamHttpSpec{
		Method:    http.MethodGet,
		TargetURL: "https://api.acme.example/v1/models",
		CredentialInjections: []port.CredentialInjectInstruction{
			{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Authorization", ValuePrefix: "Bearer "},
		},
	})
	verdictResp, _ := json.Marshal(port.ValidationVerdict{Valid: true, DetectedModelIDs: []string{"acme-1"}})
	reqResp, _ := json.Marshal(port.UpstreamHttpSpec{
		Method:    http.MethodPost,
		TargetURL: "https://api.acme.example/v1/chat/completions",
		BodyBytes: []byte(`{"model":"acme-1","messages":[{"role":"user","content":"blob_ref://r1/0"}]}`),
		OutboundBlobSplices: []port.BlobRef{
			{BlobID: "blob_1", URI: "blob_ref://r1/0", MimeType: "image/png", ByteLength: 102400, SHA256Hex: strings.Repeat("a", 64)},
		},
		CredentialInjections: []port.CredentialInjectInstruction{
			{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Authorization", ValuePrefix: "Bearer "},
		},
	})
	normResp, _ := json.Marshal(port.NormalizedInferenceResponse{
		NormalizedResponseJSON: []byte(`{"id":"cmpl_1"}`),
		Usage:                  &port.NormalizedUsage{InputTokens: 10, OutputTokens: 20, Accuracy: port.UsageAccuracyExact},
		UpstreamModelID:        "acme-1",
		FinishReason:           "stop",
	})
	chunkResp, _ := json.Marshal(port.StreamChunkOutput{
		NextSessionState:    []byte("st_1"),
		NormalizedSSEFrames: [][]byte{[]byte("data: {\"delta\":\"ok\"}\n\n")},
		StreamCompleted:     true,
	})
	errResp, _ := json.Marshal(port.CanonicalProviderError{
		CanonicalCode:      "upstream_rate_limited",
		ErrorType:          "rate_limit_error",
		RetryablePreStream: true,
		CooldownMs:         1000,
	})
	usageResp, _ := json.Marshal(port.NormalizedUsage{InputTokens: 5, OutputTokens: 7, Accuracy: port.UsageAccuracyExact})
	nativeResp, _ := json.Marshal(port.NativeRoutingHints{
		TargetModel: "acme-1",
		Stream:      true,
		CredentialInjections: []port.CredentialInjectInstruction{
			{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Authorization", ValuePrefix: "Bearer "},
		},
	})
	guardResp, _ := json.Marshal(port.GuardrailVerdict{Allowed: true})

	wasmBytes := buildMultiMethodCanonicalWASM(map[string][]byte{
		"build_validation_probe":   probeResp,
		"verify_validation_result": verdictResp,
		"transform_request":        reqResp,
		"transform_response":       normResp,
		"transform_stream_chunk":   chunkResp,
		"normalize_error":          errResp,
		"extract_usage":            usageResp,
		"inspect_native_request":   nativeResp,
		"evaluate_guardrail":       guardResp,
	})

	var enabled atomic.Bool
	enabled.Store(true)

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:           wasmBytes,
		PoolSize:            2,
		CallTimeout:         2 * time.Second,
		StreamChunkTimeout:  500 * time.Millisecond,
		AllowedHosts:        []string{"api.acme.example"},
		ApprovedSecretSlots: []string{"api_key"},
		EnabledFunc:         enabled.Load,
	})
	if err != nil {
		t.Fatalf("NewRuntime 10-RPC: %v", err)
	}
	defer rt.Close(ctx)

	if pSpec, err := rt.BuildValidationProbe(ctx, port.ValidationProbeInput{RequestID: "r1"}); err != nil || pSpec.Method != "GET" {
		t.Fatalf("BuildValidationProbe failed: spec=%+v err=%v", pSpec, err)
	}
	if v, err := rt.VerifyValidationResult(ctx, port.ValidationResultInput{RequestID: "r1", HTTPStatus: 200}); err != nil || !v.Valid {
		t.Fatalf("VerifyValidationResult failed: %+v err=%v", v, err)
	}
	if tr, err := rt.TransformRequest(ctx, port.TransformRequestInput{RequestID: "r1"}); err != nil || len(tr.OutboundBlobSplices) != 1 {
		t.Fatalf("TransformRequest failed: %+v err=%v", tr, err)
	}
	if res, err := rt.TransformResponse(ctx, port.TransformResponseInput{RequestID: "r1", HTTPStatus: 200}); err != nil || res.FinishReason != "stop" {
		t.Fatalf("TransformResponse failed: %+v err=%v", res, err)
	}
	if ch, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "r1", IsEOF: true}); err != nil || !ch.StreamCompleted {
		t.Fatalf("TransformStreamChunk failed: %+v err=%v", ch, err)
	}
	if ce, err := rt.NormalizeError(ctx, port.NormalizeErrorInput{RequestID: "r1", HTTPStatus: 429}); err != nil || !ce.RetryablePreStream {
		t.Fatalf("NormalizeError failed: %+v err=%v", ce, err)
	}
	if u, err := rt.ExtractUsage(ctx, port.ExtractUsageInput{RequestID: "r1"}); err != nil || u.OutputTokens != 7 {
		t.Fatalf("ExtractUsage failed: %+v err=%v", u, err)
	}
	if nh, err := rt.InspectNativeRequest(ctx, port.NativeRequestInput{RequestID: "r1", Dialect: "openai"}); err != nil || nh.TargetModel != "acme-1" {
		t.Fatalf("InspectNativeRequest failed: %+v err=%v", nh, err)
	}
	if gv, err := rt.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "r1", Stage: port.GuardrailStagePreRequest}); err != nil || !gv.Allowed {
		t.Fatalf("EvaluateGuardrail failed: %+v err=%v", gv, err)
	}
	if ack, err := rt.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: "r_none"}); err != nil || ack.Released {
		t.Fatalf("CancelExecution on idle request: %+v err=%v", ack, err)
	}

	// Kill switch rollback (`runtime.wasm.enabled = false`) disables Wasm cleanly.
	enabled.Store(false)
	if _, err := rt.TransformRequest(ctx, port.TransformRequestInput{RequestID: "r_off"}); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) || !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected ErrWasmRuntimeDisabled on kill-switch off, got %v", err)
	}

	// Negative guest output enforcement: DEC-004 Cookie injection, unapproved host, unapproved slot, >64KiB session_state.
	cookieBad, _ := json.Marshal(port.UpstreamHttpSpec{
		Method:    http.MethodPost,
		TargetURL: "https://api.acme.example/v1/chat/completions",
		CredentialInjections: []port.CredentialInjectInstruction{
			{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Cookie"},
		},
	})
	badCookieRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:           buildMultiMethodCanonicalWASM(map[string][]byte{"transform_request": cookieBad}),
		PoolSize:            1,
		AllowedHosts:        []string{"api.acme.example"},
		ApprovedSecretSlots: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("NewRuntime badCookie: %v", err)
	}
	defer badCookieRT.Close(ctx)
	if _, err := badCookieRT.TransformRequest(ctx, port.TransformRequestInput{RequestID: "r_ck"}); !errors.Is(err, port.ErrProhibitedCookieInjection) {
		t.Fatalf("expected DEC-004 ErrProhibitedCookieInjection, got %v", err)
	}

	oversizeState, _ := json.Marshal(port.StreamChunkOutput{
		NextSessionState: make([]byte, wasm.MaxStreamSessionStateBytes+1),
	})
	badStateRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMultiMethodCanonicalWASM(map[string][]byte{"transform_stream_chunk": oversizeState}),
		PoolSize:  1,
	})
	if err != nil {
		t.Fatalf("NewRuntime badState: %v", err)
	}
	defer badStateRT.Close(ctx)
	if _, err := badStateRT.TransformStreamChunk(ctx, port.StreamChunkInput{RequestID: "r_sz"}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected ErrExecutionFault for > 64KiB session_state, got %v", err)
	}
}

// --- FZ-05 Pointer/Length Overflow & Malformed ABI Boundary Fuzzing ---

func FuzzWasmABIBoundary(f *testing.F) {
	f.Add(uint64(65536<<32|16), uint32(131072), uint32(1048576))
	f.Add(uint64(0xffffffffffffffff), uint32(131072), uint32(1048576))
	f.Add(uint64(uint64(0xffffff00)<<32|512), uint32(131072), uint32(1048576))
	f.Add(uint64(uint64(131000)<<32|100), uint32(131072), uint32(1048576))
	f.Add(uint64(0x80000000<<32|4), uint32(131072), uint32(1048576))

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      buildPackedReturnReaderWASM(),
		PoolSize:       2,
		CallTimeout:    100 * time.Millisecond,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		f.Fatalf("NewRuntime fuzz guest: %v", err)
	}
	f.Cleanup(func() { _ = rt.Close(ctx) })

	f.Fuzz(func(t *testing.T, packed uint64, memSize uint32, maxOut uint32) {
		if memSize > wasm.AbsoluteMaxMemoryLimitPages*wasm.WasmPageBytes {
			memSize = wasm.AbsoluteMaxMemoryLimitPages * wasm.WasmPageBytes
		}
		ptr, length, decErr := wasm.DecodePackedSlice(packed, memSize, maxOut)
		if decErr == nil {
			if int32(ptr) < 0 || int32(length) < 0 || uint64(ptr)+uint64(length) > uint64(memSize) {
				t.Fatalf("DecodePackedSlice accepted out-of-bounds [%d, %d) > %d", ptr, length, memSize)
			}
			if maxOut > 0 && length > maxOut {
				t.Fatalf("DecodePackedSlice exceeded maxOut %d > %d", length, maxOut)
			}
		} else if !errors.Is(decErr, port.ErrExecutionFault) {
			t.Fatalf("expected ErrExecutionFault on ABI failure, got %v", decErr)
		}

		// Also invoke real wazero guest with the fuzzed packed u64 return value.
		var in [8]byte
		binary.LittleEndian.PutUint64(in[:], packed)
		_, gErr := rt.TransformRawSlice(ctx, "fz05", in[:])
		if gErr != nil && !errors.Is(gErr, port.ErrExecutionFault) {
			t.Fatalf("expected ErrExecutionFault or valid slice, got %v", gErr)
		}
	})
}

// --- Microbenchmarks (Isolated Pool Overhead Only — COND-PO-01) ---

func BenchmarkWasmTransformRequest(b *testing.B) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		PoolSize:         16,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages,
		CallTimeout:      200 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat/completions"},
	})
	if err != nil {
		b.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	in := port.TransformRequestInput{
		RequestID:             "bench_req",
		NormalizedRequestJSON: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.TransformRequest(ctx, in); err != nil {
			b.Fatalf("TransformRequest: %v", err)
		}
	}
}

func BenchmarkWasmTransformStreamChunk(b *testing.B) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		PoolSize:         16,
		MemoryLimitPages: wasm.DefaultMemoryLimitPages,
		CallTimeout:      200 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat/completions"},
	})
	if err != nil {
		b.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	in := port.StreamChunkInput{
		RequestID:        "bench_chunk",
		RawUpstreamChunk: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"tok\"}}]}\n\n"),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.TransformStreamChunk(ctx, in); err != nil {
			b.Fatalf("TransformStreamChunk: %v", err)
		}
	}
}

// --- Targeted Branch, Security & Pool Recovery Coverage Tests ---

func buildMinimalCustomWASM(typeSec, importSec, funcSec, memSec, exportSec, startSec, codeSec, dataSec []byte) []byte {
	var out []byte
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	if len(typeSec) > 0 {
		out = append(out, wasmSection(1, typeSec)...)
	}
	if len(importSec) > 0 {
		out = append(out, wasmSection(2, importSec)...)
	}
	if len(funcSec) > 0 {
		out = append(out, wasmSection(3, funcSec)...)
	}
	if len(memSec) > 0 {
		out = append(out, wasmSection(5, memSec)...)
	}
	if len(exportSec) > 0 {
		out = append(out, wasmSection(7, exportSec)...)
	}
	if len(startSec) > 0 {
		out = append(out, wasmSection(8, startSec)...)
	}
	if len(codeSec) > 0 {
		out = append(out, wasmSection(10, codeSec)...)
	}
	if len(dataSec) > 0 {
		out = append(out, wasmSection(11, dataSec)...)
	}
	return out
}

func TestWasmConfigAndModuleValidationBranches(t *testing.T) {
	ctx := context.Background()

	// ExecutionFaultError helpers (including nil receiver and nil Cause)
	var nilFault *wasm.ExecutionFaultError
	if nilFault.Error() != "" || nilFault.Unwrap() != nil {
		t.Fatalf("unexpected nil ExecutionFaultError methods")
	}
	plainFault := &wasm.ExecutionFaultError{Kind: wasm.FaultKindTrap, Reason: "trapped"}
	if !strings.Contains(plainFault.Error(), "trapped") ||
		plainFault.HTTPStatus() != http.StatusBadGateway ||
		plainFault.CanonicalCode() != "extension_execution_fault" ||
		plainFault.ErrorType() != "extension_error" ||
		!errors.Is(plainFault, port.ErrExecutionFault) {
		t.Fatalf("unexpected ExecutionFaultError metadata: %s %s %d", plainFault.Error(), plainFault.ErrorType(), plainFault.HTTPStatus())
	}

	// Nil Runtime safety
	var nilRT *wasm.Runtime
	if err := nilRT.Close(ctx); err != nil {
		t.Fatalf("nil Runtime.Close: %v", err)
	}
	if st := nilRT.Stats(); st.Capacity != 0 {
		t.Fatalf("nil Stats: %+v", st)
	}
	if err := nilRT.InspectLinearMemory(func([]byte) bool { return true }); err != nil {
		t.Fatalf("nil InspectLinearMemory: %v", err)
	}

	// Global kill-switch toggle (EnabledFunc == nil path)
	wasm.SetWasmEnabled(false)
	defer wasm.SetWasmEnabled(true)
	if wasm.IsWasmEnabled() {
		t.Fatalf("expected IsWasmEnabled=false")
	}
	rtDisabled, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		PoolSize:         1,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
		CompilationCache: wazero.NewCompilationCache(),
	})
	if err != nil {
		t.Fatalf("NewRuntime with CompilationCache: %v", err)
	}
	defer rtDisabled.Close(ctx)
	if _, err := rtDisabled.TransformRawSlice(ctx, "disabled_req", []byte("hi")); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) {
		t.Fatalf("expected ErrWasmRuntimeDisabled when global toggle off, got %v", err)
	}
	wasm.SetWasmEnabled(true)

	// Invalid RuntimeConfig cases
	badConfigs := []struct {
		name string
		cfg  wasm.RuntimeConfig
		want error
	}{
		{"empty_wasm", wasm.RuntimeConfig{}, wasm.ErrInvalidRuntimeConfig},
		{"pool_too_large", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), PoolSize: wasm.MaxPoolSize + 1}, wasm.ErrInvalidRuntimeConfig},
		{"pool_negative", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), PoolSize: -1}, wasm.ErrInvalidRuntimeConfig},
		{"mem_exceeds_extended_cap", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), MemoryLimitPages: wasm.AbsoluteMaxMemoryLimitPages + 1, AllowExtendedMemory: true}, wasm.ErrInvalidRuntimeConfig},
		{"slice_missing_cfg", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), ABIMode: wasm.ABIModeSliceTransform}, wasm.ErrInvalidRuntimeConfig},
		{"slice_invalid_spec", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), ABIMode: wasm.ABIModeSliceTransform, SliceTransform: &wasm.SliceTransformConfig{Method: "POST", TargetURL: "ftp://bad"}}, wasm.ErrInvalidRuntimeConfig},
		{"unsupported_abi", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), ABIMode: wasm.ABIMode("unknown")}, wasm.ErrInvalidRuntimeConfig},
		{"bad_allowed_host", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedHosts: []string{"*.acme.example"}}, wasm.ErrInvalidRuntimeConfig},
		{"blind_egress_empty_hosts", wasm.RuntimeConfig{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowHostBlindEgress: true, BlindBroker: &broker.BlindHostCredentialBroker{}}, wasm.ErrInvalidRuntimeConfig},
		{"corrupt_wasm_bytes", wasm.RuntimeConfig{WasmBytes: []byte("not_wasm")}, wasm.ErrInvalidRuntimeConfig},
	}
	for _, tc := range badConfigs {
		if _, err := wasm.NewRuntime(nil, tc.cfg); !errors.Is(err, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}

	// Extended memory within 1024-page ceiling succeeds
	extRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:           wasmfixtures.EchoTransformWASM(),
		PoolSize:            1,
		MemoryLimitPages:    512,
		AllowExtendedMemory: true,
		ABIMode:             wasm.ABIModeSliceTransform,
		AllowedHosts:        []string{"api.acme.example"},
		SliceTransform:      &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("extended memory 512 pages: %v", err)
	}
	_ = extRT.Close(ctx)
	_ = extRT.Close(ctx) // Double Close idempotent

	// Compiled module rejection cases:
	// a) Imported memory
	var impMem []byte
	impMem = append(impMem, 0x01)
	impMem = append(impMem, wasmString("env")...)
	impMem = append(impMem, wasmString("memory")...)
	impMem = append(impMem, 0x02, 0x00, 0x01) // mem min 1
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMinimalCustomWASM(nil, impMem, nil, nil, nil, nil, nil, nil),
	}); !errors.Is(err, wasm.ErrUnapprovedWASIImport) {
		t.Fatalf("imported memory: want ErrUnapprovedWASIImport, got %v", err)
	}

	// b) Missing exported memory
	var expOnlyFn []byte
	expOnlyFn = append(expOnlyFn, 0x01)
	expOnlyFn = append(expOnlyFn, wasmString("transform")...)
	expOnlyFn = append(expOnlyFn, 0x00, 0x00)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e},
			nil,
			[]byte{0x01, 0x00},
			nil,
			expOnlyFn,
			nil,
			[]byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b},
			nil,
		),
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("missing exported memory: want ErrInvalidRuntimeConfig, got %v", err)
	}

	// c) Min declared pages (4) exceeds configured MemoryLimitPages (2)
	var memExp []byte
	memExp = append(memExp, 0x02)
	memExp = append(memExp, wasmString("memory")...)
	memExp = append(memExp, 0x02, 0x00)
	memExp = append(memExp, wasmString("transform")...)
	memExp = append(memExp, 0x00, 0x00)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		MemoryLimitPages: 2,
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e},
			nil,
			[]byte{0x01, 0x00},
			[]byte{0x01, 0x01, 0x04, 0x10},
			memExp,
			nil,
			[]byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b},
			nil,
		),
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("min pages > limit: want ErrInvalidRuntimeConfig, got %v", err)
	}

	// d) ABIModeSliceTransform missing exported "transform" (exports only "other")
	var otherExp []byte
	otherExp = append(otherExp, 0x02)
	otherExp = append(otherExp, wasmString("memory")...)
	otherExp = append(otherExp, 0x02, 0x00)
	otherExp = append(otherExp, wasmString("other")...)
	otherExp = append(otherExp, 0x00, 0x00)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e},
			nil,
			[]byte{0x01, 0x00},
			[]byte{0x01, 0x01, 0x02, 0x10},
			otherExp,
			nil,
			[]byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b},
			nil,
		),
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("missing transform export: want ErrInvalidRuntimeConfig, got %v", err)
	}

	// e) ABIModeCanonicalEnvelope with only void() export fails validation
	var voidExp []byte
	voidExp = append(voidExp, 0x02)
	voidExp = append(voidExp, wasmString("memory")...)
	voidExp = append(voidExp, 0x02, 0x00)
	voidExp = append(voidExp, wasmString("tanpopo_alloc")...)
	voidExp = append(voidExp, 0x00, 0x00)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		ABIMode: wasm.ABIModeCanonicalEnvelope,
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x01, 0x60, 0x00, 0x00},
			nil,
			[]byte{0x01, 0x00},
			[]byte{0x01, 0x01, 0x02, 0x10},
			voidExp,
			nil,
			[]byte{0x01, 0x02, 0x00, 0x0b},
			nil,
		),
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("no slice abi export: want ErrInvalidRuntimeConfig, got %v", err)
	}

	// f) Guest (start) function trapping with unreachable -> instantiateSlot ExecutionFaultError
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x02, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, 0x60, 0x00, 0x00},
			nil,
			[]byte{0x02, 0x00, 0x01},
			[]byte{0x01, 0x01, 0x02, 0x10},
			memExp,
			[]byte{0x01}, // start func idx 1
			[]byte{0x02, 0x04, 0x00, 0x42, 0x00, 0x0b, 0x03, 0x00, 0x00, 0x0b},
			nil,
		),
	}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("start trap: want ErrExecutionFault, got %v", err)
	}
}

func TestWasmSliceModeAllRPCsAndOutboundValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:          wasmfixtures.EchoTransformWASM(),
		PoolSize:           2,
		CallTimeout:        2 * time.Second,
		StreamChunkTimeout: 500 * time.Millisecond,
		ABIMode:            wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{
			Method:      http.MethodPost,
			TargetURL:   "https://[::1]:8443/v1/chat",
			SafeHeaders: map[string]string{"X-Provider": "acme"},
			CredentialInjections: []port.CredentialInjectInstruction{
				{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Authorization"},
			},
		},
		AllowedHosts:        []string{"[::1]:8443", "api.acme.example", "198.51.100.1"},
		ApprovedSecretSlots: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	if spec, err := rt.BuildValidationProbe(ctx, port.ValidationProbeInput{RequestID: "s1"}); err != nil || spec.SafeHeaders["X-Provider"] != "acme" {
		t.Fatalf("BuildValidationProbe slice: spec=%+v err=%v", spec, err)
	}
	if v, err := rt.VerifyValidationResult(ctx, port.ValidationResultInput{RequestID: "s2", HTTPStatus: 204}); err != nil || !v.Valid {
		t.Fatalf("VerifyValidationResult slice: %+v err=%v", v, err)
	}
	if norm, err := rt.TransformResponse(ctx, port.TransformResponseInput{RequestID: "s3", RawResponseBody: []byte(`{"ok":1}`)}); err != nil || string(norm.NormalizedResponseJSON) != `{"ok":1}` {
		t.Fatalf("TransformResponse slice: %+v err=%v", norm, err)
	}
	if e400, err := rt.NormalizeError(ctx, port.NormalizeErrorInput{RequestID: "s4", HTTPStatus: 400, SanitizedBody: []byte("bad")}); err != nil || e400.RetryablePreStream {
		t.Fatalf("NormalizeError 400 slice: %+v err=%v", e400, err)
	}
	if e502, err := rt.NormalizeError(ctx, port.NormalizeErrorInput{RequestID: "s5", HTTPStatus: 502, SanitizedBody: []byte("bad")}); err != nil || !e502.RetryablePreStream {
		t.Fatalf("NormalizeError 502 slice: %+v err=%v", e502, err)
	}
	if u, err := rt.ExtractUsage(ctx, port.ExtractUsageInput{RequestID: "s6", RawResponseOrTrailerBytes: []byte("{}")}); err != nil || u.Accuracy != port.UsageAccuracyEstimated {
		t.Fatalf("ExtractUsage slice: %+v err=%v", u, err)
	}
	if nh, err := rt.InspectNativeRequest(ctx, port.NativeRequestInput{RequestID: "s7", RawRequestBody: []byte("{}")}); err != nil || len(nh.CredentialInjections) != 1 {
		t.Fatalf("InspectNativeRequest slice: %+v err=%v", nh, err)
	}
	if gv, err := rt.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "s8", Stage: port.GuardrailStagePreRequest, PayloadJSON: []byte(`{"x":1}`)}); err != nil || !gv.Allowed {
		t.Fatalf("EvaluateGuardrail slice: %+v err=%v", gv, err)
	}
	if _, err := rt.EvaluateGuardrail(ctx, port.GuardrailInput{RequestID: "s9", Stage: port.GuardrailStage("bogus")}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected ErrInvalidSpec for invalid GuardrailStage, got %v", err)
	}
	if _, err := rt.TransformStreamChunk(ctx, port.StreamChunkInput{SessionState: make([]byte, wasm.MaxStreamSessionStateBytes+1)}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected ErrExecutionFault for oversized input SessionState, got %v", err)
	}

	// Strict TLS / host:port / unapproved secret slot rejection in canonical mode
	badTLSJSON, _ := json.Marshal(port.UpstreamHttpSpec{Method: "POST", TargetURL: "http://api.acme.example/v1/chat"})
	badHostJSON, _ := json.Marshal(port.UpstreamHttpSpec{Method: "POST", TargetURL: "https://evil.example/v1/chat"})
	badSlotJSON, _ := json.Marshal(port.UpstreamHttpSpec{
		Method:    "POST",
		TargetURL: "https://api.acme.example:8443/v1/chat",
		CredentialInjections: []port.CredentialInjectInstruction{
			{SecretSlotName: "unapproved_slot", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "X-Key"},
		},
	})
	badPortJSON, _ := json.Marshal(port.UpstreamHttpSpec{Method: "POST", TargetURL: "https://api.acme.example:9443/v1/chat"})

	strictRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMultiMethodCanonicalWASM(map[string][]byte{
			"build_validation_probe": badTLSJSON,
			"transform_request":      badHostJSON,
			"transform_response":     []byte(`{"usage":{"accuracy":"bogus"}}`),
			"extract_usage":          []byte(`{"accuracy":"bogus"}`),
			"transform_stream_chunk": []byte(`{"incremental_usage":{"accuracy":"bogus"}}`),
			"inspect_native_request": []byte(`{"credential_injections":[{"secret_slot_name":"unapproved","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"X-A"}]}`),
		}),
		PoolSize:            1,
		AllowedHosts:        []string{"api.acme.example:8443"},
		ApprovedSecretSlots: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("NewRuntime strictRT: %v", err)
	}
	defer strictRT.Close(ctx)
	if _, err := strictRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected https TLS rejection, got %v", err)
	}
	if _, err := strictRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected unapproved host rejection, got %v", err)
	}
	if _, err := strictRT.TransformResponse(ctx, port.TransformResponseInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected invalid accuracy rejection, got %v", err)
	}
	if _, err := strictRT.ExtractUsage(ctx, port.ExtractUsageInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected extract_usage invalid accuracy rejection, got %v", err)
	}
	if _, err := strictRT.TransformStreamChunk(ctx, port.StreamChunkInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected stream incremental_usage invalid accuracy rejection, got %v", err)
	}
	if _, err := strictRT.InspectNativeRequest(ctx, port.NativeRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) || !strings.Contains(err.Error(), "unapproved secret_slot_name") {
		t.Fatalf("expected unapproved slot rejection, got %v", err)
	}

	strictSlotRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:           buildMultiMethodCanonicalWASM(map[string][]byte{"transform_request": badSlotJSON, "build_validation_probe": badPortJSON}),
		PoolSize:            1,
		AllowedHosts:        []string{"api.acme.example:8443"},
		ApprovedSecretSlots: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("NewRuntime strictSlotRT: %v", err)
	}
	defer strictSlotRT.Close(ctx)
	if _, err := strictSlotRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected strict slot rejection, got %v", err)
	}
	if _, err := strictSlotRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected mismatched port rejection, got %v", err)
	}
}

func TestWasmCanonicalFallbackDataRestoreMemoryGrowAndCloseAbort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Canonical fallback via single "transform" export + malformed JSON + invalid UTF-8 check
	fallbackRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMultiMethodCanonicalWASM(map[string][]byte{"transform": []byte("{malformed")}),
		PoolSize:  1,
	})
	if err != nil {
		t.Fatalf("NewRuntime fallback: %v", err)
	}
	defer fallbackRT.Close(ctx)
	if _, err := fallbackRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected ABI violation on malformed guest JSON, got %v", err)
	}

	utf8BadRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMultiMethodCanonicalWASM(map[string][]byte{"transform": []byte{0xff, 0xfe, 0xfd}}),
		PoolSize:  1,
	})
	if err != nil {
		t.Fatalf("NewRuntime utf8Bad: %v", err)
	}
	defer utf8BadRT.Close(ctx)
	if _, err := utf8BadRT.TransformResponse(ctx, port.TransformResponseInput{}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected invalid UTF-8 ABI violation, got %v", err)
	}

	// 2. Non-zero active data segment restoration & memory.grow tainting
	// Guest exports "transform"(ptr, len)->i64: if input=="!", calls memory.grow(1); otherwise reads 4 bytes at 70000 and then overwrites them with "AAAA".
	dataPayload := []byte("INIT")
	var fnBody []byte
	fnBody = append(fnBody, 0x00)
	// Write 4 bytes "AAAA" at offset 70000 ONLY AFTER reading? Actually read happens in host after return:
	// First copy original dataPayload[0..4] (at 70000) to output offset 80000, THEN overwrite 70000 with input[0..4]!
	// Wait: also check if len>0 && input[0]=='g', grow memory by 1 page.
	growLEB := sleb128Int64(70000)
	outLEB := sleb128Int64(80000)
	fnBody = append(fnBody,
		0x20, 0x01, 0x41, 0x00, 0x4b, // len > 0
		0x04, 0x40,
		0x20, 0x00, 0x2d, 0x00, 0x00, // input[0]
		0x41, 0xe7, 0x00, 0x46, // == 'g' (sleb128 103)
		0x04, 0x40,
		0x41, 0x01, 0x40, 0x00, 0x1a, // memory.grow(1); drop
		0x0b,
		0x0b,
	)
	fnBody = append(fnBody, 0x41)
	fnBody = append(fnBody, outLEB...)
	fnBody = append(fnBody, 0x41)
	fnBody = append(fnBody, growLEB...)
	fnBody = append(fnBody, 0x41, 0x04, 0xfc, 0x0a, 0x00, 0x00) // memory.copy(80000, 70000, 4)
	fnBody = append(fnBody, 0x41)
	fnBody = append(fnBody, growLEB...)
	fnBody = append(fnBody, 0x20, 0x00, 0x41, 0x04, 0xfc, 0x0a, 0x00, 0x00) // memory.copy(70000, ptr, 4)
	packedOut := (uint64(80000) << 32) | 4
	fnBody = append(fnBody, 0x42)
	fnBody = append(fnBody, sleb128Int64(int64(packedOut))...)
	fnBody = append(fnBody, 0x0b)

	var exp []byte
	exp = append(exp, 0x02)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wasmString("transform")...)
	exp = append(exp, 0x00, 0x00)

	var codeSec []byte
	codeSec = append(codeSec, 0x01)
	codeSec = append(codeSec, uleb128(uint32(len(fnBody)))...)
	codeSec = append(codeSec, fnBody...)

	var dataSec []byte
	dataSec = append(dataSec, 0x01, 0x00, 0x41)
	dataSec = append(dataSec, growLEB...)
	dataSec = append(dataSec, 0x0b, 0x04)
	dataSec = append(dataSec, dataPayload...)

	restoreRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e},
			nil,
			[]byte{0x01, 0x00},
			[]byte{0x01, 0x01, 0x02, 0x10},
			exp,
			nil,
			codeSec,
			dataSec,
		),
		PoolSize:       1,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime restoreRT: %v", err)
	}
	defer restoreRT.Close(ctx)

	out1, err := restoreRT.TransformRawSlice(ctx, "r_a", []byte("OVER"))
	if err != nil || string(out1) != "INIT" {
		t.Fatalf("call 1 want INIT, got %q err=%v", out1, err)
	}
	// Call 2 must see restored "INIT", NOT "OVER" written by Call 1; and 'g...' grows memory triggering pool replacement!
	out2, err := restoreRT.TransformRawSlice(ctx, "r_b", []byte("grow"))
	if err != nil || string(out2) != "INIT" {
		t.Fatalf("call 2 leaked overwritten data segment: got %q err=%v", out2, err)
	}
	if st := restoreRT.Stats(); st.ReplacedCount != 1 {
		t.Fatalf("expected memory.grow to taint and replace slot (ReplacedCount=1), got %+v", st)
	}

	// 3. Admission wait ctx cancel & Close while active call running
	busyRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.InfiniteLoopWASM(),
		PoolSize:       1,
		CallTimeout:    2 * time.Second,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime busyRT: %v", err)
	}
	activeDone := make(chan error, 1)
	go func() {
		_, e := busyRT.TransformRawSlice(context.Background(), "busy_1", []byte("loop"))
		activeDone <- e
	}()
	// Wait until the single slot is acquired
	for i := 0; i < 100 && busyRT.Stats().Available > 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, 15*time.Millisecond)
	defer cancelWait()
	if _, err := busyRT.TransformRawSlice(waitCtx, "busy_2", []byte("wait")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected admission DeadlineExceeded while pool full, got %v", err)
	}
	if _, err := busyRT.TransformRawSlice(ctx, "big", make([]byte, wasm.MaxGuestPayloadBytes+1)); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected OOM fault on >4MiB guest input, got %v", err)
	}
	if ack, _ := busyRT.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: ""}); ack.Released {
		t.Fatalf("empty RequestID should return Released=false")
	}
	// Closing while busy_1 is in-flight aborts it cleanly without hanging or leaking goroutines
	if err := busyRT.Close(ctx); err != nil {
		t.Fatalf("busyRT.Close: %v", err)
	}
	if err := <-activeDone; !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected FaultKindTrap / ExecutionFault on pool close during execution, got %v", err)
	}
}

func TestWasmFaultGuardWindowPruningResetAndDecodeSlice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// DecodePackedSlice boundary assertions
	if _, _, err := wasm.DecodePackedSlice((uint64(1024)<<32)|2048, 65536, 1024); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected ErrExecutionFault when outLen > maxOutputBytes")
	}
	if _, _, err := wasm.DecodePackedSlice((uint64(65000)<<32)|1000, 65536, 0); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected ErrExecutionFault on memory overflow")
	}

	// Nil FaultGuard safety
	var nilFG *wasm.FaultGuard
	if nilFG.IsQuarantined("inst_1") || nilFG.WindowCount("inst_1") != 0 || nilFG.RecordFault(ctx, "inst_1", wasm.FaultKindTrap, "x") {
		t.Fatalf("nil FaultGuard must safely return zero/false")
	}
	nilFG.Reset("inst_1")

	fc := clock.NewFakeClock(clock.DefaultFakeStart())
	fg := wasm.NewFaultGuard(wasm.FaultGuardConfig{Now: fc.Now})
	if fg.RecordFault(ctx, "   ", wasm.FaultKindTrap, "empty") || fg.WindowCount("") != 0 {
		t.Fatalf("blank installationID should be ignored")
	}
	for i := 0; i < 15; i++ {
		if fg.RecordFault(ctx, "inst_prune", wasm.FaultKindTrap, "early") {
			t.Fatalf("should not quarantine below threshold")
		}
	}
	if fg.WindowCount("inst_prune") != 15 {
		t.Fatalf("want WindowCount=15, got %d", fg.WindowCount("inst_prune"))
	}
	fc.Advance(61 * time.Second)
	if fg.WindowCount("inst_prune") != 0 {
		t.Fatalf("expected sliding window pruning after 61s, got %d", fg.WindowCount("inst_prune"))
	}
	for i := 0; i < 21; i++ {
		fg.RecordFault(ctx, "inst_prune", wasm.FaultKindTimeout, "burst")
	}
	if !fg.IsQuarantined("inst_prune") {
		t.Fatalf("expected quarantine after > 20 faults in window")
	}
	fg.Reset("inst_prune")
	if fg.IsQuarantined("inst_prune") || fg.WindowCount("inst_prune") != 0 {
		t.Fatalf("expected Reset to clear quarantine and counts")
	}
}

func TestWasmHostEgressEdgeCasesAndPolicyNarrowing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fc := clock.NewFakeClock(clock.DefaultFakeStart())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"sig":"%s","q":"%s"}`, r.Header.Get("X-Sig"), r.URL.Query().Get("k"))))
	}))
	defer srv.Close()

	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "egress_edge.db"), NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()
	kb := make([]byte, vault.KeySizeBytes)
	_, _ = rand.Read(kb)
	kek, err := vault.NewStaticKEKProvider("kek_edge", kb)
	clear(kb)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: func() int64 { return fc.Now().UnixMilli() }, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vs.Close()
	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "edge-org", DisplayName: "Edge"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	connID := idgen.MustNew(idgen.PrefixConnection)
	aud := srv.URL
	meta, err := vs.SealSecret(ctx, vault.SealSecretParams{
		OrgID: org.ID, Name: "hmac_k", SecretKind: vault.SecretKindAPIKey, BoundAudience: &aud, Plaintext: []byte(canary.DefaultCanarySecret()),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}
	addrHost := strings.TrimPrefix(srv.URL, "http://")
	resolver := &staticSlotResolver{slots: map[string]*broker.ResolvedSlot{}}
	resolver.put(&broker.ResolvedSlot{
		OrgID: org.ID, ConnectionID: connID, SlotName: "api_key", SecretID: meta.ID, BoundAudience: &aud,
		AllowedHosts: []string{addrHost}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"},
	})
	br, err := broker.NewBlindBroker(broker.Config{
		Resolver: resolver, PolicyResolver: resolver, Vault: vs, UnsealCap: vault.MintUnsealCapabilityForBroker(),
		Audit: &memAuditSink{}, NowMs: func() int64 { return fc.Now().UnixMilli() }, ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker: %v", err)
	}

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:            buildHostEgressBridgeWASM(),
		PoolSize:             1,
		CallTimeout:          2 * time.Second,
		AllowHostBlindEgress: true,
		BlindBroker:          br,
		AllowedHosts:         []string{addrHost},
		AllowPrivateIP:       true,
		AllowedCIDRs:         []string{"127.0.0.0/8"},
		AllowInsecureHTTP:    true,
		ApprovedSecretSlots:  []string{"api_key"},
		ABIMode:              wasm.ABIModeSliceTransform,
		SliceTransform:       &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: srv.URL + "/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	// 1. Missing EgressScope fails closed
	validReq, _ := json.Marshal(map[string]any{
		"method":     "POST",
		"target_url": srv.URL + "/v1",
	})
	if _, err := rt.TransformRawSlice(ctx, "no_scope", validReq); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected fail closed without EgressScope, got %v", err)
	}

	// 2. NarrowPolicy CIDR intersection narrows 127.0.0.0/8 ∩ 127.0.0.1/32 + QueryParam + CryptoSign works
	scopedCtx := wasm.WithEgressScope(ctx, wasm.EgressScope{
		OrgID: org.ID, ConnectionID: connID, ActorKind: "VIRTUAL_KEY", ActorID: "vky_1", EntryPoint: "test", RequestID: "r1",
		NarrowPolicy: &egress.Policy{
			AllowedHosts:   []string{addrHost},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
		},
	})
	cryptoReq, _ := json.Marshal(map[string]any{
		"method":     "POST",
		"target_url": srv.URL + "/v1",
		"credential_injections": []port.CredentialInjectInstruction{
			{
				SecretSlotName: "api_key",
				TargetLocation: port.CredentialTargetLocationHostCryptoSign,
				FieldName:      "X-Sig",
				CryptoSign: &port.HostCryptoSignSpec{
					Algorithm:        port.SigningAlgorithmHMACSHA256,
					CanonicalPayload: []byte("hello"),
					OutputEncoding:   port.SignatureEncodingHexLower,
				},
			},
			{
				SecretSlotName: "api_key",
				TargetLocation: port.CredentialTargetLocationQueryParam,
				FieldName:      "k",
			},
		},
	})
	out, err := rt.TransformRawSlice(scopedCtx, "narrow_ok", cryptoReq)
	if err != nil || !strings.Contains(string(out), `"status_code":200`) {
		t.Fatalf("narrowed host egress + crypto/query injection failed: out=%s err=%v", out, err)
	}

	// 3. Disjoint CIDR NarrowPolicy rejects (empty common CIDRs with AllowPrivateIP -> broker error -> guest trap)
	badNarrowCtx := wasm.WithEgressScope(ctx, wasm.EgressScope{
		OrgID: org.ID, ConnectionID: connID,
		NarrowPolicy: &egress.Policy{
			AllowedHosts:   []string{addrHost},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"10.0.0.0/8"},
		},
	})
	if _, err := rt.TransformRawSlice(badNarrowCtx, "disjoint_cidr", validReq); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected disjoint CIDR rejection, got %v", err)
	}

	// 4. Malformed guest JSON in blind_http_egress fails closed
	if _, err := rt.TransformRawSlice(scopedCtx, "bad_json", []byte("{bad")); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected malformed egress JSON rejection, got %v", err)
	}
}

func TestWasmAllocAndBadImportSigBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. tanpopo_alloc returns valid ptr (80000) when len<=16 and OOB ptr (9999999) when len>16
	var allocBody []byte
	allocBody = append(allocBody, 0x00)
	allocBody = append(allocBody,
		0x20, 0x00, 0x41, 0x10, 0x49, // len <= 16 (i32.le_u)
		0x04, 0x7f, // if -> i32
		0x41,
	)
	allocBody = append(allocBody, sleb128Int64(80000)...)
	allocBody = append(allocBody,
		0x05, // else
		0x41,
	)
	allocBody = append(allocBody, sleb128Int64(9999999)...)
	allocBody = append(allocBody, 0x0b, 0x0b)

	// transform(ptr, len) -> (ptr << 32) | len
	trBody := []byte{
		0x00,
		0x20, 0x00, 0xad, 0x42, 0x20, 0x86,
		0x20, 0x01, 0xad, 0x84, 0x0b,
	}

	var exp []byte
	exp = append(exp, 0x03)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wasmString("transform")...)
	exp = append(exp, 0x00, 0x00)
	exp = append(exp, wasmString("tanpopo_alloc")...)
	exp = append(exp, 0x00, 0x01)

	var codeSec []byte
	codeSec = append(codeSec, 0x02)
	codeSec = append(codeSec, uleb128(uint32(len(trBody)))...)
	codeSec = append(codeSec, trBody...)
	codeSec = append(codeSec, uleb128(uint32(len(allocBody)))...)
	codeSec = append(codeSec, allocBody...)

	allocRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMinimalCustomWASM(
			[]byte{0x02, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, 0x60, 0x01, 0x7f, 0x01, 0x7f},
			nil,
			[]byte{0x02, 0x00, 0x01},
			[]byte{0x01, 0x01, 0x02, 0x10},
			exp,
			nil,
			codeSec,
			nil,
		),
		PoolSize:       1,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime allocRT: %v", err)
	}
	defer allocRT.Close(ctx)

	if out, err := allocRT.TransformRawSlice(ctx, "alloc_ok", []byte("short")); err != nil || string(out) != "short" {
		t.Fatalf("tanpopo_alloc valid failed: %q err=%v", out, err)
	}
	if _, err := allocRT.TransformRawSlice(ctx, "alloc_oob", make([]byte, 32)); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected FaultKindOOM on invalid tanpopo_alloc ptr, got %v", err)
	}

	// 2. Host import tanpopo_host_v1.blind_http_egress with bad signature fails validation even when AllowHostBlindEgress=true
	badHostSigWasm := buildImportRejectWASM(wasm.HostModuleName, wasm.HostFuncBlindHTTPEgress)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:            badHostSigWasm,
		AllowHostBlindEgress: true,
		BlindBroker:          &broker.BlindHostCredentialBroker{},
		AllowedHosts:         []string{"api.acme.example"},
	}); !errors.Is(err, wasm.ErrUnapprovedWASIImport) {
		t.Fatalf("expected signature mismatch ErrUnapprovedWASIImport, got %v", err)
	}
}

func TestWasmConcurrentCancelWatchdogEvictRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		PoolSize:         1,
		CallTimeout:      40 * time.Millisecond,
		WatchdogInterval: 2 * time.Millisecond,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	fg := wasm.NewFaultGuard(wasm.FaultGuardConfig{})
	rtFG, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:        wasmfixtures.EchoTransformWASM(),
		InstallationID:   "ins_race",
		PoolSize:         1,
		CallTimeout:      200 * time.Millisecond,
		WatchdogInterval: 2 * time.Millisecond,
		FaultGuard:       fg,
		ABIMode:          wasm.ABIModeSliceTransform,
		AllowedHosts:     []string{"api.acme.example"},
		SliceTransform:   &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime rtFG: %v", err)
	}

	// Race fast clean completions against CancelExecution, AfterFunc cancels, and EvictAllWarm.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				reqID := fmt.Sprintf("race_%d_%d", worker, j)
				cctx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
				out, cErr := rt.TransformRawSlice(cctx, reqID, []byte("ping"))
				cancel()
				if cErr == nil && string(out) != "ping" {
					t.Errorf("corrupt output %q", out)
				}
				if j%7 == 0 {
					_, _ = rt.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: reqID})
				}
			}
		}(i)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				reqID := fmt.Sprintf("fg_race_%d_%d", worker, j)
				_, _ = rtFG.TransformRawSlice(ctx, reqID, []byte("pong"))
				_, _ = rtFG.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: reqID})
			}
		}(i)
	}
	fgEvict := wasm.NewFaultGuard(wasm.FaultGuardConfig{MaxFaultsInWindow: 2})
	rtEvict, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		InstallationID: "ins_evict_race",
		PoolSize:       2,
		CallTimeout:    25 * time.Millisecond,
		FaultGuard:     fgEvict,
		AllowedHosts:   []string{"api.acme.example"},
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime rtEvict: %v", err)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				reqID := fmt.Sprintf("evict_%d_%d", worker, j)
				out, eErr := rtEvict.TransformRawSlice(ctx, reqID, []byte("ping"))
				if eErr == nil && string(out) != "ping" {
					t.Errorf("corrupt output during EvictAllWarm race %q", out)
				}
				_, _ = rtEvict.CancelExecution(ctx, port.CancelExecutionSignal{RequestID: reqID})
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 10; j++ {
			fgEvict.RecordFault(ctx, "ins_evict_race", wasm.FaultKindTrap, "ev1")
			fgEvict.RecordFault(ctx, "ins_evict_race", wasm.FaultKindTrap, "ev2")
			fgEvict.RecordFault(ctx, "ins_evict_race", wasm.FaultKindTrap, "ev3")
			fgEvict.Reset("ins_evict_race")
		}
	}()
	wg.Wait()
	if err := rtEvict.Close(ctx); err != nil {
		t.Fatalf("rtEvict.Close: %v", err)
	}

	// Closing runtime under idle/post-race must not record FaultGuard shutdown faults.
	if err := rtFG.Close(ctx); err != nil {
		t.Fatalf("rtFG.Close: %v", err)
	}
	if _, closeErr := rtFG.TransformRawSlice(ctx, "after_close", []byte("x")); !errors.Is(closeErr, wasm.ErrWasmRuntimeClosed) {
		t.Fatalf("expected ErrWasmRuntimeClosed after Close, got %v", closeErr)
	}
	if cnt := fg.WindowCount("ins_race"); cnt != 0 {
		t.Fatalf("pool close or normal cancel must not poison FaultGuard count, got %d", cnt)
	}

	// Busy infinite-loop close also yields FaultKindClosed without poisoning FaultGuard.
	fgBusy := wasm.NewFaultGuard(wasm.FaultGuardConfig{MaxFaultsInWindow: 1})
	busyRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.InfiniteLoopWASM(),
		InstallationID: "ins_busy_close",
		PoolSize:       1,
		CallTimeout:    2 * time.Second,
		FaultGuard:     fgBusy,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime busyRT: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, e := busyRT.TransformRawSlice(ctx, "in_flight_close", []byte("loop"))
		done <- e
	}()
	for i := 0; i < 100 && busyRT.Stats().Available > 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if err := busyRT.Close(ctx); err != nil {
		t.Fatalf("busyRT.Close: %v", err)
	}
	if err := <-done; !errors.Is(err, wasm.ErrWasmRuntimeClosed) {
		t.Fatalf("expected ErrWasmRuntimeClosed on Close during execution, got %v", err)
	}
	if fgBusy.WindowCount("ins_busy_close") != 0 || fgBusy.IsQuarantined("ins_busy_close") {
		t.Fatalf("runtime shutdown must not increment FaultGuard or quarantine")
	}
}

func TestWasmMutableGlobalsTablesAndImportHardening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Module with a mutable global `(global (mut i32) (i32.const 0))` that writes old global byte
	// at offset 65536 (returning 1 byte) and stores input[0] into the global. Re-instantiation on
	// every call guarantees Call 2 (Tenant B) sees 0 rather than Tenant A's secret byte 'A'.
	var globalMod []byte
	globalMod = append(globalMod, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	// Type 0: (i32, i32) -> i64
	globalMod = append(globalMod, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	// Func 0: type 0
	globalMod = append(globalMod, wasmSection(3, []byte{0x01, 0x00})...)
	// Mem 0: min 2 max 16
	globalMod = append(globalMod, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
	// Global 0: mutable i32 = 0
	globalMod = append(globalMod, wasmSection(6, []byte{0x01, 0x7f, 0x01, 0x41, 0x00, 0x0b})...)
	// Export: memory + transform
	var exp []byte
	exp = append(exp, 0x02)
	exp = append(exp, wasmString("memory")...)
	exp = append(exp, 0x02, 0x00)
	exp = append(exp, wasmString("transform")...)
	exp = append(exp, 0x00, 0x00)
	globalMod = append(globalMod, wasmSection(7, exp)...)
	// Code:
	// i32.const 65536; global.get 0; i32.store8;
	// local.get 0; i32.load8_u; global.set 0;
	// i64.const ((65536 << 32) | 1); end
	outOffLEB := sleb128Int64(65536)
	var body []byte
	body = append(body, 0x00, 0x41)
	body = append(body, outOffLEB...)
	body = append(body, 0x23, 0x00, 0x3a, 0x00, 0x00)
	body = append(body, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x24, 0x00)
	body = append(body, 0x42)
	body = append(body, sleb128Int64(int64((uint64(65536)<<32)|1))...)
	body = append(body, 0x0b)
	var codeSec []byte
	codeSec = append(codeSec, 0x01)
	codeSec = append(codeSec, uleb128(uint32(len(body)))...)
	codeSec = append(codeSec, body...)
	globalMod = append(globalMod, wasmSection(10, codeSec)...)

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      globalMod,
		PoolSize:       1,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1/chat"},
	})
	if err != nil {
		t.Fatalf("NewRuntime mutable global: %v", err)
	}
	defer rt.Close(ctx)

	out1, err := rt.TransformRawSlice(ctx, "tenant_a", []byte("A"))
	if err != nil || len(out1) != 1 || out1[0] != 0 {
		t.Fatalf("call 1 expected [0], got %v err=%v", out1, err)
	}
	out2, err := rt.TransformRawSlice(ctx, "tenant_b", []byte("B"))
	if err != nil || len(out2) != 1 || out2[0] != 0 {
		t.Fatalf("mutable global leaked Tenant A state into Tenant B: %v err=%v", out2, err)
	}
	if st := rt.Stats(); st.ReplacedCount != 2 {
		t.Fatalf("expected mutable global runtime to re-instantiate slot after each call (ReplacedCount=2), got %+v", st)
	}

	// 2. Imported table and imported global rejected with ErrUnapprovedWASIImport
	var impTbl []byte
	impTbl = append(impTbl, 0x01)
	impTbl = append(impTbl, wasmString("env")...)
	impTbl = append(impTbl, wasmString("tbl")...)
	impTbl = append(impTbl, 0x01, 0x70, 0x00, 0x01) // funcref table min 1
	impTblWasm := buildMinimalCustomWASM(nil, impTbl, nil, []byte{0x01, 0x01, 0x01, 0x02}, nil, nil, nil, nil)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: impTblWasm}); !errors.Is(err, wasm.ErrUnapprovedWASIImport) {
		t.Fatalf("expected ErrUnapprovedWASIImport for imported table, got %v", err)
	}

	var impGlb []byte
	impGlb = append(impGlb, 0x01)
	impGlb = append(impGlb, wasmString("env")...)
	impGlb = append(impGlb, wasmString("g")...)
	impGlb = append(impGlb, 0x03, 0x7f, 0x00) // immutable i32 global
	impGlbWasm := buildMinimalCustomWASM(nil, impGlb, nil, []byte{0x01, 0x01, 0x01, 0x02}, nil, nil, nil, nil)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: impGlbWasm}); !errors.Is(err, wasm.ErrUnapprovedWASIImport) {
		t.Fatalf("expected ErrUnapprovedWASIImport for imported global, got %v", err)
	}

	// 3. AllowHostBlindEgress=true with nil BlindBroker fails fast with ErrInvalidRuntimeConfig
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:            wasmfixtures.EchoTransformWASM(),
		AllowHostBlindEgress: true,
		AllowedHosts:         []string{"api.acme.example"},
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("expected ErrInvalidRuntimeConfig when BlindBroker is nil, got %v", err)
	}
}

func TestWasmPolicyParityFaultGuardErrorsAndSliceGates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Default-deny empty ApprovedSecretSlots rejects credential injections; [::1] IPv6 portless defaults strictly to 443.
	zeroSlotWasm := buildMultiMethodCanonicalWASM(map[string][]byte{
		"transform_request":      []byte(`{"method":"POST","target_url":"https://api.acme.example/v1","credential_injections":[{"secret_slot_name":"any","target_location":"CREDENTIAL_TARGET_LOCATION_HEADER","field_name":"Authorization"}]}`),
		"build_validation_probe": []byte(`{"method":"POST","target_url":"https://[::1]/v1/chat"}`),
	})
	zeroSlotRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:    zeroSlotWasm,
		PoolSize:     1,
		AllowedHosts: []string{"[::1]", "api.acme.example"},
	})
	if err != nil {
		t.Fatalf("NewRuntime zeroSlotRT: %v", err)
	}
	defer zeroSlotRT.Close(ctx)
	if _, err := zeroSlotRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) || !strings.Contains(err.Error(), "unapproved secret_slot_name") {
		t.Fatalf("expected default-deny secret slot rejection with empty ApprovedSecretSlots, got %v", err)
	}
	if spec, err := zeroSlotRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); err != nil || spec.TargetURL != "https://[::1]/v1/chat" {
		t.Fatalf("expected portless [::1] host rule match on 443, spec=%+v err=%v", spec, err)
	}

	// Portless [::1] must reject port 8443 when not allowed
	port8443Wasm := buildMultiMethodCanonicalWASM(map[string][]byte{
		"transform_request": []byte(`{"method":"POST","target_url":"https://[::1]:8443/v1/chat"}`),
	})
	portCheckRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:    port8443Wasm,
		PoolSize:     1,
		AllowedHosts: []string{"[::1]"},
	})
	if err != nil {
		t.Fatalf("NewRuntime portCheckRT: %v", err)
	}
	defer portCheckRT.Close(ctx)
	if _, err := portCheckRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected portless [::1] rule to block :8443, got %v", err)
	}

	// Invalid network configs rejected in NewRuntime
	badNetCfgs := []wasm.RuntimeConfig{
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedHosts: []string{"api.acme.example"}, AllowedPorts: []int{70000}},
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedHosts: []string{"api.acme.example"}, AllowPrivateIP: true},
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedHosts: []string{"api.acme.example"}, AllowPrivateIP: true, AllowedCIDRs: []string{"0.0.0.0/0"}},
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedHosts: []string{"api.acme.example"}, AllowedCIDRs: []string{"not_a_cidr"}},
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedPorts: []int{443}},
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowedCIDRs: []string{"10.0.0.0/8"}},
		{WasmBytes: wasmfixtures.EchoTransformWASM(), AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}},
		{
			WasmBytes:      wasmfixtures.EchoTransformWASM(),
			ABIMode:        wasm.ABIModeSliceTransform,
			SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
		},
	}
	for i, c := range badNetCfgs {
		if _, err := wasm.NewRuntime(ctx, c); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
			t.Fatalf("badNetCfgs[%d] expected ErrInvalidRuntimeConfig, got %v", i, err)
		}
	}

	// Empty AllowedHosts in canonical mode denies outbound specs fail-closed by default.
	emptyHostsRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMultiMethodCanonicalWASM(map[string][]byte{
			"build_validation_probe": []byte(`{"method":"GET","target_url":"https://api.acme.example/v1/models"}`),
			"transform_request":      []byte(`{"method":"POST","target_url":"https://api.acme.example/v1"}`),
		}),
		PoolSize: 1,
	})
	if err != nil {
		t.Fatalf("NewRuntime emptyHostsRT: %v", err)
	}
	defer emptyHostsRT.Close(ctx)
	if _, err := emptyHostsRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected empty AllowedHosts BuildValidationProbe rejection, got %v", err)
	}
	if _, err := emptyHostsRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) {
		t.Fatalf("expected empty AllowedHosts TransformRequest rejection, got %v", err)
	}

	// 2. Host egress intersection must fail closed when AllowedPorts or default 443 vs 8443 are disjoint.
	fc := clock.NewFakeClock(clock.DefaultFakeStart())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "port_narrow.db"), NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()
	kb := make([]byte, vault.KeySizeBytes)
	_, _ = rand.Read(kb)
	kek, err := vault.NewStaticKEKProvider("kek_port", kb)
	clear(kb)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: func() int64 { return fc.Now().UnixMilli() }, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vs.Close()
	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "port-org", DisplayName: "PortOrg"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	connID := idgen.MustNew(idgen.PrefixConnection)
	addrHost := strings.TrimPrefix(srv.URL, "http://")
	hostOnly, portStr, _ := net.SplitHostPort(addrHost)
	srvPort, _ := strconv.Atoi(portStr)
	resolver := &staticSlotResolver{slots: map[string]*broker.ResolvedSlot{}, policies: map[string]*egress.Policy{
		org.ID + "|" + connID: {AllowedHosts: []string{addrHost}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, AllowedPorts: []int{srvPort}, RequireTLS: false},
	}}
	br, err := broker.NewBlindBroker(broker.Config{
		Resolver: resolver, PolicyResolver: resolver, Vault: vs, UnsealCap: vault.MintUnsealCapabilityForBroker(),
		Audit: &memAuditSink{}, NowMs: func() int64 { return fc.Now().UnixMilli() }, ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker: %v", err)
	}
	egRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:            buildHostEgressBridgeWASM(),
		PoolSize:             1,
		CallTimeout:          2 * time.Second,
		AllowHostBlindEgress: true,
		BlindBroker:          br,
		AllowedHosts:         []string{hostOnly},
		AllowPrivateIP:       true,
		AllowedCIDRs:         []string{"127.0.0.1/32"},
		AllowedPorts:         []int{srvPort},
		AllowInsecureHTTP:    true,
		ABIMode:              wasm.ABIModeSliceTransform,
		SliceTransform:       &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: srv.URL + "/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime egRT: %v", err)
	}
	defer egRT.Close(ctx)

	disjointPortCtx := wasm.WithEgressScope(ctx, wasm.EgressScope{
		OrgID:        org.ID,
		ConnectionID: connID,
		NarrowPolicy: &egress.Policy{
			AllowedHosts:   []string{hostOnly},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.1/32"},
			AllowedPorts:   []int{srvPort + 1},
		},
	})
	reqJSON, _ := json.Marshal(map[string]any{"method": "POST", "target_url": srv.URL + "/v1"})
	if _, err := egRT.TransformRawSlice(disjointPortCtx, "disjoint_port", reqJSON); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected disjoint AllowedPorts to fail closed, got %v", err)
	}

	// 3. Slice-mode gates: disabled/quarantined BuildValidationProbe + UTF-8 enforcement on TransformResponse/EvaluateGuardrail + FaultGuard error safety.
	var fgErrors atomic.Int64
	var fgAlerts atomic.Int64
	fgFail := wasm.NewFaultGuard(wasm.FaultGuardConfig{
		MaxFaultsInWindow: 1,
		EntryPoint:        "invalid_entry_point",
		Quarantine: func(context.Context, string, string, string) error {
			return errors.New("db unavailable")
		},
		OnAlert: func(context.Context, wasm.AlertEvent) { fgAlerts.Add(1) },
		OnError: func(context.Context, error) { fgErrors.Add(1) },
	})
	fgFail.RecordFault(ctx, "ins_qfail", wasm.FaultKindTrap, "t1")
	if triggered := fgFail.RecordFault(ctx, "ins_qfail", wasm.FaultKindTrap, "t2"); triggered || fgFail.IsQuarantined("ins_qfail") || fgAlerts.Load() != 0 || fgErrors.Load() < 2 {
		t.Fatalf("expected failed quarantine not to set quarantined=true or emit alert, triggered=%v q=%v alerts=%d errs=%d",
			triggered, fgFail.IsQuarantined("ins_qfail"), fgAlerts.Load(), fgErrors.Load())
	}

	var toggle atomic.Bool
	toggle.Store(true)
	sliceGateRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		PoolSize:       1,
		EnabledFunc:    toggle.Load,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime sliceGateRT: %v", err)
	}
	defer sliceGateRT.Close(ctx)
	if _, err := sliceGateRT.TransformResponse(ctx, port.TransformResponseInput{RawResponseBody: []byte{0xff, 0xfe}}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected invalid UTF-8 rejection in slice TransformResponse, got %v", err)
	}
	if _, err := sliceGateRT.EvaluateGuardrail(ctx, port.GuardrailInput{PayloadJSON: []byte{0xff}}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected invalid UTF-8 rejection in slice EvaluateGuardrail, got %v", err)
	}
	toggle.Store(false)
	if _, err := sliceGateRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) {
		t.Fatalf("expected ErrWasmRuntimeDisabled in BuildValidationProbe when toggle off, got %v", err)
	}
	if _, err := sliceGateRT.VerifyValidationResult(ctx, port.ValidationResultInput{HTTPStatus: 200}); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) {
		t.Fatalf("expected ErrWasmRuntimeDisabled in VerifyValidationResult when toggle off, got %v", err)
	}
	if _, err := sliceGateRT.NormalizeError(ctx, port.NormalizeErrorInput{HTTPStatus: 502}); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) {
		t.Fatalf("expected ErrWasmRuntimeDisabled in NormalizeError when toggle off, got %v", err)
	}
	if _, err := sliceGateRT.ExtractUsage(ctx, port.ExtractUsageInput{}); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) {
		t.Fatalf("expected ErrWasmRuntimeDisabled in ExtractUsage when toggle off, got %v", err)
	}
	if _, err := sliceGateRT.InspectNativeRequest(ctx, port.NativeRequestInput{}); !errors.Is(err, wasm.ErrWasmRuntimeDisabled) {
		t.Fatalf("expected ErrWasmRuntimeDisabled in InspectNativeRequest when toggle off, got %v", err)
	}
}

func TestWasmImmutableGlobalsTablesEgressAndAuditBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Immutable globals (mut == 0x00 across i32/i64/f32/f64/v128) must NOT trigger re-instantiation (ReplacedCount == 0).
	var immGlobalSec []byte
	immGlobalSec = append(immGlobalSec, 0x05) // 5 immutable globals:
	// i32.const 42
	immGlobalSec = append(immGlobalSec, 0x7f, 0x00, 0x41, 0x2a, 0x0b)
	// i64.const 100
	immGlobalSec = append(immGlobalSec, 0x7e, 0x00, 0x42, 0xe4, 0x00, 0x0b)
	// f32.const 0
	immGlobalSec = append(immGlobalSec, 0x7d, 0x00, 0x43, 0, 0, 0, 0, 0x0b)
	// f64.const 0
	immGlobalSec = append(immGlobalSec, 0x7c, 0x00, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b)
	// v128.const 16 bytes
	immGlobalSec = append(immGlobalSec, 0x7b, 0x00, 0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b)

	var immWasm []byte
	immWasm = append(immWasm, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	immWasm = append(immWasm, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	immWasm = append(immWasm, wasmSection(3, []byte{0x01, 0x00})...)
	immWasm = append(immWasm, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
	immWasm = append(immWasm, wasmSection(6, immGlobalSec)...)
	var immExp []byte
	immExp = append(immExp, 0x02)
	immExp = append(immExp, wasmString("memory")...)
	immExp = append(immExp, 0x02, 0x00)
	immExp = append(immExp, wasmString("transform")...)
	immExp = append(immExp, 0x00, 0x00)
	immWasm = append(immWasm, wasmSection(7, immExp)...)
	immWasm = append(immWasm, wasmSection(10, []byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b})...)

	immRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      immWasm,
		PoolSize:       1,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime immutable globals: %v", err)
	}
	defer immRT.Close(ctx)
	for i := 0; i < 3; i++ {
		if _, err := immRT.TransformRawSlice(ctx, fmt.Sprintf("imm_%d", i), []byte("hi")); err != nil {
			t.Fatalf("immutable global call %d failed: %v", i, err)
		}
	}
	if st := immRT.Stats(); st.ReplacedCount != 0 {
		t.Fatalf("immutable globals should reuse slot without re-instantiation, got ReplacedCount=%d", st.ReplacedCount)
	}

	// 2. Module with a bounded Table section (id=4, 1 funcref table min=1 max=1) triggers re-instantiation after each call.
	var tblWasm []byte
	tblWasm = append(tblWasm, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	tblWasm = append(tblWasm, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	tblWasm = append(tblWasm, wasmSection(3, []byte{0x01, 0x00})...)
	tblWasm = append(tblWasm, wasmSection(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01})...) // 1 bounded funcref table min=1 max=1
	tblWasm = append(tblWasm, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
	tblWasm = append(tblWasm, wasmSection(7, immExp)...)
	tblWasm = append(tblWasm, wasmSection(10, []byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b})...)

	tblRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      tblWasm,
		PoolSize:       1,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime table module: %v", err)
	}
	defer tblRT.Close(ctx)
	for i := 0; i < 2; i++ {
		if _, err := tblRT.TransformRawSlice(ctx, fmt.Sprintf("tbl_%d", i), []byte("hi")); err != nil {
			t.Fatalf("table module call %d: %v", i, err)
		}
	}
	if st := tblRT.Stats(); st.ReplacedCount != 2 {
		t.Fatalf("expected table module to re-instantiate slot after each call (ReplacedCount=2), got %+v", st)
	}

	// 3. Dedicated export with wrong signature falls back to "transform" in canonical mode;
	// Wrong-sig host signing algorithms / encodings and DefaultEgressScope branches.
	// Build module exporting transform(i32,i32)->i64 AND verify_validation_result()->() so hasDedicatedExport skips wrong sig.
	var misSig []byte
	misSig = append(misSig, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	misSig = append(misSig, wasmSection(1, []byte{0x02, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, 0x60, 0x00, 0x00})...)
	misSig = append(misSig, wasmSection(3, []byte{0x02, 0x00, 0x01})...)
	misSig = append(misSig, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
	var misExp []byte
	misExp = append(misExp, 0x03)
	misExp = append(misExp, wasmString("memory")...)
	misExp = append(misExp, 0x02, 0x00)
	misExp = append(misExp, wasmString("transform")...)
	misExp = append(misExp, 0x00, 0x00)
	misExp = append(misExp, wasmString("verify_validation_result")...)
	misExp = append(misExp, 0x00, 0x01)
	misSig = append(misSig, wasmSection(7, misExp)...)
	trRet := fmt.Sprintf(`{"valid":true}`)
	trBody := []byte{0x00, 0x42}
	trBody = append(trBody, sleb128Int64(int64((uint64(65536)<<32)|uint64(len(trRet))))...)
	trBody = append(trBody, 0x0b)
	var misCode []byte
	misCode = append(misCode, 0x02)
	misCode = append(misCode, uleb128(uint32(len(trBody)))...)
	misCode = append(misCode, trBody...)
	misCode = append(misCode, 0x02, 0x00, 0x0b)
	misSig = append(misSig, wasmSection(10, misCode)...)
	var misData []byte
	misData = append(misData, 0x01, 0x00, 0x41)
	misData = append(misData, sleb128Int64(65536)...)
	misData = append(misData, 0x0b)
	misData = append(misData, uleb128(uint32(len(trRet)))...)
	misData = append(misData, trRet...)
	misSig = append(misSig, wasmSection(11, misData)...)

	misRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: misSig,
		PoolSize:  1,
	})
	if err != nil {
		t.Fatalf("NewRuntime misSig: %v", err)
	}
	defer misRT.Close(ctx)
	if v, err := misRT.VerifyValidationResult(ctx, port.ValidationResultInput{RequestID: "m1", HTTPStatus: 200}); err != nil || !v.Valid {
		t.Fatalf("expected VerifyValidationResult fallback to transform, got %+v err=%v", v, err)
	}

	// 4. FaultGuard audit append failure notifies OnError while Quarantine stays true, plus valid EntryPoint="system_reaper" & quarantined slice gate.
	var auditErrCount atomic.Int64
	errSink := &errAuditSink{err: errors.New("audit disk full")}
	fgAuditErr := wasm.NewFaultGuard(wasm.FaultGuardConfig{
		MaxFaultsInWindow: 1,
		EntryPoint:        "system_reaper",
		AuditSink:         errSink,
		OnError:           func(context.Context, error) { auditErrCount.Add(1) },
	})
	fgAuditErr.RecordFault(ctx, "ins_aud_err", wasm.FaultKindTrap, "t1")
	if !fgAuditErr.RecordFault(ctx, "ins_aud_err", wasm.FaultKindTrap, "t2") || !fgAuditErr.IsQuarantined("ins_aud_err") || auditErrCount.Load() != 1 {
		t.Fatalf("expected quarantine true and 1 audit error notification, q=%v errs=%d", fgAuditErr.IsQuarantined("ins_aud_err"), auditErrCount.Load())
	}

	qSliceRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		InstallationID: "ins_aud_err",
		PoolSize:       1,
		FaultGuard:     fgAuditErr,
		ABIMode:        wasm.ABIModeSliceTransform,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime qSliceRT: %v", err)
	}
	defer qSliceRT.Close(ctx)
	if _, err := qSliceRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected quarantined error on BuildValidationProbe, got %v", err)
	}
	canceledCtx, cancelNow := context.WithCancel(ctx)
	cancelNow()
	if _, err := qSliceRT.BuildValidationProbe(canceledCtx, port.ValidationProbeInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on BuildValidationProbe, got %v", err)
	}

	// 5. DefaultEgressScope + SensitiveHeaders + IPv6 "[::1]" + RSASHA256 / Base64URL branch coverage in hostBlindHTTPEgress.
	fc := clock.NewFakeClock(clock.DefaultFakeStart())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "def_scope.db"), NowMs: func() int64 { return fc.Now().UnixMilli() }})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()
	kb := make([]byte, vault.KeySizeBytes)
	_, _ = rand.Read(kb)
	kek, err := vault.NewStaticKEKProvider("kek_def", kb)
	clear(kb)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	defer kek.Close()
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: kek, NowMs: func() int64 { return fc.Now().UnixMilli() }, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}
	defer vs.Close()
	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "def-org", DisplayName: "DefOrg"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	connID := idgen.MustNew(idgen.PrefixConnection)
	addrHost := strings.TrimPrefix(srv.URL, "http://")
	hostOnly, portStr, _ := net.SplitHostPort(addrHost)
	srvPort, _ := strconv.Atoi(portStr)
	aud := srv.URL
	meta, err := vs.SealSecret(ctx, vault.SealSecretParams{
		OrgID: org.ID, Name: "api_k", SecretKind: vault.SecretKindAPIKey, BoundAudience: &aud, Plaintext: []byte(canary.DefaultCanarySecret()),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}
	resolver := &staticSlotResolver{slots: map[string]*broker.ResolvedSlot{}, policies: map[string]*egress.Policy{}}
	resolver.put(&broker.ResolvedSlot{
		OrgID: org.ID, ConnectionID: connID, SlotName: "api_key", SecretID: meta.ID, BoundAudience: &aud,
		AllowedHosts: []string{addrHost}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, AllowedPorts: []int{srvPort}, RequireTLS: false,
	})
	br, err := broker.NewBlindBroker(broker.Config{
		Resolver: resolver, PolicyResolver: resolver, Vault: vs, UnsealCap: vault.MintUnsealCapabilityForBroker(),
		Audit: &memAuditSink{}, NowMs: func() int64 { return fc.Now().UnixMilli() }, ControlHook: netguard.Control,
	})
	if err != nil {
		t.Fatalf("NewBlindBroker: %v", err)
	}
	defRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:            buildHostEgressBridgeWASM(),
		PoolSize:             1,
		CallTimeout:          2 * time.Second,
		AllowHostBlindEgress: true,
		BlindBroker:          br,
		AllowedHosts:         []string{hostOnly, "[::1]"},
		AllowPrivateIP:       true,
		AllowedCIDRs:         []string{"127.0.0.1/32"},
		AllowedPorts:         []int{srvPort},
		AllowInsecureHTTP:    true,
		ApprovedSecretSlots:  []string{"api_key"},
		DefaultEgressScope: &wasm.EgressScope{
			OrgID:        org.ID,
			ConnectionID: connID,
			ActorKind:    "VIRTUAL_KEY",
			ActorID:      "vky_def",
			EntryPoint:   "data_plane_guard",
			RequestID:    "req_def",
			NarrowPolicy: &egress.Policy{
				AllowedHosts:     []string{addrHost, "[::1]:" + portStr},
				AllowPrivateIP:   true,
				AllowedCIDRs:     []string{"127.0.0.0/8"},
				SensitiveHeaders: []string{"X-Custom-Secret"},
			},
		},
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: srv.URL + "/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime defRT: %v", err)
	}
	defer defRT.Close(ctx)
	reqOK, _ := json.Marshal(map[string]any{
		"method":     "POST",
		"target_url": srv.URL + "/v1",
		"credential_injections": []port.CredentialInjectInstruction{
			{SecretSlotName: "api_key", TargetLocation: port.CredentialTargetLocationHeader, FieldName: "Authorization", ValuePrefix: "Bearer "},
		},
	})
	if out, err := defRT.TransformRawSlice(ctx, "def_ok", reqOK); err != nil || !strings.Contains(string(out), `"status_code":200`) {
		t.Fatalf("expected DefaultEgressScope request to succeed, out=%s err=%v", out, err)
	}

	// AWSSigV4 & Base64URL error path on non-AWS/PEM secret cleanly rejected by broker -> guest trap
	awsReq, _ := json.Marshal(map[string]any{
		"method":     "POST",
		"target_url": srv.URL + "/v1",
		"credential_injections": []port.CredentialInjectInstruction{
			{
				SecretSlotName: "api_key",
				TargetLocation: port.CredentialTargetLocationHostCryptoSign,
				FieldName:      "X-Sig",
				CryptoSign: &port.HostCryptoSignSpec{
					Algorithm:                port.SigningAlgorithmRSASHA256,
					CanonicalPayload:         []byte("abc"),
					OutputEncoding:           port.SignatureEncodingBase64URL,
					AWSRegion:                "us-east-1",
					AWSService:               "execute-api",
					SigningTimestampEpochSec: 1700000000,
				},
			},
		},
	})
	if _, err := defRT.TransformRawSlice(ctx, "rsa_fail", awsReq); !errors.Is(err, port.ErrExecutionFault) {
		t.Fatalf("expected guest trap on non-PEM RSA key error, got %v", err)
	}
}

type errAuditSink struct {
	err error
}

func (s *errAuditSink) Append(context.Context, storageport.AuditEvent) error {
	return s.err
}

func TestWasmSecurityAuditAndPassiveDataHardening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Cross-tenant DoS prevention: pre-call oversized caller payloads fail without tripping FaultGuard.
	fg := wasm.NewFaultGuard(wasm.FaultGuardConfig{MaxFaultsInWindow: 5})
	oversizeRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		InstallationID: "ins_oversized_caller",
		PoolSize:       1,
		FaultGuard:     fg,
		AllowedHosts:   []string{"api.acme.example"},
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime oversizeRT: %v", err)
	}
	defer oversizeRT.Close(ctx)

	tooBig := make([]byte, wasm.MaxGuestPayloadBytes+1)
	for i := 0; i < 25; i++ {
		_, cErr := oversizeRT.TransformRawSlice(ctx, "big_max", tooBig)
		if !errors.Is(cErr, port.ErrExecutionFault) {
			t.Fatalf("call %d expected port.ErrExecutionFault, got %v", i, cErr)
		}
		var fe *wasm.ExecutionFaultError
		if errors.As(cErr, &fe) {
			t.Fatalf("caller oversized input must not return *ExecutionFaultError (would poison FaultGuard), got %+v", fe)
		}
	}
	if fg.WindowCount("ins_oversized_caller") != 0 || fg.IsQuarantined("ins_oversized_caller") {
		t.Fatalf("expected 0 FaultGuard hits and zero quarantine for oversized inputs, count=%d q=%v",
			fg.WindowCount("ins_oversized_caller"), fg.IsQuarantined("ins_oversized_caller"))
	}
	if out, okErr := oversizeRT.TransformRawSlice(ctx, "still_healthy", []byte("ok")); okErr != nil || string(out) != "ok" {
		t.Fatalf("expected healthy call after caller oversize rejections, out=%q err=%v", out, okErr)
	}

	// Same non-quarantine guarantee when non-alloc module rejects input exceeding linear memory size.
	staticEcho := buildMinimalCustomWASM(
		[]byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e},
		nil,
		[]byte{0x01, 0x00},
		[]byte{0x01, 0x01, 0x02, 0x02},
		func() []byte {
			var b []byte
			b = append(b, 0x02)
			b = append(b, wasmString("memory")...)
			b = append(b, 0x02, 0x00)
			b = append(b, wasmString("transform")...)
			b = append(b, 0x00, 0x00)
			return b
		}(),
		nil,
		func() []byte {
			body := []byte{0x00, 0x42, 0x00, 0x0b}
			c := []byte{0x01}
			c = append(c, uleb128(uint32(len(body)))...)
			c = append(c, body...)
			return c
		}(),
		nil,
	)
	staticRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      staticEcho,
		InstallationID: "ins_oversized_static",
		PoolSize:       1,
		InputOffset:    65536,
		FaultGuard:     fg,
		AllowedHosts:   []string{"api.acme.example"},
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime staticRT: %v", err)
	}
	defer staticRT.Close(ctx)
	overMem := make([]byte, 70000)
	for i := 0; i < 25; i++ {
		_, mErr := staticRT.TransformRawSlice(ctx, "big_mem", overMem)
		if !errors.Is(mErr, port.ErrExecutionFault) {
			t.Fatalf("static oversize %d expected ErrExecutionFault, got %v", i, mErr)
		}
		var fe *wasm.ExecutionFaultError
		if errors.As(mErr, &fe) {
			t.Fatalf("linear-mem caller oversize must not return *ExecutionFaultError, got %+v", fe)
		}
	}
	if fg.WindowCount("ins_oversized_static") != 0 || fg.IsQuarantined("ins_oversized_static") {
		t.Fatalf("expected 0 FaultGuard hits on static oversize, count=%d q=%v",
			fg.WindowCount("ins_oversized_static"), fg.IsQuarantined("ins_oversized_static"))
	}

	// 2. Unconditional requireTLS & default-deny empty AllowedHosts across Slice & Canonical modes.
	httpSliceDenyEmpty := wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.EchoTransformWASM(),
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "http://api.acme.example/v1"},
	}
	if _, err := wasm.NewRuntime(ctx, httpSliceDenyEmpty); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("expected ErrInvalidRuntimeConfig for http slice URL with AllowInsecureHTTP=false and empty AllowedHosts, got %v", err)
	}
	httpSliceDenyHosts := httpSliceDenyEmpty
	httpSliceDenyHosts.AllowedHosts = []string{"api.acme.example"}
	if _, err := wasm.NewRuntime(ctx, httpSliceDenyHosts); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("expected ErrInvalidRuntimeConfig for http slice URL with AllowInsecureHTTP=false, got %v", err)
	}
	httpSliceAllow := httpSliceDenyHosts
	httpSliceAllow.AllowInsecureHTTP = true
	allowRT, err := wasm.NewRuntime(ctx, httpSliceAllow)
	if err != nil {
		t.Fatalf("expected AllowInsecureHTTP=true http slice URL to succeed, got %v", err)
	}
	if spec, err := allowRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); err != nil || spec.TargetURL != "http://api.acme.example/v1" {
		t.Fatalf("BuildValidationProbe allowRT failed: %+v err=%v", spec, err)
	}
	// Slice BuildValidationProbe fails closed after Close().
	_ = allowRT.Close(ctx)
	if _, err := allowRT.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, wasm.ErrWasmRuntimeClosed) {
		t.Fatalf("expected ErrWasmRuntimeClosed on slice BuildValidationProbe after Close, got %v", err)
	}

	// Canonical runtime with empty AllowedHosts: HTTP fails on TLS, HTTPS fails on empty allowed_hosts, non-egress RPCs work.
	canEmptyHTTP, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes: buildMultiMethodCanonicalWASM(map[string][]byte{
			"build_validation_probe":   []byte(`{"method":"GET","target_url":"http://api.acme.example/v1/models"}`),
			"transform_request":        []byte(`{"method":"POST","target_url":"http://api.acme.example/v1"}`),
			"evaluate_guardrail":       []byte(`{"allowed":true}`),
			"verify_validation_result": []byte(`{"valid":true}`),
		}),
		PoolSize: 1,
	})
	if err != nil {
		t.Fatalf("NewRuntime canEmptyHTTP: %v", err)
	}
	defer canEmptyHTTP.Close(ctx)
	if _, err := canEmptyHTTP.BuildValidationProbe(ctx, port.ValidationProbeInput{}); !errors.Is(err, port.ErrInvalidSpec) || !strings.Contains(err.Error(), "https scheme") {
		t.Fatalf("expected TLS rejection on canonical BuildValidationProbe with empty AllowedHosts, got %v", err)
	}
	if _, err := canEmptyHTTP.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, port.ErrInvalidSpec) || !strings.Contains(err.Error(), "https scheme") {
		t.Fatalf("expected TLS rejection on canonical TransformRequest with empty AllowedHosts, got %v", err)
	}
	if v, err := canEmptyHTTP.EvaluateGuardrail(ctx, port.GuardrailInput{Stage: port.GuardrailStagePreRequest}); err != nil || !v.Allowed {
		t.Fatalf("expected non-egress EvaluateGuardrail to succeed on empty AllowedHosts, v=%+v err=%v", v, err)
	}
	if vv, err := canEmptyHTTP.VerifyValidationResult(ctx, port.ValidationResultInput{HTTPStatus: 200}); err != nil || !vv.Valid {
		t.Fatalf("expected VerifyValidationResult to succeed on empty AllowedHosts, vv=%+v err=%v", vv, err)
	}

	// 3. Passive data segments (`memory.init` + `data.drop`) force slot reinstantiation across pooled calls (no trap).
	var pExp []byte
	pExp = append(pExp, 0x02)
	pExp = append(pExp, wasmString("memory")...)
	pExp = append(pExp, 0x02, 0x00)
	pExp = append(pExp, wasmString("transform")...)
	pExp = append(pExp, 0x00, 0x00)

	var pBody []byte
	pBody = append(pBody, 0x00) // 0 locals
	pBody = append(pBody, 0x41)
	pBody = append(pBody, sleb128Int64(65536)...)
	pBody = append(pBody, 0x41, 0x00, 0x41, 0x04) // offset=0, len=4
	pBody = append(pBody, 0xfc, 0x08, 0x00, 0x00) // memory.init 0 0
	pBody = append(pBody, 0xfc, 0x09, 0x00)       // data.drop 0
	pBody = append(pBody, 0x42)
	pBody = append(pBody, sleb128Int64(int64((uint64(65536)<<32)|4))...)
	pBody = append(pBody, 0x0b)
	pCode := []byte{0x01}
	pCode = append(pCode, uleb128(uint32(len(pBody)))...)
	pCode = append(pCode, pBody...)

	var passiveWasm []byte
	passiveWasm = append(passiveWasm, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	passiveWasm = append(passiveWasm, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	passiveWasm = append(passiveWasm, wasmSection(3, []byte{0x01, 0x00})...)
	passiveWasm = append(passiveWasm, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x02})...)
	passiveWasm = append(passiveWasm, wasmSection(7, pExp)...)
	passiveWasm = append(passiveWasm, wasmSection(12, []byte{0x01})...) // DataCount before Code
	passiveWasm = append(passiveWasm, wasmSection(10, pCode)...)
	passiveWasm = append(passiveWasm, wasmSection(11, []byte{0x01, 0x01, 0x04, 'p', 'o', 'n', 'g'})...)

	passiveRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      passiveWasm,
		PoolSize:       1,
		AllowedHosts:   []string{"api.acme.example"},
		ABIMode:        wasm.ABIModeSliceTransform,
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime passiveRT: %v", err)
	}
	defer passiveRT.Close(ctx)
	for i := 0; i < 2; i++ {
		out, err := passiveRT.TransformRawSlice(ctx, fmt.Sprintf("pd_%d", i), []byte("x"))
		if err != nil || string(out) != "pong" {
			t.Fatalf("passive data segment call %d failed (data.drop leak?): out=%q err=%v", i, out, err)
		}
	}
	if st := passiveRT.Stats(); st.ReplacedCount < 2 {
		t.Fatalf("expected passive data module to reinstantiate slot after each call, stats=%+v", st)
	}

	// 4. Mis-typed exported tanpopo_alloc signature fails fast at NewRuntime.
	var badAllocExp []byte
	badAllocExp = append(badAllocExp, 0x03)
	badAllocExp = append(badAllocExp, wasmString("memory")...)
	badAllocExp = append(badAllocExp, 0x02, 0x00)
	badAllocExp = append(badAllocExp, wasmString("transform")...)
	badAllocExp = append(badAllocExp, 0x00, 0x00)
	badAllocExp = append(badAllocExp, wasmString("tanpopo_alloc")...)
	badAllocExp = append(badAllocExp, 0x00, 0x01)
	trBody := []byte{0x00, 0x42, 0x00, 0x0b}
	badAllocCode := []byte{0x02}
	badAllocCode = append(badAllocCode, uleb128(uint32(len(trBody)))...)
	badAllocCode = append(badAllocCode, trBody...)
	badAllocCode = append(badAllocCode, 0x02, 0x00, 0x0b)
	badAllocWasm := buildMinimalCustomWASM(
		[]byte{0x02, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, 0x60, 0x00, 0x00},
		nil,
		[]byte{0x02, 0x00, 0x01},
		[]byte{0x01, 0x01, 0x02, 0x02},
		badAllocExp,
		nil,
		badAllocCode,
		nil,
	)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: badAllocWasm}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "tanpopo_alloc") {
		t.Fatalf("expected ErrInvalidRuntimeConfig on mistyped tanpopo_alloc, got %v", err)
	}

	// 5. Section 4 Table DoS hardening: zero-max table, huge 16M-element table, multi-table, and externref rejected pre-compile.
	buildTableTestWasm := func(tablePayload []byte) []byte {
		var b []byte
		b = append(b, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
		b = append(b, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
		b = append(b, wasmSection(3, []byte{0x01, 0x00})...)
		b = append(b, wasmSection(4, tablePayload)...)
		b = append(b, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
		var exp []byte
		exp = append(exp, 0x02)
		exp = append(exp, wasmString("memory")...)
		exp = append(exp, 0x02, 0x00)
		exp = append(exp, wasmString("transform")...)
		exp = append(exp, 0x00, 0x00)
		b = append(b, wasmSection(7, exp)...)
		b = append(b, wasmSection(10, []byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b})...)
		return b
	}

	// Zero-max table (flags=0x00, min=1) rejected.
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: buildTableTestWasm([]byte{0x01, 0x70, 0x00, 0x01})}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "bounded maximum") {
		t.Fatalf("expected ErrInvalidRuntimeConfig for zero-max table, got %v", err)
	}
	// Huge 16,777,216-element table rejected before host allocates table memory.
	hugeTbl := []byte{0x01, 0x70, 0x01}
	hugeTbl = append(hugeTbl, uleb128(16777216)...)
	hugeTbl = append(hugeTbl, uleb128(16777216)...)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: buildTableTestWasm(hugeTbl)}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "exceed max") {
		t.Fatalf("expected ErrInvalidRuntimeConfig for 16M-element table, got %v", err)
	}
	// Multi-table (count=2) and externref (0x6f) rejected.
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: buildTableTestWasm([]byte{0x02, 0x70, 0x01, 0x01, 0x01, 0x70, 0x01, 0x01, 0x01})}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "table") {
		t.Fatalf("expected ErrInvalidRuntimeConfig for multi-table module, got %v", err)
	}
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: buildTableTestWasm([]byte{0x01, 0x6f, 0x01, 0x01, 0x01})}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "table element type") {
		t.Fatalf("expected ErrInvalidRuntimeConfig for externref table, got %v", err)
	}

	// 6. CoreFeatureReferenceTypes opcodes (table.grow, ref.func, ref.null) fail at CompileModule with feature "reference-types" disabled.
	var tgExp []byte
	tgExp = append(tgExp, 0x02)
	tgExp = append(tgExp, wasmString("memory")...)
	tgExp = append(tgExp, 0x02, 0x00)
	tgExp = append(tgExp, wasmString("transform")...)
	tgExp = append(tgExp, 0x00, 0x00)

	// table.grow: ref.null 0x70; i32.const 1; table.grow 0; drop; i64.const 0; end
	tableGrowBody := []byte{0x00, 0xd0, 0x70, 0x41, 0x01, 0xfc, 0x0f, 0x00, 0x1a, 0x42, 0x00, 0x0b}
	var tgCode []byte
	tgCode = append(tgCode, 0x01)
	tgCode = append(tgCode, uleb128(uint32(len(tableGrowBody)))...)
	tgCode = append(tgCode, tableGrowBody...)
	var tableGrowWasm []byte
	tableGrowWasm = append(tableGrowWasm, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	tableGrowWasm = append(tableGrowWasm, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	tableGrowWasm = append(tableGrowWasm, wasmSection(3, []byte{0x01, 0x00})...)
	tableGrowWasm = append(tableGrowWasm, wasmSection(4, []byte{0x01, 0x70, 0x01, 0x01, 0x02})...)
	tableGrowWasm = append(tableGrowWasm, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
	tableGrowWasm = append(tableGrowWasm, wasmSection(7, tgExp)...)
	tableGrowWasm = append(tableGrowWasm, wasmSection(10, tgCode)...)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: tableGrowWasm}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "reference-types") {
		t.Fatalf("expected ErrInvalidRuntimeConfig mentioning reference-types for table.grow / ref.null, got %v", err)
	}

	// ref.func 0; drop; i64.const 0; end
	refFuncBody := []byte{0x00, 0xd2, 0x00, 0x1a, 0x42, 0x00, 0x0b}
	var rfCode []byte
	rfCode = append(rfCode, 0x01)
	rfCode = append(rfCode, uleb128(uint32(len(refFuncBody)))...)
	rfCode = append(rfCode, refFuncBody...)
	var refFuncWasm []byte
	refFuncWasm = append(refFuncWasm, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	refFuncWasm = append(refFuncWasm, wasmSection(1, []byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e})...)
	refFuncWasm = append(refFuncWasm, wasmSection(3, []byte{0x01, 0x00})...)
	refFuncWasm = append(refFuncWasm, wasmSection(5, []byte{0x01, 0x01, 0x02, 0x10})...)
	refFuncWasm = append(refFuncWasm, wasmSection(7, tgExp)...)
	refFuncWasm = append(refFuncWasm, wasmSection(9, []byte{0x01, 0x03, 0x00, 0x01, 0x00})...) // declarative elem segment
	refFuncWasm = append(refFuncWasm, wasmSection(10, rfCode)...)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: refFuncWasm}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "reference-types") {
		t.Fatalf("expected ErrInvalidRuntimeConfig mentioning reference-types for ref.func, got %v", err)
	}

	// 7. tanpopo_alloc module with small declared max memory (2 pages = 128 KiB): 200 KiB caller payloads 25x must return port.ErrExecutionFault without quarantining.
	var allocExp []byte
	allocExp = append(allocExp, 0x03)
	allocExp = append(allocExp, wasmString("memory")...)
	allocExp = append(allocExp, 0x02, 0x00)
	allocExp = append(allocExp, wasmString("transform")...)
	allocExp = append(allocExp, 0x00, 0x00)
	allocExp = append(allocExp, wasmString("tanpopo_alloc")...)
	allocExp = append(allocExp, 0x00, 0x01)
	allocTrBody := []byte{0x00, 0x42, 0x00, 0x0b}
	allocFnBody := []byte{0x00, 0x41, 0x80, 0x80, 0x04, 0x0b} // i32.const 65536; end
	var smallAllocCode []byte
	smallAllocCode = append(smallAllocCode, 0x02)
	smallAllocCode = append(smallAllocCode, uleb128(uint32(len(allocTrBody)))...)
	smallAllocCode = append(smallAllocCode, allocTrBody...)
	smallAllocCode = append(smallAllocCode, uleb128(uint32(len(allocFnBody)))...)
	smallAllocCode = append(smallAllocCode, allocFnBody...)
	smallAllocWasm := buildMinimalCustomWASM(
		[]byte{0x02, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, 0x60, 0x01, 0x7f, 0x01, 0x7f},
		nil,
		[]byte{0x02, 0x00, 0x01},
		[]byte{0x01, 0x01, 0x02, 0x02}, // min=2, max=2 (128 KiB)
		allocExp,
		nil,
		smallAllocCode,
		nil,
	)
	smallAllocFG := wasm.NewFaultGuard(wasm.FaultGuardConfig{MaxFaultsInWindow: 20, Window: time.Minute})
	smallAllocRT, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      smallAllocWasm,
		InstallationID: "ins_small_alloc_caller",
		PoolSize:       1,
		ABIMode:        wasm.ABIModeSliceTransform,
		FaultGuard:     smallAllocFG,
		AllowedHosts:   []string{"api.acme.example"},
		SliceTransform: &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("NewRuntime smallAllocRT: %v", err)
	}
	defer smallAllocRT.Close(ctx)
	oversizeSmall := make([]byte, 200*1024)
	for i := 0; i < 25; i++ {
		_, err := smallAllocRT.TransformRawSlice(ctx, fmt.Sprintf("sa_%d", i), oversizeSmall)
		if !errors.Is(err, port.ErrExecutionFault) {
			t.Fatalf("call %d expected port.ErrExecutionFault, got %v", i, err)
		}
		var fe *wasm.ExecutionFaultError
		if errors.As(err, &fe) {
			t.Fatalf("caller > memDef.Max payload must not surface as *ExecutionFaultError: %v", err)
		}
	}
	if smallAllocFG.IsQuarantined("ins_small_alloc_caller") || smallAllocFG.WindowCount("ins_small_alloc_caller") != 0 {
		t.Fatalf("caller > memDef.Max payload must not quarantine or increment FaultGuard")
	}

	// 8. Oversized raw Wasm binary > MaxWasmBinaryBytes fails fast pre-compile, and nil *Runtime RPCs return ErrWasmRuntimeClosed without panic.
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: make([]byte, wasm.MaxWasmBinaryBytes+1)}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("expected ErrInvalidRuntimeConfig for > MaxWasmBinaryBytes module, got %v", err)
	}
	var nilRT *wasm.Runtime
	if _, err := nilRT.TransformRequest(ctx, port.TransformRequestInput{}); !errors.Is(err, wasm.ErrWasmRuntimeClosed) {
		t.Fatalf("expected ErrWasmRuntimeClosed on nil TransformRequest, got %v", err)
	}
	if _, err := nilRT.TransformRawSlice(ctx, "nil", []byte("x")); !errors.Is(err, wasm.ErrWasmRuntimeClosed) {
		t.Fatalf("expected ErrWasmRuntimeClosed on nil TransformRawSlice, got %v", err)
	}

	// 9. Pre-compile Wasm binary DoS hardening: Sec 10 compressed locals bomb, inflated vector counts,
	// framing errors, Section 10 byte cap, 5th-byte ULEB overflow, and zero max memory pages.
	magic := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	buildHeaderWithSec := func(sec ...byte) []byte {
		out := append([]byte(nil), magic...)
		return append(out, sec...)
	}

	// 9a. Sec 10 compressed locals = 0xffffffff (ff ff ff ff 0f) -> 4 GiB bomb caught pre-compile.
	localsBomb := append([]byte(nil), magic...)
	localsBomb = append(localsBomb, wasmSection(1, []byte{0x01, 0x60, 0x00, 0x00})...)
	localsBomb = append(localsBomb, wasmSection(3, []byte{0x01, 0x00})...)
	localsBomb = append(localsBomb, wasmSection(5, []byte{0x01, 0x01, 0x01, 0x01})...)
	bombBody := []byte{0x01, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x7f, 0x0b}
	bombCode := []byte{0x01, byte(len(bombBody))}
	bombCode = append(bombCode, bombBody...)
	localsBomb = append(localsBomb, wasmSection(10, bombCode)...)
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: localsBomb}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) || !strings.Contains(err.Error(), "locals") {
		t.Fatalf("expected ErrInvalidRuntimeConfig for Sec 10 locals bomb, got %v", err)
	}

	// 9b. Inflated 0x7fffffff vector counts across Sections 1..11 rejected pre-compile.
	inflatedVec := []byte{0xff, 0xff, 0xff, 0xff, 0x07}
	for _, secID := range []byte{1, 3, 4, 5, 6, 7, 9, 10, 11} {
		poc := buildHeaderWithSec(wasmSection(secID, inflatedVec)...)
		if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: poc}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
			t.Fatalf("section %d inflated count 0x7fffffff: expected ErrInvalidRuntimeConfig, got %v", secID, err)
		}
	}
	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: buildHeaderWithSec(wasmSection(2, inflatedVec)...)}); !errors.Is(err, wasm.ErrUnapprovedWASIImport) {
		t.Fatalf("section 2 inflated count 0x7fffffff: expected ErrUnapprovedWASIImport, got %v", err)
	}

	// 9c. Framing, duplicate, out-of-order, unknown, mismatch, ULEB overflow, MaxWasmCodeBytes, and max=0 memory.
	hugeCodePayload := make([]byte, wasm.MaxWasmCodeBytes+2)
	hugeCodePayload[0] = 0x01
	var codeTooBig []byte
	codeTooBig = append(codeTooBig, magic...)
	codeTooBig = append(codeTooBig, wasmSection(1, []byte{0x01, 0x60, 0x00, 0x00})...)
	codeTooBig = append(codeTooBig, wasmSection(3, []byte{0x01, 0x00})...)
	codeTooBig = append(codeTooBig, wasmSection(10, hugeCodePayload)...)

	var zeroMaxExp []byte
	zeroMaxExp = append(zeroMaxExp, 0x02)
	zeroMaxExp = append(zeroMaxExp, wasmString("memory")...)
	zeroMaxExp = append(zeroMaxExp, 0x02, 0x00)
	zeroMaxExp = append(zeroMaxExp, wasmString("transform")...)
	zeroMaxExp = append(zeroMaxExp, 0x00, 0x00)
	zeroMaxMemWasm := buildMinimalCustomWASM(
		[]byte{0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e},
		nil,
		[]byte{0x01, 0x00},
		[]byte{0x01, 0x01, 0x00, 0x00}, // min=0, max=0
		zeroMaxExp,
		nil,
		[]byte{0x01, 0x04, 0x00, 0x42, 0x00, 0x0b},
		nil,
	)

	malformedCases := []struct {
		name string
		raw  []byte
	}{
		{"truncated_sec_header", buildHeaderWithSec(0x01, 0x05, 0x00)},
		{"unknown_sec_13", buildHeaderWithSec(wasmSection(13, []byte{0x00})...)},
		{"duplicate_sec_1", buildHeaderWithSec(append(wasmSection(1, []byte{0x00}), wasmSection(1, []byte{0x00})...)...)},
		{"out_of_order_3_before_1", buildHeaderWithSec(append(wasmSection(3, []byte{0x00}), wasmSection(1, []byte{0x00})...)...)},
		{"malformed_custom_sec_0", buildHeaderWithSec(wasmSection(0, []byte{0x05, 'a'})...)},
		{"func_code_count_mismatch", buildHeaderWithSec(append(wasmSection(1, []byte{0x01, 0x60, 0x00, 0x00}), wasmSection(3, []byte{0x01, 0x00})...)...)},
		{"datacount_data_mismatch", buildHeaderWithSec(wasmSection(12, []byte{0x02})...)},
		{"code_section_exceeds_max_bytes", codeTooBig},
		{"uleb32_5th_byte_overflow", buildHeaderWithSec(0x01, 0x80, 0x80, 0x80, 0x80, 0x10)},
		{"zero_max_linear_memory", zeroMaxMemWasm},
	}
	for _, tc := range malformedCases {
		if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{WasmBytes: tc.raw}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
			t.Fatalf("%s: expected ErrInvalidRuntimeConfig, got %v", tc.name, err)
		}
	}
}

func TestWasmTimeoutUpperBoundsFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:   wasmfixtures.EchoTransformWASM(),
		CallTimeout: wasm.MaxAllowedCallTimeout + time.Millisecond,
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("expected ErrInvalidRuntimeConfig for CallTimeout > MaxAllowedCallTimeout, got %v", err)
	}

	if _, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:          wasmfixtures.EchoTransformWASM(),
		StreamChunkTimeout: wasm.MaxAllowedStreamChunkTimeout + time.Millisecond,
	}); !errors.Is(err, wasm.ErrInvalidRuntimeConfig) {
		t.Fatalf("expected ErrInvalidRuntimeConfig for StreamChunkTimeout > MaxAllowedStreamChunkTimeout, got %v", err)
	}

	rt, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:          wasmfixtures.EchoTransformWASM(),
		PoolSize:           1,
		CallTimeout:        wasm.MaxAllowedCallTimeout,
		StreamChunkTimeout: wasm.MaxAllowedStreamChunkTimeout,
		ABIMode:            wasm.ABIModeSliceTransform,
		AllowedHosts:       []string{"api.acme.example"},
		SliceTransform:     &wasm.SliceTransformConfig{Method: http.MethodPost, TargetURL: "https://api.acme.example/v1"},
	})
	if err != nil {
		t.Fatalf("expected boundary timeouts (%v / %v) to be accepted, got %v", wasm.MaxAllowedCallTimeout, wasm.MaxAllowedStreamChunkTimeout, err)
	}
	_ = rt.Close(ctx)
}

