// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package blob_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if addrs, ok := r[strings.ToLower(host)]; ok {
		return addrs, nil
	}
	return nil, errors.New("not found")
}

func TestOffloadAndSpliceMultimodalChatAndResponses(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	// 1. Small inline payload <= 64 KiB stays inline (zero HostBlob entries).
	smallReq := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`)
	smallRes, err := blob.OffloadMultimodalRequest(context.Background(), "req_small", smallReq, "gpt-4o-up", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{})
	if err != nil {
		t.Fatalf("small offload failed: %v", err)
	}
	defer smallRes.Scrub()
	if len(smallRes.BlobRefs) != 0 || len(smallRes.HostBlobs) != 0 {
		t.Fatalf("expected inline payload <=64KiB not to offload, got %d blobs", len(smallRes.BlobRefs))
	}

	// 2. Large (>64 KiB) base64 image in Chat completions + audio in Responses input.
	bigImage := "data:image/png;base64," + strings.Repeat("QUFB", 25000) // ~100 KiB
	bigAudio := strings.Repeat("QkJC", 22000)                           // ~88 KiB
	respBody, err := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_image", "url": bigImage},
					map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": bigAudio, "format": "wav"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := blob.OffloadMultimodalRequest(context.Background(), "req_big_1", respBody, "gpt-4o-up", true, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{})
	if err != nil {
		t.Fatalf("large offload failed: %v", err)
	}
	if len(res.BlobRefs) != 2 || len(res.HostBlobs) != 2 {
		t.Fatalf("expected 2 offloaded blobs, got refs=%d map=%d", len(res.BlobRefs), len(res.HostBlobs))
	}
	if len(res.NormalizedJSON) > 1024 {
		t.Fatalf("expected lightweight NormalizedJSON carrying blob_ref handles, got %d bytes", len(res.NormalizedJSON))
	}
	if !strings.Contains(string(res.NormalizedJSON), "blob_ref://req_big_1-") {
		t.Fatalf("expected blob_ref:// handle inside NormalizedJSON: %s", string(res.NormalizedJSON))
	}
	if !strings.Contains(string(res.NormalizedJSON), `"stream":true`) {
		t.Fatalf("expected stream=true normalized inside JSON: %s", string(res.NormalizedJSON))
	}

	// Verify SpliceHostBlobs restores original binary strings verbatim.
	spliced, err := blob.SpliceHostBlobs(res.NormalizedJSON, res.HostBlobs, blob.DefaultMaxBlobBytes)
	if err != nil {
		t.Fatalf("SpliceHostBlobs failed: %v", err)
	}
	if !bytes.Contains(spliced, []byte(bigImage)) || !bytes.Contains(spliced, []byte(bigAudio)) {
		t.Fatalf("expected spliced payload to contain original big image & audio payloads")
	}

	// Hold reference to underlying blob slice and verify Scrub() zeroizes it in place.
	var firstBlob []byte
	for _, b := range res.HostBlobs {
		firstBlob = b
		break
	}
	res.Scrub()
	if len(res.HostBlobs) != 0 {
		t.Fatalf("expected HostBlobs map emptied after Scrub")
	}
	for i, bt := range firstBlob {
		if bt != 0 {
			t.Fatalf("expected Scrub() to zeroize host blob buffer at index %d, got %d", i, bt)
		}
	}
}

func TestRejectClientInjectedBlobRef(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	req := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"blob_ref://forged/0"}}]}]}`)
	_, err := blob.OffloadMultimodalRequest(context.Background(), "req_forge", req, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{})
	var bErr *blob.Error
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusBadRequest || !errors.Is(err, blob.ErrInvalidPayload) {
		t.Fatalf("expected 400 ErrInvalidPayload on forged blob_ref://, got %v", err)
	}
}

