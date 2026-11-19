// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

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
	store := &memoryIndexStore{expiresAt: time.Now().Add(time.Hour), entries: []IndexEntry{{PackageSlug: "team/adapter", Version: "1.0.0", Digest: digest, OCIURI: ref}}}
	client := &OCIClient{httpClient: &http.Client{Transport: registryRoundTripper(func(req *http.Request) (*http.Response, error) {
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
	resolver.now = time.Now
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
	store.entries = []IndexEntry{{PackageSlug: "team/adapter", Version: "1.0.0", Digest: digest, OCIURI: ref}}
	store.expiresAt = time.Now().Add(-time.Second)
	if _, _, err := resolver.Resolve(context.Background(), "org_test", ref, digest); err != ErrFeedExpired {
		t.Fatalf("artifact from expired verified index must be rejected, got %v", err)
	}
}

func TestOCIRegistryEgressPolicyAllowsOnlyCredentialScopedPrivateCIDRs(t *testing.T) {
	publicPolicy, err := ociRegistryEgressPolicy("registry.example.com:5443", nil)
	if err != nil || publicPolicy.AllowPrivateIP || len(publicPolicy.AllowedCIDRs) != 0 || len(publicPolicy.AllowedHosts) != 1 || publicPolicy.AllowedHosts[0] != "registry.example.com" || len(publicPolicy.AllowedPorts) != 1 || publicPolicy.AllowedPorts[0] != 5443 {
		t.Fatalf("public registry policy was not exact-host, exact-port and default-deny: policy=%+v err=%v", publicPolicy, err)
	}

	credentials := &RegistryCredentials{RegistryHost: "registry.internal.example:5443", AllowedCIDRs: []string{"10.44.0.0/16"}}
	privatePolicy, err := ociRegistryEgressPolicy(credentials.RegistryHost, credentials)
	if err != nil || !privatePolicy.AllowPrivateIP || len(privatePolicy.AllowedCIDRs) != 1 || privatePolicy.AllowedCIDRs[0] != "10.44.0.0/16" || len(privatePolicy.AllowedHosts) != 1 || privatePolicy.AllowedHosts[0] != "registry.internal.example" || privatePolicy.AllowedPorts[0] != 5443 {
		t.Fatalf("private registry policy was not narrowed to its credential authority and CIDR: policy=%+v err=%v", privatePolicy, err)
	}
	if _, err := ociRegistryEgressPolicy("other.internal.example:5443", credentials); !errors.Is(err, ErrRegistryAuthScope) {
		t.Fatalf("private CIDRs escaped their exact registry authority: %v", err)
	}
}
