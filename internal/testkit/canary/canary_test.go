// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package canary

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// spyTB intercepts Fatalf without aborting the outer Go test runner so NEG-2 can
// verify AssertClean and AssertCleanDir fail and redact secrets cleanly.
type spyTB struct {
	testing.TB
	failed bool
	msg    string
}

func (s *spyTB) Helper() {}

func (s *spyTB) Fatalf(format string, args ...any) {
	s.failed = true
	s.msg = fmt.Sprintf(format, args...)
	panic("spyTB.Fatalf")
}

func callExpectingFatalf(fn func()) (failed bool, msg string) {
	defer func() {
		if r := recover(); r != nil {
			if r == "spyTB.Fatalf" {
				return
			}
			panic(r)
		}
	}()
	fn()
	return false, ""
}

func TestCanaryScannerDetectsAllVariantsAndRedactsNEG2(t *testing.T) {
	t.Parallel()

	scanner := NewScanner()
	token := DefaultCanarySecret()
	b64Std := base64.StdEncoding.EncodeToString([]byte(token))
	b64Shifted := base64.StdEncoding.EncodeToString([]byte("Authorization: Bearer " + token))
	urlEsc := url.QueryEscape(token)
	doubleURLEsc := url.QueryEscape(strings.ReplaceAll(token, "-", "%2D"))
	jsonUnicode := strings.ReplaceAll(token, "-", `\u002d`)

	variants := []struct {
		name    string
		payload string
	}{
		{"raw", `error connecting with key ` + token},
		{"base64-std", `{"auth":"` + b64Std + `"}`},
		{"base64-shifted-bearer", `header=` + b64Shifted},
		{"base64-rawurl", base64.RawURLEncoding.EncodeToString([]byte(token))},
		{"url-query", `https://localhost/cb?api_key=` + urlEsc},
		{"url-percent-encoded", `auth=` + strings.ReplaceAll(token, "-", "%2D")},
		{"url-double-percent-encoded", `next=` + doubleURLEsc},
		{"url-invalid-percent-then-secret", `broken=%ZZ&k=` + strings.ReplaceAll(token, "-", "%2d")},
		{"json-unicode-escaped", `{"secret":"` + jsonUnicode + `"}`},
		{"ci-forbidden-token", `leak ` + DefaultForbiddenCIKey()},
	}

	for _, tc := range variants {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := NewSafeBuffer()
			if _, err := buf.Write([]byte(tc.payload)); err != nil {
				t.Fatalf("write buffer: %v", err)
			}

			spy := &spyTB{TB: t}
			_, _ = callExpectingFatalf(func() {
				scanner.AssertCleanBuffer(spy, tc.name, buf)
			})
			if !spy.failed {
				t.Fatalf("expected AssertCleanBuffer to fail on %s leak", tc.name)
			}
			for _, forbidden := range []string{token, b64Std, b64Shifted, urlEsc, DefaultForbiddenCIKey()} {
				if strings.Contains(spy.msg, forbidden) {
					t.Fatalf("failure output leaked secret variant %q: %s", tc.name, spy.msg)
				}
			}
		})
	}
}

