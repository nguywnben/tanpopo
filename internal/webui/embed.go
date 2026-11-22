// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package webui embeds and serves the production console bundle.
package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
)

//go:embed all:dist
var bundle embed.FS

var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.`)

// Handler serves the embedded Console and allows SPA history routes to resolve
// to index.html. API paths are intentionally left to the caller's API mux.
func Handler() http.Handler {
	assets, err := fs.Sub(bundle, "dist")
	if err != nil {
		panic("webui: embedded dist directory missing: " + err.Error())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; font-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			loc := i18n.ResolveAcceptLanguage(r.Header.Get("Accept-Language"))
			w.Header().Set("Allow", "GET, HEAD")
			w.Header().Set("Content-Language", string(loc))
			http.Error(w, i18n.T(loc, "webui.method_not_allowed"), http.StatusMethodNotAllowed)
			return
		}
		cleaned := path.Clean("/" + r.URL.Path)
		if isReservedBackendRequest(cleaned, r) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(cleaned, "/")
		if name == "." || name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		data, readErr := fs.ReadFile(assets, name)
		if readErr != nil {
			// Only extensionless UI paths get history fallback; missing assets stay 404.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			data, readErr = fs.ReadFile(assets, name)
			if readErr != nil {
				loc := i18n.ResolveAcceptLanguage(r.Header.Get("Accept-Language"))
				w.Header().Set("Content-Language", string(loc))
				http.Error(w, i18n.T(loc, "webui.bundle_unavailable"), http.StatusInternalServerError)
				return
			}
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		w.Header().Set("Content-Type", contentType)
		if hashedAsset.MatchString(path.Base(name)) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		// The checked-in bundle includes generated gzip siblings for static assets.
		if name != "index.html" {
			if compressed, err := fs.ReadFile(assets, name+".gz"); err == nil {
				w.Header().Add("Vary", "Accept-Encoding")
				if acceptsGzip(r.Header.Values("Accept-Encoding")) {
					w.Header().Set("Content-Encoding", "gzip")
					w.Header().Set("Content-Length", itoa(len(compressed)))
					if r.Method == http.MethodHead {
						return
					}
					_, _ = w.Write(compressed)
					return
				}
			}
		}
		w.Header().Set("Content-Length", itoa(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	})
}

func acceptsGzip(values []string) bool {
	for _, value := range values {
		for _, encoding := range strings.Split(value, ",") {
			parts := strings.SplitN(strings.TrimSpace(encoding), ";", 2)
			if strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
				if len(parts) == 1 {
					return true
				}
				for _, param := range strings.Split(parts[1], ";") {
					name, rawValue, ok := strings.Cut(strings.TrimSpace(param), "=")
					if ok && strings.EqualFold(strings.TrimSpace(name), "q") {
						quality, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
						return err == nil && quality > 0 && quality <= 1
					}
				}
				return true
			}
		}
	}
	return false
}

// BundleFS returns a read-only view for startup checks and diagnostics.
func BundleFS() fs.FS {
	root, _ := fs.Sub(bundle, "dist")
	return root
}

func isReservedBackendRequest(p string, r *http.Request) bool {
	// MCP Streamable HTTP transport lives strictly at /mcp; Console SPA uses /mcp/servers and /mcp/tools.
	if p == "/mcp" {
		return true
	}
	// Console SPA renders /a2a/agents on browser HTML navigation without API Authorization header.
	if p == "/a2a/agents" && r != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		strings.TrimSpace(r.Header.Get("Authorization")) == "" &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		return false
	}
	for _, prefix := range []string{"/v1", "/admin/v1", "/native", "/a2a", "/.well-known"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
