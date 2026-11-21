// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package kms implements external Hardware/Cloud KEK and per-project root key
// providers (AWS KMS, GCP Cloud KMS, and HashiCorp Vault Transit) implementing
// vault.KEKProvider and vault.ProjectRootKeyProvider (TP-IMP-0041 / DEC-017 / CR-12).
//
// All HTTP egress routes exclusively through egress.NewGuardedHTTPClient.
package kms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
)

const maxKMSResponseBytes = 1 << 20

// AWSCredentials provides static or STS-derived AWS credentials for AWS KMS SigV4 requests.
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// AWSCredentialsProvider dynamically supplies fresh AWS credentials (e.g., IRSA / IAM roles).
type AWSCredentialsProvider interface {
	Credentials(ctx context.Context) (AWSCredentials, error)
}

// StaticAWSCredentials implements AWSCredentialsProvider for explicit credentials.
type StaticAWSCredentials struct {
	Creds AWSCredentials
}

// Credentials returns a copy of the static AWS credentials.
func (s StaticAWSCredentials) Credentials(_ context.Context) (AWSCredentials, error) {
	if strings.TrimSpace(s.Creds.AccessKeyID) == "" || strings.TrimSpace(s.Creds.SecretAccessKey) == "" {
		return AWSCredentials{}, errors.New("kms/aws: non-empty access_key_id and secret_access_key required")
	}
	return s.Creds, nil
}

// AWSKMSConfig configures an AWS KMS KEKProvider & ProjectRootKeyProvider.
type AWSKMSConfig struct {
	EndpointURL     string
	Region          string
	ActiveKeyID     string
	Creds           AWSCredentialsProvider
	ProjectKeys     map[string]string // "orgID:projectID" -> KMS key ID / ARN
	HTTPClient      *http.Client
	EgressPolicy    *egress.Policy
	HostResolver    egress.HostResolver
	TLSRootCAs      *tls.Config
	Now             func() time.Time
	RequestTimeout  time.Duration
}

// AWSKMSProvider wraps/unwraps DEKs via AWS KMS Encrypt and Decrypt APIs with strict EncryptionContext binding.
type AWSKMSProvider struct {
	endpoint     *url.URL
	region       string
	activeKeyID  string
	creds        AWSCredentialsProvider
	client       *http.Client
	now          func() time.Time
	mu           sync.RWMutex
	projectKeys  map[string]string
}

// NewAWSKMSProvider constructs a fail-closed AWSKMSProvider routed through SSRFGuardDialer.
func NewAWSKMSProvider(cfg AWSKMSConfig) (*AWSKMSProvider, error) {
	rawEP := strings.TrimSpace(cfg.EndpointURL)
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return nil, fmt.Errorf("%w: kms/aws region required", vault.ErrInvalidInput)
	}
	if rawEP == "" {
		rawEP = fmt.Sprintf("https://kms.%s.amazonaws.com", region)
	}
	u, err := url.Parse(rawEP)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: kms/aws invalid endpoint_url %q", vault.ErrInvalidInput, rawEP)
	}
	if strings.TrimSpace(cfg.ActiveKeyID) == "" {
		return nil, fmt.Errorf("%w: kms/aws active_key_id required", vault.ErrInvalidInput)
	}
	if cfg.Creds == nil {
		return nil, fmt.Errorf("%w: kms/aws credentials provider required", vault.ErrInvalidInput)
	}
	var rootCAs *x509.CertPool
	if cfg.TLSRootCAs != nil {
		rootCAs = cfg.TLSRootCAs.RootCAs
	}
	client, err := buildKMSHTTPClient(u, cfg.HTTPClient, cfg.EgressPolicy, cfg.HostResolver, rootCAs, cfg.RequestTimeout)
	if err != nil {
		return nil, err
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	pk := make(map[string]string, len(cfg.ProjectKeys))
	for k, v := range cfg.ProjectKeys {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			pk[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return &AWSKMSProvider{
		endpoint:    u,
		region:      region,
		activeKeyID: strings.TrimSpace(cfg.ActiveKeyID),
		creds:       cfg.Creds,
		client:      client,
		now:         nowFn,
		projectKeys: pk,
	}, nil
}

// ActiveKeyID returns the configured primary AWS KMS Key ID/ARN.
func (p *AWSKMSProvider) ActiveKeyID(_ context.Context) (string, error) {
	return p.activeKeyID, nil
}

// SetProjectKey registers or updates a dedicated AWS KMS key ARN/ID for (orgID, projectID).
func (p *AWSKMSProvider) SetProjectKey(orgID, projectID, kmsKeyID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(kmsKeyID) == "" {
		delete(p.projectKeys, projectMapKey(orgID, projectID))
		return
	}
	p.projectKeys[projectMapKey(orgID, projectID)] = strings.TrimSpace(kmsKeyID)
}

// ProjectKeyURI returns any dedicated AWS KMS key configured for (orgID, projectID).
func (p *AWSKMSProvider) ProjectKeyURI(_ context.Context, orgID, projectID string) (string, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ref, ok := p.projectKeys[projectMapKey(orgID, projectID)]
	return ref, ok && ref != "", nil
}

// WrapDEK encrypts an Org DEK via AWS KMS Encrypt with EncryptionContext bound to org_id and dek_version.
func (p *AWSKMSProvider) WrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, plaintextDEK []byte) ([]byte, []byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || strings.TrimSpace(orgID) == "" || dekVersion < 1 || len(plaintextDEK) != vault.KeySizeBytes {
		return nil, nil, fmt.Errorf("%w: invalid WrapDEK arguments", vault.ErrInvalidInput)
	}
	ct, err := p.encrypt(ctx, kekKeyID, plaintextDEK, map[string]string{
		"tanpopo_scope":       "org_dek",
		"tanpopo_org_id":      orgID,
		"tanpopo_dek_version": strconv.FormatInt(dekVersion, 10),
	})
	if err != nil {
		return nil, nil, err
	}
	// AWS KMS embeds its own key metadata inside CiphertextBlob; persist empty/random placeholder nonce if needed by caller.
	nonce := make([]byte, vault.NonceSizeBytes)
	nonce[0] = 0x01
	return nonce, ct, nil
}

// UnwrapDEK decrypts an Org DEK via AWS KMS Decrypt verifying EncryptionContext.
func (p *AWSKMSProvider) UnwrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, _, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || strings.TrimSpace(orgID) == "" || dekVersion < 1 || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapDEK arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kekKeyID, ciphertext, map[string]string{
		"tanpopo_scope":       "org_dek",
		"tanpopo_org_id":      orgID,
		"tanpopo_dek_version": strconv.FormatInt(dekVersion, 10),
	})
}

