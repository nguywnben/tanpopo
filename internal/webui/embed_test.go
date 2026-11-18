// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package webui

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHandlerServesConsoleAndHistoryRoutesWithSecurityHeaders(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/catalog/providers"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Tanpopo Console") {
			t.Fatalf("GET %s: status=%d body missing console", path, w.Code)
		}
		for header, want := range map[string]string{
			"Content-Security-Policy": "frame-ancestors 'none'",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
		} {
			if got := w.Header().Get(header); !strings.Contains(got, want) {
				t.Errorf("GET %s %s=%q, want contains %q", path, header, got, want)
			}
		}
	}
}

func TestHandlerServesGzipHashedAssetsAndImmutableCache(t *testing.T) {
	var asset string
	if err := fs.WalkDir(BundleFS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(name, "assets/") && strings.HasSuffix(name, ".js") && hashedAsset.MatchString(name) {
			asset = "/" + name
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if asset == "" {
		t.Fatal("no hashed JS asset in embedded bundle")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", asset, nil)
	r.Header.Set("Accept-Encoding", "gzip")
	Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding=%q", w.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("Cache-Control=%q", w.Header().Get("Cache-Control"))
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, zr); err != nil {
		t.Fatalf("invalid gzip stream: %v", err)
	}
	_ = zr.Close()
}

func TestHandlerGzipNegotiationHonorsQualityAndHead(t *testing.T) {
	asset := "/assets/index-BuV-_Co6.js"
	original, err := fs.ReadFile(BundleFS(), strings.TrimPrefix(asset, "/"))
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := fs.ReadFile(BundleFS(), strings.TrimPrefix(asset, "/")+".gz")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		quality, encoding string
		length            int
	}{
		{"gzip;q=0", "", len(original)},
		{"gzip;q=0.5", "gzip", len(compressed)},
		{"gzip;q=1", "gzip", len(compressed)},
	} {
		r := httptest.NewRequest("HEAD", asset, nil)
		r.Header.Set("Accept-Encoding", test.quality)
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		if got := w.Header().Get("Content-Encoding"); got != test.encoding {
			t.Errorf("HEAD Accept-Encoding %q got Content-Encoding %q", test.quality, got)
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("HEAD Accept-Encoding %q Vary=%q", test.quality, w.Header().Get("Vary"))
		}
		if got := w.Header().Get("Content-Length"); got != strconv.Itoa(test.length) {
			t.Errorf("HEAD Accept-Encoding %q Content-Length=%q, want %d", test.quality, got, test.length)
		}
		if w.Body.Len() != 0 {
			t.Errorf("HEAD wrote %d bytes", w.Body.Len())
		}
	}
}

func TestHandlerDoesNotFallbackForAPIOrMissingAsset(t *testing.T) {
	for _, path := range []string{"/v1/models", "/admin/v1/setup", "/assets/missing.js"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Errorf("GET %s status=%d, want 404", path, w.Code)
		}
	}
}

func TestHandlerRejectsMutationMethods(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("status=%d Allow=%q", w.Code, w.Header().Get("Allow"))
	}
}
