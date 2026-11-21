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

// VaultTransitConfig configures a HashiCorp Vault Transit KEKProvider & ProjectRootKeyProvider.
type VaultTransitConfig struct {
	Address        string
	Token          string
	Namespace      string
	Mount          string // defaults to "transit"
	ActiveKeyName  string
	ProjectKeys    map[string]string
	HTTPClient     *http.Client
	EgressPolicy   *egress.Policy
	HostResolver   egress.HostResolver
	RequestTimeout time.Duration
}

// VaultTransitProvider wraps/unwraps DEKs via HashiCorp Vault Transit encrypt/decrypt with context AAD binding.
type VaultTransitProvider struct {
	address       *url.URL
	token         string
	namespace     string
	mount         string
	activeKeyName string
	client        *http.Client
	mu            sync.RWMutex
	projectKeys   map[string]string
}

// NewVaultTransitProvider constructs a fail-closed HashiCorp Vault Transit provider routed through SSRFGuardDialer.
func NewVaultTransitProvider(cfg VaultTransitConfig) (*VaultTransitProvider, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.Address))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: kms/vault invalid address %q", vault.ErrInvalidInput, cfg.Address)
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("%w: kms/vault token required", vault.ErrInvalidInput)
	}
	if strings.TrimSpace(cfg.ActiveKeyName) == "" {
		return nil, fmt.Errorf("%w: kms/vault active_key_name required", vault.ErrInvalidInput)
	}
	mount := strings.Trim(strings.TrimSpace(cfg.Mount), "/")
	if mount == "" {
		mount = "transit"
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
	return &VaultTransitProvider{
		address:       u,
		token:         strings.TrimSpace(cfg.Token),
		namespace:     strings.TrimSpace(cfg.Namespace),
		mount:         mount,
		activeKeyName: strings.TrimSpace(cfg.ActiveKeyName),
		client:        client,
		projectKeys:   pk,
	}, nil
}

// ActiveKeyID returns the configured primary Vault Transit key name.
func (p *VaultTransitProvider) ActiveKeyID(_ context.Context) (string, error) {
	return p.activeKeyName, nil
}

// SetProjectKey registers or removes a dedicated Vault Transit key name for (orgID, projectID).
func (p *VaultTransitProvider) SetProjectKey(orgID, projectID, transitKeyName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(transitKeyName) == "" {
		delete(p.projectKeys, projectMapKey(orgID, projectID))
		return
	}
	p.projectKeys[projectMapKey(orgID, projectID)] = strings.TrimSpace(transitKeyName)
}

// ProjectKeyURI returns any dedicated Vault Transit key configured for (orgID, projectID).
func (p *VaultTransitProvider) ProjectKeyURI(_ context.Context, orgID, projectID string) (string, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ref, ok := p.projectKeys[projectMapKey(orgID, projectID)]
	return ref, ok && ref != "", nil
}

// WrapDEK encrypts an Org DEK under Vault Transit key kekKeyID with org context binding.
func (p *VaultTransitProvider) WrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, plaintextDEK []byte) ([]byte, []byte, error) {
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

// UnwrapDEK decrypts an Org DEK under Vault Transit key kekKeyID verifying org context binding.
func (p *VaultTransitProvider) UnwrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, _, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || strings.TrimSpace(orgID) == "" || dekVersion < 1 || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapDEK arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kekKeyID, ciphertext, gcpOrgAAD(orgID, dekVersion))
}

// WrapArchiveKey encrypts a backup archive master key under Vault Transit.
func (p *VaultTransitProvider) WrapArchiveKey(ctx context.Context, kekKeyID string, plaintextKey []byte) ([]byte, []byte, error) {
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

// UnwrapArchiveKey decrypts a backup archive master key via Vault Transit.
func (p *VaultTransitProvider) UnwrapArchiveKey(ctx context.Context, kekKeyID string, _, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapArchiveKey arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kekKeyID, ciphertext, []byte("tanpopo:v1:kms:archive"))
}

// WrapProjectDEK encrypts a Project DEK directly under a dedicated Vault Transit key with project context binding.
func (p *VaultTransitProvider) WrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, plaintextDEK []byte) ([]byte, error) {
	if strings.TrimSpace(kmsKeyRef) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || dekVersion < 1 || len(plaintextDEK) != vault.KeySizeBytes {
		return nil, fmt.Errorf("%w: invalid WrapProjectDEK arguments", vault.ErrInvalidInput)
	}
	return p.encrypt(ctx, kmsKeyRef, plaintextDEK, gcpProjectAAD(orgID, projectID, dekVersion))
}

// UnwrapProjectDEK decrypts a Project DEK under its dedicated Vault Transit key.
func (p *VaultTransitProvider) UnwrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, wrappedBlob []byte) ([]byte, error) {
	if strings.TrimSpace(kmsKeyRef) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || dekVersion < 1 || len(wrappedBlob) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapProjectDEK arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kmsKeyRef, wrappedBlob, gcpProjectAAD(orgID, projectID, dekVersion))
}

func (p *VaultTransitProvider) encrypt(ctx context.Context, keyName string, plaintext, contextAAD []byte) ([]byte, error) {
	body, err := json.Marshal(struct {
		Plaintext string `json:"plaintext"`
		Context   string `json:"context"`
	}{
		Plaintext: base64.StdEncoding.EncodeToString(plaintext),
		Context:   base64.StdEncoding.EncodeToString(contextAAD),
	})
	if err != nil {
		return nil, err
	}
	defer clear(body)

	respBytes, err := p.doTransit(ctx, "encrypt", keyName, body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("kms/vault: decode encrypt response: %w", err)
	}
	if strings.TrimSpace(resp.Data.Ciphertext) == "" {
		return nil, fmt.Errorf("kms/vault: empty ciphertext returned")
	}
	return []byte(resp.Data.Ciphertext), nil
}

func (p *VaultTransitProvider) decrypt(ctx context.Context, keyName string, ciphertext, contextAAD []byte) ([]byte, error) {
	body, err := json.Marshal(struct {
		Ciphertext string `json:"ciphertext"`
		Context    string `json:"context"`
	}{
		Ciphertext: string(ciphertext),
		Context:    base64.StdEncoding.EncodeToString(contextAAD),
	})
	if err != nil {
		return nil, err
	}

	respBytes, err := p.doTransit(ctx, "decrypt", keyName, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", vault.ErrDecryptionFailed, err)
	}
	defer clear(respBytes)

	var resp struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("%w: decode decrypt response: %v", vault.ErrDecryptionFailed, err)
	}
	pt, err := base64.StdEncoding.DecodeString(resp.Data.Plaintext)
	if err != nil || len(pt) != vault.KeySizeBytes {
		clear(pt)
		return nil, fmt.Errorf("%w: invalid decrypted DEK length (expected %s)", vault.ErrDecryptionFailed, strconv.Itoa(vault.KeySizeBytes))
	}
	return pt, nil
}

func (p *VaultTransitProvider) doTransit(ctx context.Context, op, keyName string, body []byte) ([]byte, error) {
	u := *p.address
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/" + p.mount + "/" + op + "/" + url.PathEscape(strings.TrimSpace(keyName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", p.token)
	if p.namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.namespace)
	}

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
		return nil, fmt.Errorf("kms/vault %s failed with HTTP %d", op, resp.StatusCode)
	}
	return respBody, nil
}
