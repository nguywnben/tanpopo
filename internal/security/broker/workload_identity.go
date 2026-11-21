// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
)

const (
	// WorkloadIdentityAWSIRSA exchanges an ambient OIDC JWT via AWS STS AssumeRoleWithWebIdentity (Bedrock / AWS).
	WorkloadIdentityAWSIRSA WorkloadIdentityMode = "AWS_IRSA"
	// WorkloadIdentityGCPFederation exchanges an ambient OIDC JWT via GCP STS + IAM Credentials (Vertex AI / GCP).
	WorkloadIdentityGCPFederation WorkloadIdentityMode = "GCP_WORKLOAD_IDENTITY"

	maxWebIdentityTokenBytes = 64 << 10
	maxWorkloadResponseBytes = 1 << 20
	workloadRefreshMargin    = 30 * time.Second
	maxWorkloadCacheEntries  = 512
)

// ErrWorkloadIdentityConfig indicates invalid or incomplete Workload Identity configuration.
var ErrWorkloadIdentityConfig = errors.New("broker: invalid workload identity binding")

// WorkloadIdentityMode identifies the target cloud federation exchange protocol.
type WorkloadIdentityMode string

// AmbientOIDCTokenSource yields raw ambient workload OIDC JWT bytes.
type AmbientOIDCTokenSource func(ctx context.Context) ([]byte, error)

// WorkloadIdentityBinding binds a tenant/target host allowlist to keyless cloud federation parameters (TP-FR-0206).
type WorkloadIdentityBinding struct {
	BindingID                 string
	OrgID                     string
	Mode                      WorkloadIdentityMode
	AllowedHosts              []string
	TokenFilePath             string
	TokenSource               AmbientOIDCTokenSource
	RoleARN                   string
	RoleSessionName           string
	STSEndpoint               string
	Audience                  string
	ServiceAccountEmail       string
	GCPSTSEndpoint            string
	GCPIAMCredentialsEndpoint string
	Scopes                    []string
	AllowedTokenRoots         []string
	EgressPolicy              *egress.Policy
	HostResolver              egress.HostResolver
	RequestTimeout            time.Duration
}

// EphemeralCloudCredentials holds short-lived exchanged AWS SigV4 or GCP Bearer credentials.
type EphemeralCloudCredentials struct {
	Mode            WorkloadIdentityMode
	AccessKeyID     string
	SecretAccessKey []byte
	SessionToken    []byte
	BearerToken     []byte
	ExpiresAt       time.Time
}

// Zeroize overwrites all ephemeral credential byte buffers in memory.
func (c *EphemeralCloudCredentials) Zeroize() {
	if c == nil {
		return
	}
	c.AccessKeyID = ""
	clear(c.SecretAccessKey)
	c.SecretAccessKey = nil
	clear(c.SessionToken)
	c.SessionToken = nil
	clear(c.BearerToken)
	c.BearerToken = nil
}

// AWSCredentialJSON marshals ephemeral AWS STS credentials into the canonical JSON format accepted by signAWSSigV4.
func (c *EphemeralCloudCredentials) AWSCredentialJSON() ([]byte, error) {
	if c == nil || c.Mode != WorkloadIdentityAWSIRSA || c.AccessKeyID == "" || len(c.SecretAccessKey) == 0 {
		return nil, fmt.Errorf("%w: invalid AWS STS ephemeral credentials", ErrWorkloadIdentityConfig)
	}
	doc := struct {
		AccessKeyID     string `json:"access_key_id"`
		SecretAccessKey string `json:"secret_access_key"`
		SessionToken    string `json:"session_token,omitempty"`
	}{
		AccessKeyID:     c.AccessKeyID,
		SecretAccessKey: string(c.SecretAccessKey),
		SessionToken:    string(c.SessionToken),
	}
	return json.Marshal(doc)
}

type cachedWorkloadEntry struct {
	creds *EphemeralCloudCredentials
}

// WorkloadIdentityExchanger resolves and caches short-lived cloud credentials from ambient OIDC tokens
// under explicit Host-level allowlist enforcement and SSRF-guarded HTTP egress.
type WorkloadIdentityExchanger struct {
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cachedWorkloadEntry
}

