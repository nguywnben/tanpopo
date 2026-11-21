// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
)

// BearerTokenSource supplies OAuth2 Bearer access tokens (e.g., GCP Workload Identity / ADC).
type BearerTokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// StaticBearerToken implements BearerTokenSource with a fixed token string.
type StaticBearerToken string

// AccessToken returns the configured static bearer token.
func (s StaticBearerToken) AccessToken(_ context.Context) (string, error) {
	if strings.TrimSpace(string(s)) == "" {
		return "", fmt.Errorf("%w: empty bearer token", vault.ErrInvalidInput)
	}
	return strings.TrimSpace(string(s)), nil
}

// GCPKMSConfig configures a GCP Cloud KMS KEKProvider & ProjectRootKeyProvider.
type GCPKMSConfig struct {
	EndpointURL    string
	ActiveKeyName  string // projects/{p}/locations/{l}/keyRings/{r}/cryptoKeys/{k}
	TokenSource    BearerTokenSource
	ProjectKeys    map[string]string
	HTTPClient     *http.Client
	EgressPolicy   *egress.Policy
	HostResolver   egress.HostResolver
	RequestTimeout time.Duration
}

// GCPKMSProvider wraps/unwraps DEKs via GCP Cloud KMS REST v1 encrypt/decrypt with AAD binding.
type GCPKMSProvider struct {
	endpoint      *url.URL
	activeKeyName string
	tokens        BearerTokenSource
	client        *http.Client
	mu            sync.RWMutex
	projectKeys   map[string]string
}

// NewGCPKMSProvider constructs a fail-closed GCP Cloud KMS provider routed through SSRFGuardDialer.
func NewGCPKMSProvider(cfg GCPKMSConfig) (*GCPKMSProvider, error) {
	rawEP := strings.TrimSpace(cfg.EndpointURL)
	if rawEP == "" {
		rawEP = "https://cloudkms.googleapis.com"
	}
	u, err := url.Parse(rawEP)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: kms/gcp invalid endpoint_url %q", vault.ErrInvalidInput, rawEP)
	}
	if strings.TrimSpace(cfg.ActiveKeyName) == "" {
		return nil, fmt.Errorf("%w: kms/gcp active_key_name required", vault.ErrInvalidInput)
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("%w: kms/gcp token_source required", vault.ErrInvalidInput)
	}
	client, err := buildKMSHTTPClient(u, cfg.HTTPClient, cfg.EgressPolicy, cfg.HostResolver, nil, cfg.RequestTimeout)
	if err != nil {
		return nil, err
	}
	pk := make(map[string]string, len(cfg.ProjectKeys))
	for k, v := range cfg.ProjectKeys {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			pk[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return &GCPKMSProvider{
		endpoint:      u,
		activeKeyName: strings.Trim(strings.TrimSpace(cfg.ActiveKeyName), "/"),
		tokens:        cfg.TokenSource,
		client:        client,
		projectKeys:   pk,
	}, nil
}

// ActiveKeyID returns the configured primary GCP Cloud KMS CryptoKey resource path.
func (p *GCPKMSProvider) ActiveKeyID(_ context.Context) (string, error) {
	return p.activeKeyName, nil
}

// SetProjectKey registers or removes a dedicated GCP Cloud KMS key resource for (orgID, projectID).
func (p *GCPKMSProvider) SetProjectKey(orgID, projectID, cryptoKeyName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(cryptoKeyName) == "" {
		delete(p.projectKeys, projectMapKey(orgID, projectID))
		return
	}
	p.projectKeys[projectMapKey(orgID, projectID)] = strings.Trim(strings.TrimSpace(cryptoKeyName), "/")
}

// ProjectKeyURI returns any dedicated GCP KMS key resource path configured for (orgID, projectID).
func (p *GCPKMSProvider) ProjectKeyURI(_ context.Context, orgID, projectID string) (string, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ref, ok := p.projectKeys[projectMapKey(orgID, projectID)]
	return ref, ok && ref != "", nil
}

func gcpOrgAAD(orgID string, dekVersion int64) []byte {
	return []byte("tanpopo:v1:kms:org:" + orgID + ":" + strconv.FormatInt(dekVersion, 10))
}

func gcpProjectAAD(orgID, projectID string, dekVersion int64) []byte {
	return []byte("tanpopo:v1:kms:proj:" + orgID + ":" + projectID + ":" + strconv.FormatInt(dekVersion, 10))
}

// WrapDEK encrypts an Org DEK via GCP KMS :encrypt bound to org AAD.
func (p *GCPKMSProvider) WrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, plaintextDEK []byte) ([]byte, []byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || strings.TrimSpace(orgID) == "" || dekVersion < 1 || len(plaintextDEK) != vault.KeySizeBytes {
		return nil, nil, fmt.Errorf("%w: invalid WrapDEK arguments", vault.ErrInvalidInput)
	}
	ct, err := p.encrypt(ctx, kekKeyID, plaintextDEK, gcpOrgAAD(orgID, dekVersion))
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, vault.NonceSizeBytes)
	nonce[0] = 0x01
	return nonce, ct, nil
}

