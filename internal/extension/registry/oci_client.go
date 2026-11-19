// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

var (
	ErrInvalidOCIReference = errors.New("registry: invalid OCI reference")
	ErrRegistryAuthScope   = errors.New("registry: credentials do not match registry host")
	ErrArtifactTooLarge    = errors.New("registry: OCI artifact exceeds size limit")
)

type RegistryCredentials struct {
	RegistryHost string
	Username     []byte
	Password     []byte
}

// Zeroize clears the credential buffers after a registry operation.
func (c *RegistryCredentials) Zeroize() {
	if c == nil {
		return
	}
	clear(c.Username)
	clear(c.Password)
	c.Username = nil
	c.Password = nil
}

type OCIClient struct {
	httpClient *http.Client
	maxBytes   int64
}

// Pull fetches an OCI blob by immutable digest. Credentials are attached only
// when their recorded registry authority exactly matches the reference.
func (c *OCIClient) Pull(ctx context.Context, rawRef string, credentials *RegistryCredentials) ([]byte, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("%w: guarded HTTP client is required", ErrInvalidOCIReference)
	}
	registryHost, repository, digest, err := parseOCIReference(rawRef)
	if err != nil {
		return nil, err
	}
	if credentials != nil && (!strings.EqualFold(strings.TrimSpace(credentials.RegistryHost), registryHost) || len(credentials.Username) == 0 || len(credentials.Password) == 0) {
		return nil, ErrRegistryAuthScope
	}
	endpoint := "https://" + registryHost + "/v2/" + repository + "/blobs/sha256:" + digest
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot construct OCI request", ErrInvalidOCIReference)
	}
	request.Header.Set("Accept", "application/octet-stream")
	if credentials != nil {
		request.SetBasicAuth(string(credentials.Username), string(credentials.Password))
		defer request.Header.Del("Authorization")
	}
	guarded := *c.httpClient
	guarded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := guarded.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry: OCI registry returned HTTP %d", response.StatusCode)
	}
	limit := c.maxBytes
	if limit <= 0 || limit > installer.MaxArchiveCompressedBytes {
		limit = installer.MaxArchiveCompressedBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		clear(body)
		return nil, ErrArtifactTooLarge
	}
	actual := sha256.Sum256(body)
	if err := VerifyDigest(digest, actual); err != nil {
		clear(body)
		return nil, err
	}
	return body, nil
}

func parseOCIReference(raw string) (registryHost, repository, digest string, err error) {
	u, parseErr := url.Parse(strings.TrimSpace(raw))
	if parseErr != nil || u.Scheme != "oci" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return "", "", "", ErrInvalidOCIReference
	}
	registryHost = strings.ToLower(u.Host)
	if u.Hostname() == "" || strings.ContainsAny(registryHost, "\\%@") {
		return "", "", "", ErrInvalidOCIReference
	}
	if strings.ContainsAny(u.Path, "\\%") {
		return "", "", "", ErrInvalidOCIReference
	}
	path := strings.TrimPrefix(u.EscapedPath(), "/")
	idx := strings.LastIndex(path, "@sha256:")
	if idx <= 0 || idx+len("@sha256:") >= len(path) {
		return "", "", "", fmt.Errorf("%w: immutable sha256 digest is required", ErrInvalidOCIReference)
	}
	repository = path[:idx]
	digest = path[idx+len("@sha256:"):]
	if strings.Contains(repository, "//") || strings.HasPrefix(repository, ".") || strings.Contains(repository, "/../") || strings.HasSuffix(repository, "/..") || strings.Contains(repository, "/./") {
		return "", "", "", ErrInvalidOCIReference
	}
	if decodeDigest(digest) != nil {
		return "", "", "", fmt.Errorf("%w: malformed sha256 digest", ErrInvalidOCIReference)
	}
	return registryHost, repository, digest, nil
}

// Digest returns a canonical OCI sha256 digest string for a downloaded blob.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