// NewWorkloadIdentityExchanger constructs a fail-closed keyless cloud credential exchanger.
func NewWorkloadIdentityExchanger(nowFn func() time.Time) *WorkloadIdentityExchanger {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &WorkloadIdentityExchanger{
		now:   nowFn,
		cache: make(map[string]cachedWorkloadEntry),
	}
}

// Close zeroizes and evicts all cached ephemeral cloud credentials.
func (w *WorkloadIdentityExchanger) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, v := range w.cache {
		v.creds.Zeroize()
		delete(w.cache, k)
	}
}

// ExchangeHostCredentials verifies that targetHost is permitted by binding.AllowedHosts, then returns
// cached or freshly exchanged short-lived credentials for AWS Bedrock / GCP Vertex AI.
func (w *WorkloadIdentityExchanger) ExchangeHostCredentials(ctx context.Context, binding WorkloadIdentityBinding, targetHost string) (*EphemeralCloudCredentials, error) {
	if err := validateHostAgainstAllowlist(targetHost, binding.AllowedHosts); err != nil {
		return nil, err
	}
	cacheKey := string(binding.Mode) + "|" + strings.TrimSpace(binding.OrgID) + "|" + strings.TrimSpace(binding.BindingID) + "|" + strings.ToLower(strings.TrimSpace(targetHost))
	now := w.now()

	w.mu.Lock()
	if ent, ok := w.cache[cacheKey]; ok && ent.creds != nil && ent.creds.ExpiresAt.After(now.Add(workloadRefreshMargin)) {
		cp := cloneWorkloadCreds(ent.creds)
		w.mu.Unlock()
		return cp, nil
	}
	w.mu.Unlock()

	rawToken, err := readAmbientOIDCToken(ctx, binding)
	if err != nil {
		return nil, err
	}
	defer clear(rawToken)

	var fresh *EphemeralCloudCredentials
	switch binding.Mode {
	case WorkloadIdentityAWSIRSA:
		fresh, err = exchangeAWSIRSA(ctx, binding, rawToken, now)
	case WorkloadIdentityGCPFederation:
		fresh, err = exchangeGCPWIF(ctx, binding, rawToken, now)
	default:
		return nil, fmt.Errorf("%w: unsupported workload identity mode %q", ErrWorkloadIdentityConfig, binding.Mode)
	}
	if err != nil {
		return nil, err
	}

	w.mu.Lock()
	if len(w.cache) >= maxWorkloadCacheEntries {
		for k, v := range w.cache {
			v.creds.Zeroize()
			delete(w.cache, k)
		}
	}
	if prev, ok := w.cache[cacheKey]; ok && prev.creds != nil {
		prev.creds.Zeroize()
	}
	w.cache[cacheKey] = cachedWorkloadEntry{creds: fresh}
	out := cloneWorkloadCreds(fresh)
	w.mu.Unlock()
	return out, nil
}

func validateHostAgainstAllowlist(targetHost string, allowedHosts []string) error {
	host := strings.ToLower(strings.TrimSpace(targetHost))
	if host == "" {
		return &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "target host is required for workload identity exchange",
			Err:        egress.ErrHostDenied,
		}
	}
	if len(allowedHosts) == 0 {
		return &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    "workload identity binding defines no allowed hosts",
			Err:        egress.ErrHostDenied,
		}
	}
	if !egress.HostMatches(&url.URL{Scheme: "https", Host: host}, allowedHosts) {
		return &egress.Error{
			HTTPStatus: http.StatusForbidden,
			Code:       "egress_host_denied",
			Message:    fmt.Sprintf("host %q denied by workload identity allowlist", host),
			Err:        egress.ErrHostDenied,
		}
	}
	return nil
}