// WrapArchiveKey encrypts a backup archive master key under AWS KMS.
func (p *AWSKMSProvider) WrapArchiveKey(ctx context.Context, kekKeyID string, plaintextKey []byte) ([]byte, []byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || len(plaintextKey) != vault.KeySizeBytes {
		return nil, nil, fmt.Errorf("%w: invalid WrapArchiveKey arguments", vault.ErrInvalidInput)
	}
	ct, err := p.encrypt(ctx, kekKeyID, plaintextKey, map[string]string{
		"tanpopo_scope": "archive_key",
	})
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, vault.NonceSizeBytes)
	nonce[0] = 0x01
	return nonce, ct, nil
}

// UnwrapArchiveKey decrypts a backup archive master key via AWS KMS.
func (p *AWSKMSProvider) UnwrapArchiveKey(ctx context.Context, kekKeyID string, _, ciphertext []byte) ([]byte, error) {
	if strings.TrimSpace(kekKeyID) == "" || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapArchiveKey arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kekKeyID, ciphertext, map[string]string{
		"tanpopo_scope": "archive_key",
	})
}

// WrapProjectDEK encrypts a Project DEK directly under a dedicated AWS KMS key with project-scoped EncryptionContext.
func (p *AWSKMSProvider) WrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, plaintextDEK []byte) ([]byte, error) {
	if strings.TrimSpace(kmsKeyRef) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || dekVersion < 1 || len(plaintextDEK) != vault.KeySizeBytes {
		return nil, fmt.Errorf("%w: invalid WrapProjectDEK arguments", vault.ErrInvalidInput)
	}
	return p.encrypt(ctx, kmsKeyRef, plaintextDEK, map[string]string{
		"tanpopo_scope":       "project_dek",
		"tanpopo_org_id":      orgID,
		"tanpopo_project_id":  projectID,
		"tanpopo_dek_version": strconv.FormatInt(dekVersion, 10),
	})
}

// UnwrapProjectDEK decrypts a Project DEK directly from its dedicated AWS KMS key.
func (p *AWSKMSProvider) UnwrapProjectDEK(ctx context.Context, kmsKeyRef, orgID, projectID string, dekVersion int64, wrappedBlob []byte) ([]byte, error) {
	if strings.TrimSpace(kmsKeyRef) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || dekVersion < 1 || len(wrappedBlob) == 0 {
		return nil, fmt.Errorf("%w: invalid UnwrapProjectDEK arguments", vault.ErrDecryptionFailed)
	}
	return p.decrypt(ctx, kmsKeyRef, wrappedBlob, map[string]string{
		"tanpopo_scope":       "project_dek",
		"tanpopo_org_id":      orgID,
		"tanpopo_project_id":  projectID,
		"tanpopo_dek_version": strconv.FormatInt(dekVersion, 10),
	})
}

