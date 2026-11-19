// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

type fixedRegistryCredentials struct {
	host string
}

func (r fixedRegistryCredentials) CredentialsForRegistry(_ context.Context, orgID, host string) (*RegistryCredentials, error) {
	if orgID != "org_test" || host != r.host {
		return nil, ErrRegistryAuthScope
	}
	return &RegistryCredentials{RegistryHost: host, Username: []byte("robot"), Password: []byte("opaque-token")}, nil
}

func TestOCIArchiveResolverStagesOnlyVerifiedIndexedArtifact(t *testing.T) {
	body := []byte("signed package archive")
	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	ref := "oci://registry.example.com/team/adapter@sha256:" + digest
	store := &memoryIndexStore{entries: []IndexEntry{{PackageSlug: "team/adapter", Version: "1.0.0", Digest: digest, OCIURI: ref}}}
	client := &OCIClient{HTTPClient: &http.Client{Transport: registryRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "registry.example.com" || req.URL.Path != "/v2/team/adapter/blobs/sha256:"+digest {
			t.Fatalf("unexpected OCI request: %s", req.URL)
		}
		user, password, ok := req.BasicAuth()
		if !ok || user != "robot" || password != "opaque-token" {
			t.Fatal("registry credentials were not scoped and injected")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}}
	resolver := NewOCIArchiveResolver(store, fixedRegistryCredentials{host: "registry.example.com"}, nil)
	resolver.clientForRegistry = func(_ context.Context, host string) (*OCIClient, error) {
			if host != "registry.example.com" {
				t.Fatalf("guarded client requested for unexpected registry %q", host)
			}
			return client, nil
	}
	request, cleanup, err := resolver.Resolve(context.Background(), "org_test", ref, digest)
	if err != nil {
		t.Fatalf("Resolve indexed artifact: %v", err)
	}
	defer func() {
		cleanup()
		if _, err := os.Stat(request.ArchivePath); !os.IsNotExist(err) {
			t.Errorf("private archive snapshot was not removed: %v", err)
		}
	}()
	if request.Source.Kind != installer.SourceKindOCI || request.Source.URI != ref || request.Source.ExpectedSHA256 != digest {
		t.Fatalf("unexpected install provenance: %+v", request.Source)
	}
	file, err := os.ReadFile(request.ArchivePath)
	if err != nil || !bytes.Equal(file, body) {
		t.Fatalf("staged archive content=%q err=%v", file, err)
	}
	if _, _, err := resolver.Resolve(context.Background(), "org_test", ref, hex.EncodeToString(make([]byte, sha256.Size))); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	store.entries = nil
	if _, _, err := resolver.Resolve(context.Background(), "org_test", ref, digest); err != ErrOCIArtifactNotIndexed {
		t.Fatalf("artifact outside verified index must be rejected, got %v", err)
	}
}