func readAmbientOIDCToken(ctx context.Context, binding WorkloadIdentityBinding) ([]byte, error) {
	if binding.TokenSource != nil {
		tok, err := binding.TokenSource(ctx)
		if err != nil {
			return nil, fmt.Errorf("broker: read ambient OIDC token: %w", err)
		}
		trimmed := bytes.TrimSpace(tok)
		if len(trimmed) == 0 || len(trimmed) > maxWebIdentityTokenBytes {
			clear(tok)
			return nil, fmt.Errorf("%w: empty or oversized ambient OIDC token", ErrWorkloadIdentityConfig)
		}
		out := make([]byte, len(trimmed))
		copy(out, trimmed)
		clear(tok)
		return out, nil
	}
	rawPath := strings.TrimSpace(binding.TokenFilePath)
	if rawPath == "" {
		return nil, fmt.Errorf("%w: token_file_path or token_source required", ErrWorkloadIdentityConfig)
	}
	cleanPath := filepath.Clean(rawPath)
	if !filepath.IsAbs(cleanPath) {
		return nil, fmt.Errorf("%w: web identity token_file_path must be an absolute path", ErrWorkloadIdentityConfig)
	}
	if !isPathWithinAllowedRoots(cleanPath, binding.AllowedTokenRoots) {
		return nil, fmt.Errorf("%w: web identity token_file_path %q outside allowed token roots", ErrWorkloadIdentityConfig, cleanPath)
	}
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("broker: stat web identity token file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: web identity token file must be a regular non-symlink file", ErrWorkloadIdentityConfig)
	}
	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("broker: open web identity token file: %w", err)
	}
	defer func() { _ = f.Close() }()
	fdInfo, err := f.Stat()
	if err != nil || !fdInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: web identity token file handle must be a regular file", ErrWorkloadIdentityConfig)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxWebIdentityTokenBytes+1))
	if err != nil {
		clear(raw)
		return nil, fmt.Errorf("broker: read web identity token file: %w", err)
	}
	if int64(len(raw)) > maxWebIdentityTokenBytes {
		clear(raw)
		return nil, fmt.Errorf("%w: ambient OIDC token file exceeds max size", ErrWorkloadIdentityConfig)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		clear(raw)
		return nil, fmt.Errorf("%w: ambient OIDC token file is empty", ErrWorkloadIdentityConfig)
	}
	out := make([]byte, len(trimmed))
	copy(out, trimmed)
	clear(raw)
	return out, nil
}