func TestCanaryScannerFilesJSONCustomSecretsAndCleanPass(t *testing.T) {
	t.Parallel()

	scanner := NewScanner()
	scanner.AddSecret("")
	scanner.AddSecret(DefaultCanarySecret()) // duplicate ignored cleanly
	customSecret := "custom-secret-token-xyz-9911"
	scanner.AddSecret(customSecret)

	// Clean payloads and directory pass cleanly.
	cleanDir := t.TempDir()
	cleanFile := filepath.Join(cleanDir, "clean.log")
	if err := os.WriteFile(cleanFile, []byte("2026-11-14T10:00:00Z status=ok model=gpt-4o\n"), 0o600); err != nil {
		t.Fatalf("write clean file: %v", err)
	}
	scanner.AssertCleanDir(t, cleanDir)
	scanner.AssertClean(t, NamedData{Source: "clean-json", Data: []byte(`{"status":"ok","tokens":42}`)})
	if err := scanner.Check("clean", []byte("nothing sensitive here")); err != nil {
		t.Fatalf("expected clean Check error nil, got %v", err)
	}

	// ScanJSON across structs, json.RawMessage, and string.
	if findings, err := scanner.ScanJSON("struct", struct{ Auth string }{Auth: DefaultCanarySecret()}); err != nil || len(findings) == 0 {
		t.Fatalf("expected ScanJSON struct leak finding, err=%v findings=%v", err, findings)
	}
	if findings, err := scanner.ScanJSON("rawmsg", json.RawMessage(`{"x":"`+customSecret+`"}`)); err != nil || len(findings) == 0 {
		t.Fatalf("expected ScanJSON RawMessage leak finding, err=%v findings=%v", err, findings)
	}
	if findings, err := scanner.ScanJSON("str", `{"x":"`+customSecret+`"}`); err != nil || len(findings) == 0 {
		t.Fatalf("expected ScanJSON string leak finding, err=%v findings=%v", err, findings)
	}
	if _, err := scanner.ScanJSON("bad", make(chan int)); err == nil {
		t.Fatalf("expected ScanJSON marshal error on chan")
	}

	// ScanReader & Check error redaction.
	rFindings, err := scanner.ScanReader("reader", strings.NewReader("prefix "+customSecret+" suffix"))
	if err != nil || len(rFindings) == 0 {
		t.Fatalf("expected ScanReader finding, err=%v findings=%v", err, rFindings)
	}
	checkErr := scanner.Check("stderr", []byte("x="+DefaultCanarySecret()))
	if checkErr == nil || strings.Contains(checkErr.Error(), DefaultCanarySecret()) {
		t.Fatalf("expected redacted non-nil Check error, got %v", checkErr)
	}

	// ScanDir & AssertCleanDir detect temp file leak.
	dirtyFile := filepath.Join(cleanDir, "nested", "trace.json")
	if err := os.MkdirAll(filepath.Dir(dirtyFile), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(dirtyFile, []byte(base64.RawStdEncoding.EncodeToString([]byte(DefaultCanarySecret()))), 0o600); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}
	spy := &spyTB{TB: t}
	_, _ = callExpectingFatalf(func() {
		scanner.AssertCleanDir(spy, cleanDir)
	})
	if !spy.failed || strings.Contains(spy.msg, DefaultCanarySecret()) {
		t.Fatalf("expected redacted AssertCleanDir failure, got failed=%v msg=%q", spy.failed, spy.msg)
	}

	// Non-existent dir/file and AssertClean failure paths.
	if _, err := scanner.ScanDir(filepath.Join(cleanDir, "does-not-exist")); err == nil {
		t.Fatalf("expected ScanDir error on missing dir")
	}
	if _, err := scanner.ScanFile(filepath.Join(cleanDir, "missing.log")); err == nil {
		t.Fatalf("expected ScanFile error on missing file")
	}
	spyBytes := &spyTB{TB: t}
	_, _ = callExpectingFatalf(func() {
		scanner.AssertClean(spyBytes, NamedData{Source: "bad-bytes", Data: []byte(DefaultCanarySecret())})
	})
	if !spyBytes.failed || strings.Contains(spyBytes.msg, DefaultCanarySecret()) {
		t.Fatalf("expected redacted AssertClean failure, got failed=%v msg=%q", spyBytes.failed, spyBytes.msg)
	}

	// Concurrency-safe SafeBuffer test under -race.
	buf := NewSafeBuffer()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _ = fmt.Fprintf(buf, "worker=%d ok\n", id)
			_ = buf.String()
		}(i)
	}
	wg.Wait()
	scanner.AssertCleanBuffer(t, "concurrent-buf", buf)
	buf.Reset()
	if len(buf.Bytes()) != 0 {
		t.Fatalf("expected empty buffer after Reset")
	}

	// Custom NewScanner with empty args still covers explicit slice branch.
	customScanner := NewScanner(customSecret)
	if len(customScanner.ScanString("custom", customSecret)) == 0 {
		t.Fatalf("expected custom secret match")
	}

	// ScanReader error path.
	if _, err := scanner.ScanReader("err-reader", failingReader{}); err == nil {
		t.Fatalf("expected ScanReader error on failing reader")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("injected read error")
}