// UnwrapDEK decrypts an Org DEK via GCP KMS :decrypt bound to org AAD.
func (p *GCPKMSProvider) UnwrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, _, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || strings.TrimSpace(orgID) == "" || dekVersion < 1 || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapDEK arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kekKeyID, ciphertext, gcpOrgAAD(orgID, dekVersion))
}

// WrapArchiveKey encrypts a backup archive key via GCP KMS :encrypt.
func (p *GCPKMSProvider) WrapArchiveKey(ctx context.Context, kekKeyID string, plaintextKey []byte) ([]byte, []byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || len(plaintextKey) != vault.KeySizeBytes {
		return nil, nil, fmt.Errorf("%w: invalid WrapArchiveKey arguments", vault.ErrInvalidInput)
	}
	ct, err := p.encrypt(ctx, kekKeyID, plaintextKey, []byte("tanpopo:v1:kms:archive"))
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, vault.NonceSizeBytes)
	nonce[0] = 0x01
	return nonce, ct, nil
}

// UnwrapArchiveKey decrypts a backup archive key via GCP KMS :decrypt.
func (p *GCPKMSProvider) UnwrapArchiveKey(ctx context.Context, kekKeyID string, _, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapArchiveKey arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kekKeyID, ciphertext, []byte("tanpopo:v1:kms:archive"))
}

// WrapProjectDEK encrypts a Project DEK directly under a dedicated GCP KMS CryptoKey with project AAD.
func (p *GCPKMSProvider) WrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, plaintextDEK []byte) ([]byte, error) {
	if strings.TrimSpace(kmsKeyRef) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || dekVersion < 1 || len(plaintextDEK) != vault.KeySizeBytes {
		return nil, fmt.Errorf("%w: invalid WrapProjectDEK arguments", vault.ErrInvalidInput)
	}
	return p.encrypt(ctx, kmsKeyRef, plaintextDEK, gcpProjectAAD(orgID, projectID, dekVersion))
}

// UnwrapProjectDEK decrypts a Project DEK directly under its dedicated GCP KMS CryptoKey.
func (p *GCPKMSProvider) UnwrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, wrappedBlob []byte) ([]byte, error) {
	if strings.TrimSpace(kmsKeyRef) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || dekVersion < 1 || len(wrappedBlob) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapProjectDEK arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kmsKeyRef, wrappedBlob, gcpProjectAAD(orgID, projectID, dekVersion))
}

func (p *GCPKMSProvider) encrypt(ctx context.Context, keyName string, plaintext, aad []byte) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Plaintext                   string `json:"plaintext"`
		AdditionalAuthenticatedData string `json:"additionalAuthenticatedData"`
	}{
		Plaintext:                   base64.StdEncoding.EncodeToString(plaintext),
		AdditionalAuthenticatedData: base64.StdEncoding.EncodeToString(aad),
	})
	if err != nil {
		return nil, err
	}
	defer clear(payload)

	respBytes, err := p.doGCPRequest(ctx, strings.Trim(keyName, "/")+"/encrypt", payload)
	if err != nil {
		// GCP Cloud KMS REST uses colon action ":encrypt".
		if strings.Contains(err.Error(), "COLON_RETRY") {
			return nil, err
		}
		return nil, err
	}
	var resp struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("kms/gcp: decode encrypt response: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(resp.Ciphertext)
	if err != nil || len(ct) == 0 {
		return nil, fmt.Errorf("kms/gcp: invalid ciphertext returned")
	}
	return ct, nil
}

func (p *GCPKMSProvider) decrypt(ctx context.Context, keyName string, ciphertext, aad []byte) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Ciphertext                  string `json:"ciphertext"`
		AdditionalAuthenticatedData string `json:"additionalAuthenticatedData"`
	}{
		Ciphertext:                  base64.StdEncoding.EncodeToString(ciphertext),
		AdditionalAuthenticatedData: base64.StdEncoding.EncodeToString(aad),
	})
	if err != nil {
		return nil, err
	}

	respBytes, err := p.doGCPRequest(ctx, strings.Trim(keyName, "/")+":decrypt", payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", vault.ErrDecryptionFailed, err)
	}
	defer clear(respBytes)

	var resp struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("%w: decode decrypt response: %v", vault.ErrDecryptionFailed, err)
	}
	pt, err := base64.StdEncoding.DecodeString(resp.Plaintext)
	if err != nil || len(pt) != vault.KeySizeBytes {
		clear(pt)
		return nil, fmt.Errorf("%w: invalid decrypted DEK length", vault.ErrDecryptionFailed)
	}
	return pt, nil
}

func (p *GCPKMSProvider) doGCPRequest(ctx context.Context, resourceAction string, body []byte) ([]byte, error) {
	// Normalize "/encrypt" -> ":encrypt" while keeping ":decrypt" intact.
	if strings.HasSuffix(resourceAction, "/encrypt") {
		resourceAction = strings.TrimSuffix(resourceAction, "/encrypt") + ":encrypt"
	}
	tok, err := p.tokens.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("kms/gcp: acquire access token: %w", err)
	}

	u := *p.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/" + strings.TrimLeft(resourceAction, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxKMSResponseBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kms/gcp request failed with HTTP %d", resp.StatusCode)
	}
	return respBody, nil
}