func isPathWithinAllowedRoots(cleanPath string, allowedRoots []string) bool {
	roots := allowedRoots
	if len(roots) == 0 {
		roots = []string{"/var/run/secrets"}
	}
	evalDir, err := filepath.EvalSymlinks(filepath.Dir(cleanPath))
	if err != nil {
		evalDir = filepath.Dir(cleanPath)
	}
	resolvedTarget := filepath.Join(evalDir, filepath.Base(cleanPath))
	for _, r := range roots {
		cleanRoot := filepath.Clean(strings.TrimSpace(r))
		if cleanRoot == "" || !filepath.IsAbs(cleanRoot) {
			continue
		}
		evalRoot, err := filepath.EvalSymlinks(cleanRoot)
		if err != nil {
			evalRoot = cleanRoot
		}
		rel, rerr := filepath.Rel(evalRoot, resolvedTarget)
		if rerr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func exchangeAWSIRSA(ctx context.Context, binding WorkloadIdentityBinding, jwtToken []byte, now time.Time) (*EphemeralCloudCredentials, error) {
	if strings.TrimSpace(binding.RoleARN) == "" {
		return nil, fmt.Errorf("%w: AWS IRSA role_arn required", ErrWorkloadIdentityConfig)
	}
	ep := strings.TrimSpace(binding.STSEndpoint)
	if ep == "" {
		ep = "https://sts.amazonaws.com"
	}
	client, err := buildWorkloadHTTPClient(ep, binding.EgressPolicy, binding.HostResolver, binding.RequestTimeout)
	if err != nil {
		return nil, err
	}
	sessName := strings.TrimSpace(binding.RoleSessionName)
	if sessName == "" {
		sessName = "tanpopo-gateway"
	}
	form := url.Values{}
	form.Set("Action", "AssumeRoleWithWebIdentity")
	form.Set("Version", "2011-06-15")
	form.Set("RoleArn", strings.TrimSpace(binding.RoleARN))
	form.Set("RoleSessionName", sessName)
	form.Set("WebIdentityToken", string(jwtToken))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker: AWS STS exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWorkloadResponseBytes))
	if err != nil {
		return nil, err
	}
	defer clear(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("broker: AWS STS AssumeRoleWithWebIdentity rejected with HTTP %d", resp.StatusCode)
	}

	var ak, sk, st, expStr string
	var jsonEnv struct {
		AssumeRoleWithWebIdentityResponse struct {
			AssumeRoleWithWebIdentityResult struct {
				Credentials struct {
					AccessKeyID     string `json:"AccessKeyId"`
					SecretAccessKey string `json:"SecretAccessKey"`
					SessionToken    string `json:"SessionToken"`
					Expiration      any    `json:"Expiration"`
				} `json:"Credentials"`
			} `json:"AssumeRoleWithWebIdentityResult"`
		} `json:"AssumeRoleWithWebIdentityResponse"`
	}
	if err := json.Unmarshal(body, &jsonEnv); err == nil && jsonEnv.AssumeRoleWithWebIdentityResponse.AssumeRoleWithWebIdentityResult.Credentials.AccessKeyID != "" {
		c := jsonEnv.AssumeRoleWithWebIdentityResponse.AssumeRoleWithWebIdentityResult.Credentials
		ak, sk, st = c.AccessKeyID, c.SecretAccessKey, c.SessionToken
		switch v := c.Expiration.(type) {
		case string:
			expStr = v
		case float64:
			expStr = time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
		}
	} else {
		var xmlEnv struct {
			Credentials struct {
				AccessKeyID     string `xml:"AssumeRoleWithWebIdentityResult>Credentials>AccessKeyId"`
				SecretAccessKey string `xml:"AssumeRoleWithWebIdentityResult>Credentials>SecretAccessKey"`
				SessionToken    string `xml:"AssumeRoleWithWebIdentityResult>Credentials>SessionToken"`
				Expiration      string `xml:"AssumeRoleWithWebIdentityResult>Credentials>Expiration"`
			}
		}
		if xerr := xml.Unmarshal(body, &xmlEnv.Credentials); xerr != nil || xmlEnv.Credentials.AccessKeyID == "" {
			return nil, errors.New("broker: failed decoding AWS STS AssumeRoleWithWebIdentity credentials")
		}
		ak = xmlEnv.Credentials.AccessKeyID
		sk = xmlEnv.Credentials.SecretAccessKey
		st = xmlEnv.Credentials.SessionToken
		expStr = xmlEnv.Credentials.Expiration
	}
	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		return nil, errors.New("broker: incomplete AWS STS credentials")
	}
	exp := now.Add(15 * time.Minute)
	if parsed, perr := time.Parse(time.RFC3339, strings.TrimSpace(expStr)); perr == nil {
		exp = parsed
	}
	return &EphemeralCloudCredentials{
		Mode:            WorkloadIdentityAWSIRSA,
		AccessKeyID:     ak,
		SecretAccessKey: []byte(sk),
		SessionToken:    []byte(st),
		ExpiresAt:       exp,
	}, nil
}