func TestRemoteMediaURLDefaultDisabledAndSSRFBlocked(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	metaReq := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://169.254.169.254/latest/meta-data/"}}]}]}`)

	// 1. Default policy (Enabled: false) -> 400 remote_media_url_disabled
	_, err := blob.OffloadMultimodalRequest(context.Background(), "req_neg2_def", metaReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{})
	var bErr *blob.Error
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusBadRequest || bErr.Code != "remote_media_url_disabled" {
		t.Fatalf("expected 400 remote_media_url_disabled, got %+v (%v)", bErr, err)
	}

	// 2. Policy enabled without private IP allowance -> 403 ssrf_blocked_ip
	_, err = blob.OffloadMultimodalRequest(context.Background(), "req_neg2_ssrf", metaReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{
		Enabled:      true,
		AllowedHosts: []string{"169.254.169.254"},
		AllowedPorts: []int{80},
	})
	bErr = nil
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusForbidden || bErr.Code != "ssrf_blocked_ip" {
		t.Fatalf("expected 403 ssrf_blocked_ip, got %+v (%v)", bErr, err)
	}

	// 3. Policy enabled hitting a valid allowed loopback media endpoint splices fetched bytes as data URI.
	pngPayload := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngPayload)
	}))
	defer srv.Close()

	port := 0
	if _, err := json.Marshal(srv.URL); err == nil {
		_, _ = strings.CutPrefix(srv.URL, "http://127.0.0.1:")
	}
	uPortIdx := strings.LastIndex(srv.URL, ":")
	for i := uPortIdx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}

	// 3b. Cleartext http:// MUST fail closed by default when AllowInsecureHTTP is false.
	remoteReq := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://media.internal.test:` + srv.URL[uPortIdx+1:] + `/pic.png"}}]}]}`)
	_, err = blob.OffloadMultimodalRequest(context.Background(), "req_http_tls_default", remoteReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{
		Enabled:        true,
		AllowedHosts:   []string{"media.internal.test"},
		AllowedPorts:   []int{port},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.1/32"},
		Resolver: staticResolver{
			"media.internal.test": {netip.MustParseAddr("127.0.0.1")},
		},
	})
	bErr = nil
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected cleartext http:// denied by default TLS requirement, got %+v (%v)", bErr, err)
	}

	res, err := blob.OffloadMultimodalRequest(context.Background(), "req_remote_ok", remoteReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{
		Enabled:           true,
		AllowedHosts:      []string{"media.internal.test"},
		AllowedPorts:      []int{port},
		AllowPrivateIP:    true,
		AllowedCIDRs:      []string{"127.0.0.1/32"},
		AllowInsecureHTTP: true,
		Resolver: staticResolver{
			"media.internal.test": {netip.MustParseAddr("127.0.0.1")},
		},
	})
	if err != nil {
		t.Fatalf("expected allowed policy-guarded remote media fetch to succeed, got %v", err)
	}
	defer res.Scrub()
	if len(res.HostBlobs) != 1 {
		t.Fatalf("expected 1 fetched host blob, got %d", len(res.HostBlobs))
	}
	spliced, err := blob.SpliceHostBlobs(res.NormalizedJSON, res.HostBlobs, blob.DefaultMaxBlobBytes)
	if err != nil {
		t.Fatalf("SpliceHostBlobs failed: %v", err)
	}
	expectedDataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngPayload)
	if !bytes.Contains(spliced, []byte(expectedDataURI)) {
		t.Fatalf("expected spliced JSON to carry fetched data URI %q, got %s", expectedDataURI, string(spliced))
	}

	// 4. Policy enabled with empty AllowedHosts MUST fail closed even for public IP/hostnames.
	_, err = blob.OffloadMultimodalRequest(context.Background(), "req_empty_allowlist", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://93.184.216.34/pic.png"}}]}]}`), "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{
		Enabled: true,
	})
	bErr = nil
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusForbidden || bErr.Code != "ssrf_blocked_ip" {
		t.Fatalf("expected fail-closed 403 ssrf_blocked_ip when AllowedHosts is empty, got %+v (%v)", bErr, err)
	}

	// 5. Port-scanning defense: unset AllowedPorts defaults strictly to 80/443 (non-80/443 ports like :6379 blocked),
	// and AllowAnyPublicHost=true with AllowedPorts=[443] must not bypass port checks via u.Host.
	for _, tc := range []struct {
		name string
		url  string
		pol  blob.RemoteMediaPolicy
	}{
		{
			name: "unset_allowed_ports_blocks_non_80_443",
			url:  "http://media.internal.test:6379/ping",
			pol: blob.RemoteMediaPolicy{
				Enabled:           true,
				AllowedHosts:      []string{"media.internal.test"},
				AllowPrivateIP:    true,
				AllowInsecureHTTP: true,
				AllowedCIDRs:      []string{"127.0.0.1/32"},
				Resolver:          staticResolver{"media.internal.test": {netip.MustParseAddr("127.0.0.1")}},
			},
		},
		{
			name: "allow_any_public_host_honors_allowed_ports",
			url:  "http://media.internal.test:6379/ping",
			pol: blob.RemoteMediaPolicy{
				Enabled:            true,
				AllowAnyPublicHost: true,
				AllowedPorts:       []int{443},
				AllowPrivateIP:     true,
				AllowInsecureHTTP:  true,
				AllowedCIDRs:       []string{"127.0.0.1/32"},
				Resolver:           staticResolver{"media.internal.test": {netip.MustParseAddr("127.0.0.1")}},
			},
		},
	} {
		req := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"` + tc.url + `"}}]}]}`)
		_, err = blob.OffloadMultimodalRequest(context.Background(), "req_port_"+tc.name, req, "gpt-4o", false, blob.DefaultMaxBlobBytes, tc.pol)
		bErr = nil
		if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusForbidden || bErr.Code != "ssrf_blocked_ip" {
			t.Fatalf("%s: expected 403 ssrf_blocked_ip, got %+v (%v)", tc.name, bErr, err)
		}
	}

	// 6. Reject active-content image/svg+xml responses from remote media endpoints.
	svgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	}))
	defer svgSrv.Close()
	svgPort := 0
	svgPortIdx := strings.LastIndex(svgSrv.URL, ":")
	for i := svgPortIdx + 1; i < len(svgSrv.URL); i++ {
		svgPort = svgPort*10 + int(svgSrv.URL[i]-'0')
	}
	_, err = blob.OffloadMultimodalRequest(context.Background(), "req_svg_deny", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://media.internal.test:`+svgSrv.URL[svgPortIdx+1:]+`/x.svg"}}]}]}`), "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{
		Enabled:           true,
		AllowedHosts:      []string{"media.internal.test"},
		AllowedPorts:      []int{svgPort},
		AllowPrivateIP:    true,
		AllowInsecureHTTP: true,
		AllowedCIDRs:      []string{"127.0.0.1/32"},
		Resolver:          staticResolver{"media.internal.test": {netip.MustParseAddr("127.0.0.1")}},
	})
	bErr = nil
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("expected 400 rejection for image/svg+xml remote media, got %+v (%v)", bErr, err)
	}

	// 7. Reject requests exceeding MaxRemoteMediaURLsPerRequest fan-out limit.
	var parts []string
	for i := 0; i < blob.MaxRemoteMediaURLsPerRequest+1; i++ {
		parts = append(parts, `{"type":"image_url","image_url":{"url":"http://media.internal.test:`+srv.URL[uPortIdx+1:]+`/pic.png"}}`)
	}
	fanoutReq := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[` + strings.Join(parts, ",") + `]}]}`)
	_, err = blob.OffloadMultimodalRequest(context.Background(), "req_fanout_limit", fanoutReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{
		Enabled:           true,
		AllowedHosts:      []string{"media.internal.test"},
		AllowedPorts:      []int{port},
		AllowPrivateIP:    true,
		AllowInsecureHTTP: true,
		AllowedCIDRs:      []string{"127.0.0.1/32"},
		Resolver:          staticResolver{"media.internal.test": {netip.MustParseAddr("127.0.0.1")}},
	})
	bErr = nil
	if !errors.As(err, &bErr) || bErr.HTTPStatus != http.StatusBadRequest || bErr.Code != "remote_media_url_limit_exceeded" {
		t.Fatalf("expected 400 remote_media_url_limit_exceeded, got %+v (%v)", bErr, err)
	}
}

func TestStrictJSONAndEndpointFieldIsolation(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	// 1. Duplicate/case-colliding JSON keys must fail closed (prevents parser desync).
	for _, bad := range [][]byte{
		[]byte(`{"model":"gpt-4o","Tools":null,"tools":[{"type":"function"}]}`),
		[]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://169.254.169.254/"}}],"Content":"hi"}]}`),
		[]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}{"extra":1}`),
	} {
		if _, err := blob.DecodeStrictJSONObject(bad); err == nil {
			t.Fatalf("expected DecodeStrictJSONObject to reject %s", string(bad))
		}
		if _, err := blob.OffloadEndpointMultimodalRequest(context.Background(), "req_dup", bad, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "messages"); err == nil {
			t.Fatalf("expected OffloadEndpointMultimodalRequest to reject %s", string(bad))
		}
	}

	// 2. input_image inspects part['url'] even if part['image_url'] is present with non-string value.
	trickReq := []byte(`{"model":"gpt-4o","input":[{"role":"user","content":[{"type":"input_image","image_url":123,"url":"http://169.254.169.254/latest/meta-data/"}]}]}`)
	_, err := blob.OffloadEndpointMultimodalRequest(context.Background(), "req_trick", trickReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "input")
	var bErr *blob.Error
	if !errors.As(err, &bErr) || bErr.Code != "remote_media_url_disabled" {
		t.Fatalf("expected remote_media_url_disabled on input_image with image_url=123, got %+v (%v)", bErr, err)
	}

	// 3. Endpoint field 'none' (embeddings) skips walking extraneous 'messages' or 'input' arrays.
	embExtra := []byte(`{"model":"text-embedding-3-small","input":"hello","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://169.254.169.254/"}}]}]}`)
	res, err := blob.OffloadEndpointMultimodalRequest(context.Background(), "req_emb_none", embExtra, "text-embedding-3-small", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "none")
	if err != nil {
		t.Fatalf("expected endpointRootField='none' to skip extraneous 'messages' media fetch, got %v", err)
	}
	defer res.Scrub()
	if len(res.HostBlobs) != 0 {
		t.Fatalf("expected 0 offloaded blobs for endpointRootField='none', got %d", len(res.HostBlobs))
	}

	// 4. All audio, file, input_file, and document URL shapes enforce remote media / SSRF policy.
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"audio_url", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"audio","audio":{"url":"http://169.254.169.254/a.wav"}}]}]}`)},
		{"file_url", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"file","file":{"url":"http://169.254.169.254/doc.pdf"}}]}]}`)},
		{"input_file_url", []byte(`{"model":"gpt-4o","input":[{"role":"user","content":[{"type":"input_file","url":"http://169.254.169.254/doc.pdf"}]}]}`)},
		{"document_url", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"document","document":{"url":"http://169.254.169.254/doc.pdf"}}]}]}`)},
		{"document_source_url", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"document","source":{"type":"url","url":"http://169.254.169.254/doc.pdf"}}]}]}`)},
		{"image_source_url", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"http://169.254.169.254/pic.png"}}]}]}`)},
		{"nested_image_source_url", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image","image":{"source":{"type":"url","url":"http://169.254.169.254/pic.png"}}}]}]}`)},
	} {
		_, err := blob.OffloadEndpointMultimodalRequest(context.Background(), "req_"+tc.name, tc.body, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "")
		var mmErr *blob.Error
		if !errors.As(err, &mmErr) || mmErr.Code != "remote_media_url_disabled" {
			t.Fatalf("%s: expected remote_media_url_disabled, got %+v (%v)", tc.name, mmErr, err)
		}
	}

	// 5. Large (>64 KiB) image.source.data offloads to Host blob_ref:// cleanly.
	bigImagePayload := strings.Repeat("A", blob.MaxInlineBlobBytes+128)
	imgSrcReq := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"` + bigImagePayload + `"}}]}]}`)
	imgRes, err := blob.OffloadEndpointMultimodalRequest(context.Background(), "req_img_src_big", imgSrcReq, "gpt-4o", false, blob.DefaultMaxBlobBytes, blob.RemoteMediaPolicy{}, "messages")
	if err != nil {
		t.Fatalf("expected large image.source.data offload to succeed, got %v", err)
	}
	defer imgRes.Scrub()
	if len(imgRes.BlobRefs) != 1 || bytes.Contains(imgRes.NormalizedJSON, []byte(bigImagePayload)) {
		t.Fatalf("expected large image.source.data replaced by blob_ref:// handle, refs=%d", len(imgRes.BlobRefs))
	}
}