func (p *AWSKMSProvider) encrypt(ctx context.Context, keyID string, plaintext []byte, encCtx map[string]string) ([]byte, error) {
	reqPayload := struct {
		KeyID             string            `json:"KeyId"`
		Plaintext         string            `json:"Plaintext"`
		EncryptionContext map[string]string `json:"EncryptionContext,omitempty"`
	}{
		KeyID:             keyID,
		Plaintext:         base64.StdEncoding.EncodeToString(plaintext),
		EncryptionContext: encCtx,
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}
	defer clear(body)

	respBytes, err := p.doKMSRequest(ctx, "TrentService.Encrypt", body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		CiphertextBlob string `json:"CiphertextBlob"`
		KeyID          string `json:"KeyId"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("kms/aws: decode Encrypt response: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(resp.CiphertextBlob)
	if err != nil || len(ct) == 0 {
		return nil, fmt.Errorf("kms/aws: invalid CiphertextBlob returned")
	}
	return ct, nil
}

func (p *AWSKMSProvider) decrypt(ctx context.Context, keyID string, ciphertext []byte, encCtx map[string]string) ([]byte, error) {
	reqPayload := struct {
		KeyID             string            `json:"KeyId,omitempty"`
		CiphertextBlob    string            `json:"CiphertextBlob"`
		EncryptionContext map[string]string `json:"EncryptionContext,omitempty"`
	}{
		KeyID:             keyID,
		CiphertextBlob:    base64.StdEncoding.EncodeToString(ciphertext),
		EncryptionContext: encCtx,
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	respBytes, err := p.doKMSRequest(ctx, "TrentService.Decrypt", body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", vault.ErrDecryptionFailed, err)
	}
	defer clear(respBytes)

	var resp struct {
		Plaintext string `json:"Plaintext"`
		KeyID     string `json:"KeyId"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("%w: decode Decrypt response: %v", vault.ErrDecryptionFailed, err)
	}
	pt, err := base64.StdEncoding.DecodeString(resp.Plaintext)
	if err != nil || len(pt) != vault.KeySizeBytes {
		clear(pt)
		return nil, fmt.Errorf("%w: invalid decrypted DEK length", vault.ErrDecryptionFailed)
	}
	return pt, nil
}

func (p *AWSKMSProvider) doKMSRequest(ctx context.Context, target string, body []byte) ([]byte, error) {
	creds, err := p.creds.Credentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("kms/aws: resolve credentials: %w", err)
	}
	defer func() { creds.SecretAccessKey = ""; creds.SessionToken = "" }()

	u := *p.endpoint
	if u.Path == "" {
		u.Path = "/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", target)
	req.Host = u.Host
	signAWSRequestV4(req, body, p.region, "kms", creds, p.now())

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
		return nil, fmt.Errorf("kms/aws %s failed with HTTP %d", target, resp.StatusCode)
	}
	return respBody, nil
}

func signAWSRequestV4(req *http.Request, body []byte, region, service string, creds AWSCredentials, now time.Time) {
	utcNow := now.UTC()
	amzDate := utcNow.Format("20060102T150405Z")
	dateStamp := utcNow.Format("20060102")
	payloadSum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payloadSum[:])

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	hdrNames := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	if creds.SessionToken != "" {
		hdrNames = append(hdrNames, "x-amz-security-token")
	}
	sort.Strings(hdrNames)

	var canonHdrs strings.Builder
	for _, h := range hdrNames {
		val := strings.TrimSpace(req.Header.Get(h))
		if h == "host" {
			val = req.Host
		}
		canonHdrs.WriteString(h)
		canonHdrs.WriteByte(':')
		canonHdrs.WriteString(val)
		canonHdrs.WriteByte('\n')
	}
	signedHdrs := strings.Join(hdrNames, ";")
	canonPath := req.URL.EscapedPath()
	if canonPath == "" {
		canonPath = "/"
	}
	canonReq := strings.Join([]string{
		req.Method,
		canonPath,
		req.URL.RawQuery,
		canonHdrs.String(),
		signedHdrs,
		payloadHash,
	}, "\n")

	canonSum := sha256.Sum256([]byte(canonReq))
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(canonSum[:])

	kDate := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	sig := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.AccessKeyID, scope, signedHdrs, sig,
	))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func projectMapKey(orgID, projectID string) string {
	return strings.TrimSpace(orgID) + ":" + strings.TrimSpace(projectID)
}

func buildKMSHTTPClient(u *url.URL, customClient *http.Client, customPolicy *egress.Policy, hostResolver egress.HostResolver, rootCAs *x509.CertPool, timeout time.Duration) (*http.Client, error) {
	if customClient != nil {
		if customClient.Transport == nil {
			return nil, fmt.Errorf("%w: kms custom http client requires non-nil Transport", vault.ErrInvalidInput)
		}
		return customClient, nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	var pol egress.Policy
	if customPolicy != nil {
		pol = *customPolicy
	} else {
		pol = egress.Policy{
			AllowedHosts:   []string{u.Host},
			AllowPrivateIP: false,
			RequireTLS:     true,
		}
	}
	return egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy:                   pol,
		Timeout:                  timeout,
		Resolver:                 hostResolver,
		RootCAs:                  rootCAs,
		DenyCrossOriginRedirects: true,
	})
}