func exchangeGCPWIF(ctx context.Context, binding WorkloadIdentityBinding, jwtToken []byte, now time.Time) (*EphemeralCloudCredentials, error) {
	if strings.TrimSpace(binding.Audience) == "" {
		return nil, fmt.Errorf("%w: GCP workload identity audience required", ErrWorkloadIdentityConfig)
	}
	stsEP := strings.TrimSpace(binding.GCPSTSEndpoint)
	if stsEP == "" {
		stsEP = "https://sts.googleapis.com/v1/token"
	}
	stsClient, err := buildWorkloadHTTPClient(stsEP, binding.EgressPolicy, binding.HostResolver, binding.RequestTimeout)
	if err != nil {
		return nil, err
	}
	scopes := binding.Scopes
	if len(scopes) == 0 {
		scopes = []string{"https://www.googleapis.com/auth/cloud-platform"}
	}
	stsReqBody, err := json.Marshal(struct {
		GrantType          string `json:"grantType"`
		Audience           string `json:"audience"`
		Scope              string `json:"scope"`
		RequestedTokenType string `json:"requestedTokenType"`
		SubjectTokenType   string `json:"subjectTokenType"`
		SubjectToken       string `json:"subjectToken"`
	}{
		GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
		Audience:           strings.TrimSpace(binding.Audience),
		Scope:              strings.Join(scopes, " "),
		RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		SubjectTokenType:   "urn:ietf:params:oauth:token-type:jwt",
		SubjectToken:       string(jwtToken),
	})
	if err != nil {
		return nil, err
	}
	defer clear(stsReqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stsEP, bytes.NewReader(stsReqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := stsClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker: GCP STS exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWorkloadResponseBytes))
	if err != nil {
		return nil, err
	}
	defer clear(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("broker: GCP STS rejected token exchange with HTTP %d", resp.StatusCode)
	}
	var stsOut struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &stsOut); err != nil || strings.TrimSpace(stsOut.AccessToken) == "" {
		return nil, errors.New("broker: GCP STS response missing access_token")
	}

	exp := now.Add(time.Duration(maxInt64(stsOut.ExpiresIn, 600)) * time.Second)
	finalTok := []byte(stsOut.AccessToken)

	if sa := strings.TrimSpace(binding.ServiceAccountEmail); sa != "" {
		iamBase := strings.TrimRight(strings.TrimSpace(binding.GCPIAMCredentialsEndpoint), "/")
		if iamBase == "" {
			iamBase = "https://iamcredentials.googleapis.com"
		}
		impersonateURL := iamBase + "/v1/projects/-/serviceAccounts/" + url.PathEscape(sa) + ":generateAccessToken"
		iamClient, err := buildWorkloadHTTPClient(impersonateURL, binding.EgressPolicy, binding.HostResolver, binding.RequestTimeout)
		if err != nil {
			clear(finalTok)
			return nil, err
		}
		iamPayload, _ := json.Marshal(struct {
			Scope    []string `json:"scope"`
			Lifetime string   `json:"lifetime,omitempty"`
		}{
			Scope:    scopes,
			Lifetime: "1800s",
		})
		ireq, err := http.NewRequestWithContext(ctx, http.MethodPost, impersonateURL, bytes.NewReader(iamPayload))
		if err != nil {
			clear(finalTok)
			return nil, err
		}
		ireq.Header.Set("Content-Type", "application/json")
		ireq.Header.Set("Authorization", "Bearer "+string(finalTok))
		clear(finalTok)
		iresp, err := iamClient.Do(ireq)
		if err != nil {
			return nil, fmt.Errorf("broker: GCP IAM generateAccessToken failed: %w", err)
		}
		defer func() { _ = iresp.Body.Close() }()
		ibody, err := io.ReadAll(io.LimitReader(iresp.Body, maxWorkloadResponseBytes))
		if err != nil {
			return nil, err
		}
		defer clear(ibody)
		if iresp.StatusCode < 200 || iresp.StatusCode >= 300 {
			return nil, fmt.Errorf("broker: GCP IAM generateAccessToken rejected with HTTP %d", iresp.StatusCode)
		}
		var iamOut struct {
			AccessToken string `json:"accessToken"`
			ExpireTime  string `json:"expireTime"`
		}
		if err := json.Unmarshal(ibody, &iamOut); err != nil || strings.TrimSpace(iamOut.AccessToken) == "" {
			return nil, errors.New("broker: GCP IAM response missing accessToken")
		}
		if parsed, perr := time.Parse(time.RFC3339, strings.TrimSpace(iamOut.ExpireTime)); perr == nil {
			exp = parsed
		}
		finalTok = []byte(iamOut.AccessToken)
	}

	return &EphemeralCloudCredentials{
		Mode:        WorkloadIdentityGCPFederation,
		BearerToken: finalTok,
		ExpiresAt:   exp,
	}, nil
}

func buildWorkloadHTTPClient(rawEndpoint string, customPolicy *egress.Policy, resolver egress.HostResolver, timeout time.Duration) (*http.Client, error) {
	u, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: invalid endpoint URL %q", ErrWorkloadIdentityConfig, rawEndpoint)
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
		Resolver:                 resolver,
		DenyCrossOriginRedirects: true,
	})
}

func cloneWorkloadCreds(src *EphemeralCloudCredentials) *EphemeralCloudCredentials {
	if src == nil {
		return nil
	}
	return &EphemeralCloudCredentials{
		Mode:            src.Mode,
		AccessKeyID:     src.AccessKeyID,
		SecretAccessKey: append([]byte(nil), src.SecretAccessKey...),
		SessionToken:    append([]byte(nil), src.SessionToken...),
		BearerToken:     append([]byte(nil), src.BearerToken...),
		ExpiresAt:       src.ExpiresAt,
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
