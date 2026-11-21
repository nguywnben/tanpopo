// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package cache implements Tanpopo's default-disabled, strictly tenant-isolated
// Exact-Match Response Cache (CR-09, CT-28, FMEA-03 / TP-IMP-0041).
//
// Invariants:
//   - Disabled at every scope unless explicitly enabled via RESPONSE_CACHE policy.
//   - Only deterministic non-streaming temperature==0 requests without
//     Cache-Control: no-store/no-cache are eligible.
//   - Errors, truncated payloads, and tool-calling responses are never cached.
//   - Keys equal SHA-256(JCS({ org_id, project_id, env_id, policy_revision,
//     model_deployment_id, tools_schema_hash, canonical_request })).
//   - Values are sealed in AES-256-GCM under the tenant Project DEK with
//     ProjectRecordAAD(..., RecordKindCacheEntry, cache_key_sha256).
package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	defaultCacheTTLSeconds   int64 = 300
	defaultMaxCacheEntrySize int64 = 1 << 20 // 1 MiB
)

var (
	// ErrCacheMiss indicates no valid cached entry was found for the exact key.
	ErrCacheMiss = errors.New("cache: miss")
	// ErrUndecryptable indicates a persisted entry cannot be decrypted because its Project DEK or external root key is destroyed.
	ErrUndecryptable = errors.New("cache: entry permanently undecryptable")
	// ErrInvalidConfig indicates missing ExactCache dependencies.
	ErrInvalidConfig = errors.New("cache: invalid exact cache configuration")
)

// Config configures ExactCache fail-closed.
type Config struct {
	DB            *sql.DB
	Vault         *vault.Store
	NowMs         func() int64
	MaxEntryBytes int64
}

// ExactCache manages Project DEK-encrypted exact-match response cache entries in SQLite or PostgreSQL 16 FORCE RLS.
type ExactCache struct {
	db            *sql.DB
	dialect       port.Dialect
	vault         *vault.Store
	nowMs         func() int64
	maxEntryBytes int64
}

// NewExactCache constructs a fail-closed ExactCache.
func NewExactCache(cfg Config) (*ExactCache, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB required", ErrInvalidConfig)
	}
	if cfg.Vault == nil {
		return nil, fmt.Errorf("%w: non-nil *vault.Store required", ErrInvalidConfig)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	maxBytes := cfg.MaxEntryBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxCacheEntrySize
	}
	return &ExactCache{
		db:            cfg.DB,
		dialect:       port.DetectDialect(context.Background(), cfg.DB),
		vault:         cfg.Vault,
		nowMs:         nowFn,
		maxEntryBytes: maxBytes,
	}, nil
}

// KeyInput holds all tenant, routing, policy, tool, and canonical request dimensions bound into the cache key.
type KeyInput struct {
	OrgID             string
	ProjectID         string
	EnvID             string
	PolicyRevision    string
	ModelDeploymentID string
	RequestBody       []byte
}

// EligibilityResult describes whether a request is eligible for lookup/storage and holds its precomputed cache key.
type EligibilityResult struct {
	Eligible       bool
	BypassReason   string
	CacheKeySHA256 string
	TTLSeconds     int64
	MaxEntryBytes  int64
}

// EvaluateEligibility checks policy opt-in, Cache-Control directives, streaming flag, and temperature == 0,
// and computes the deterministic JCS SHA-256 cache key when eligible.
func (c *ExactCache) EvaluateEligibility(headers http.Header, effPolicy engine.EffectivePolicy, in KeyInput) (EligibilityResult, error) {
	if !effPolicy.ResponseCacheEnabled {
		return EligibilityResult{Eligible: false, BypassReason: "cache_disabled"}, nil
	}
	if hasNoStoreCacheControl(headers) {
		return EligibilityResult{Eligible: false, BypassReason: "cache_control_no_store"}, nil
	}
	if strings.TrimSpace(in.OrgID) == "" || strings.TrimSpace(in.ProjectID) == "" ||
		strings.TrimSpace(in.EnvID) == "" || strings.TrimSpace(in.ModelDeploymentID) == "" {
		return EligibilityResult{Eligible: false, BypassReason: "missing_scope"}, nil
	}

	var rawObj map[string]any
	dec := json.NewDecoder(bytes.NewReader(in.RequestBody))
	dec.UseNumber()
	if err := dec.Decode(&rawObj); err != nil || rawObj == nil {
		return EligibilityResult{Eligible: false, BypassReason: "invalid_json"}, nil
	}
	if streamVal, hasStream := rawObj["stream"]; hasStream {
		if b, ok := streamVal.(bool); ok && b {
			return EligibilityResult{Eligible: false, BypassReason: "streaming_unsupported"}, nil
		}
	}
	tempVal, hasTemp := rawObj["temperature"]
	if !hasTemp || !isExactZeroJSONNumber(tempVal) {
		return EligibilityResult{Eligible: false, BypassReason: "non_deterministic_temperature"}, nil
	}

	keySHA, err := ComputeCacheKeySHA256(in)
	if err != nil {
		return EligibilityResult{}, err
	}
	ttlSec := defaultCacheTTLSeconds
	if effPolicy.ResponseCacheTTLSeconds != nil && *effPolicy.ResponseCacheTTLSeconds > 0 {
		ttlSec = *effPolicy.ResponseCacheTTLSeconds
	}
	maxEntry := c.maxEntryBytes
	if effPolicy.ResponseCacheMaxEntryBytes != nil && *effPolicy.ResponseCacheMaxEntryBytes > 0 && *effPolicy.ResponseCacheMaxEntryBytes < maxEntry {
		maxEntry = *effPolicy.ResponseCacheMaxEntryBytes
	}
	return EligibilityResult{
		Eligible:       true,
		CacheKeySHA256: keySHA,
		TTLSeconds:     ttlSec,
		MaxEntryBytes:  maxEntry,
	}, nil
}

// ComputePolicyRevision computes a deterministic fingerprint of an EffectivePolicy for cache-key isolation.
func ComputePolicyRevision(eff engine.EffectivePolicy) string {
	doc := map[string]any{
		"has_allowed_models":     eff.HasAllowedModels,
		"allowed_models":         eff.AllowedModels,
		"denied_models":          eff.DeniedModels,
		"has_allowed_adapters":   eff.HasAllowedAdapters,
		"allowed_adapters":       eff.AllowedAdapters,
		"denied_adapters":        eff.DeniedAdapters,
		"has_allowed_regions":    eff.HasAllowedDataRegions,
		"allowed_regions":        eff.AllowedDataRegions,
		"min_trust":              eff.MinPluginTrust,
		"min_assurance":          eff.MinCapabilityAssurance,
		"max_context":            eff.MaxContextTokens,
		"max_body":               eff.MaxRequestBodyBytes,
		"rpm_limit":              eff.RPMLimit,
		"tpm_limit":              eff.TPMLimit,
		"max_concurrent_streams": eff.MaxConcurrentStreams,
		"has_response_cache":     eff.HasResponseCache,
		"cache_enabled":          eff.ResponseCacheEnabled,
		"cache_ttl_seconds":      eff.ResponseCacheTTLSeconds,
		"cache_max_entry_bytes":  eff.ResponseCacheMaxEntryBytes,
		"guardrail_hooks":        eff.GuardrailHooks,
	}
	jcs, err := audit.CanonicalizeValue(doc)
	if err != nil {
		raw, _ := json.Marshal(doc)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(jcs)
	return hex.EncodeToString(sum[:])
}

// ComputeCacheKeySHA256 implements CR-09 / FMEA-03 canonical cache key hashing:
// SHA-256(JCS({ org_id, project_id, env_id, policy_revision, model_deployment_id, tools_schema_hash, canonical_request })).
func ComputeCacheKeySHA256(in KeyInput) (string, error) {
	canonReqBytes, err := audit.CanonicalizeJSON(in.RequestBody)
	if err != nil {
		return "", fmt.Errorf("cache: canonicalize request JSON: %w", err)
	}
	var reqMap map[string]any
	dec := json.NewDecoder(bytes.NewReader(canonReqBytes))
	dec.UseNumber()
	if err := dec.Decode(&reqMap); err != nil || reqMap == nil {
		return "", fmt.Errorf("cache: decode canonical request object: %w", err)
	}

	toolsDoc := map[string]any{
		"tools":       reqMap["tools"],
		"tool_choice": reqMap["tool_choice"],
	}
	canonToolsBytes, err := audit.CanonicalizeValue(toolsDoc)
	if err != nil {
		return "", fmt.Errorf("cache: canonicalize tools schema: %w", err)
	}
	toolsSum := sha256.Sum256(canonToolsBytes)

	envelope := map[string]any{
		"org_id":              strings.TrimSpace(in.OrgID),
		"project_id":          strings.TrimSpace(in.ProjectID),
		"env_id":              strings.TrimSpace(in.EnvID),
		"policy_revision":     strings.TrimSpace(in.PolicyRevision),
		"model_deployment_id": strings.TrimSpace(in.ModelDeploymentID),
		"tools_schema_hash":   hex.EncodeToString(toolsSum[:]),
		"canonical_request":   reqMap,
	}
	jcsKeyDoc, err := audit.CanonicalizeValue(envelope)
	if err != nil {
		return "", fmt.Errorf("cache: canonicalize key document: %w", err)
	}
	keySum := sha256.Sum256(jcsKeyDoc)
	return hex.EncodeToString(keySum[:]), nil
}

// IsCacheableResponse enforces FMEA-03 anti-cache-poisoning invariants:
// rejects non-200 HTTP status, oversized/malformed bodies, error envelopes, and responses containing tool calls.
func IsCacheableResponse(httpStatus int, body []byte, maxEntryBytes int64) bool {
	if httpStatus != http.StatusOK || len(body) == 0 {
		return false
	}
	if maxEntryBytes > 0 && int64(len(body)) > maxEntryBytes {
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if !bytes.HasPrefix(trimmed, []byte("{")) || !bytes.HasSuffix(trimmed, []byte("}")) {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
		return false
	}
	if _, hasErr := obj["error"]; hasErr {
		return false
	}
	if choices, ok := obj["choices"].([]any); ok {
		for _, ch := range choices {
			cm, ok := ch.(map[string]any)
			if !ok {
				continue
			}
			if msg, ok := cm["message"].(map[string]any); ok {
				if tc, hasTC := msg["tool_calls"].([]any); hasTC && len(tc) > 0 {
					return false
				}
				if fc, hasFC := msg["function_call"]; hasFC && fc != nil {
					return false
				}
			}
		}
	}
	// Reject OpenAI Responses API or Anthropic-style tool invocations.
	if outArr, ok := obj["output"].([]any); ok {
		for _, item := range outArr {
			if im, ok := item.(map[string]any); ok {
				if t, _ := im["type"].(string); strings.Contains(t, "call") || strings.Contains(t, "tool") {
					return false
				}
			}
		}
	}
	if contentArr, ok := obj["content"].([]any); ok {
		for _, block := range contentArr {
			if bm, ok := block.(map[string]any); ok {
				if t, _ := bm["type"].(string); t == "tool_use" || t == "server_tool_use" {
					return false
				}
			}
		}
	}
	return true
}

// Lookup retrieves and Project-DEK decrypts a non-expired cached response entry strictly scoped by (orgID, projectID, envID, cacheKeySHA256).
func (c *ExactCache) Lookup(ctx context.Context, orgID, projectID, envID, cacheKeySHA256 string) ([]byte, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(envID) == "" || strings.TrimSpace(cacheKeySHA256) == "" {
		return nil, ErrCacheMiss
	}
	now := c.nowMs()

	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return nil, err
	}

	var aeadAlg string
	var nonce, ciphertext []byte
	q := port.Rebind(c.dialect, `
		SELECT aead_alg, nonce, ciphertext
		FROM response_cache_entries
		WHERE org_id = ? AND project_id = ? AND env_id = ? AND cache_key_sha256 = ? AND expires_at_ms > ?`)
	err = tx.QueryRowContext(ctx, q, orgID, projectID, envID, cacheKeySHA256, now).Scan(&aeadAlg, &nonce, &ciphertext)
	_ = tx.Rollback()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	if aeadAlg != "AES-256-GCM" {
		return nil, fmt.Errorf("%w: unsupported cache aead_alg %q", ErrUndecryptable, aeadAlg)
	}

	plaintext, uerr := c.vault.UnsealProjectBlob(ctx, orgID, projectID, vault.RecordKindCacheEntry, cacheKeySHA256, nonce, ciphertext)
	if uerr != nil {
		if errors.Is(uerr, vault.ErrDecryptionFailed) || errors.Is(uerr, port.ErrNotFound) || errors.Is(uerr, vault.ErrProjectExternalKeyUnavailable) {
			return nil, fmt.Errorf("%w: %v", ErrUndecryptable, uerr)
		}
		return nil, uerr
	}
	return plaintext, nil
}

// Store encrypts responseBody under the tenant's Project DEK (RecordKindCacheEntry) and persists it in response_cache_entries.
func (c *ExactCache) Store(ctx context.Context, orgID, projectID, envID, modelDeploymentID, cacheKeySHA256 string, responseBody []byte, ttlSeconds int64) error {
	if len(responseBody) == 0 || int64(len(responseBody)) > c.maxEntryBytes {
		return nil
	}
	if ttlSeconds <= 0 {
		ttlSeconds = defaultCacheTTLSeconds
	}
	nonce, ciphertext, err := c.vault.SealProjectBlob(ctx, orgID, projectID, vault.RecordKindCacheEntry, cacheKeySHA256, responseBody)
	if err != nil {
		return err
	}
	now := c.nowMs()
	expiresAt := now + ttlSeconds*1000

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := port.SetOrgRLSTx(ctx, tx, c.dialect, orgID); err != nil {
		return err
	}
	q := port.Rebind(c.dialect, `
		INSERT INTO response_cache_entries (
			org_id, project_id, env_id, cache_key_sha256, model_deployment_id,
			aead_alg, nonce, ciphertext, created_at_ms, expires_at_ms
		) VALUES (?, ?, ?, ?, ?, 'AES-256-GCM', ?, ?, ?, ?)
		ON CONFLICT (org_id, project_id, cache_key_sha256) DO UPDATE SET
			env_id = EXCLUDED.env_id,
			model_deployment_id = EXCLUDED.model_deployment_id,
			nonce = EXCLUDED.nonce,
			ciphertext = EXCLUDED.ciphertext,
			created_at_ms = EXCLUDED.created_at_ms,
			expires_at_ms = EXCLUDED.expires_at_ms`)
	if _, err := tx.ExecContext(ctx, q, orgID, projectID, envID, cacheKeySHA256, modelDeploymentID, nonce, ciphertext, now, expiresAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func hasNoStoreCacheControl(headers http.Header) bool {
	if headers == nil {
		return false
	}
	for _, raw := range headers.Values("Cache-Control") {
		for _, part := range strings.Split(raw, ",") {
			dir := strings.ToLower(strings.TrimSpace(part))
			if dir == "no-store" || dir == "no-cache" {
				return true
			}
		}
	}
	return false
}

func isExactZeroJSONNumber(v any) bool {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return err == nil && f == 0.0
	case float64:
		return n == 0.0
	case int64:
		return n == 0
	case int:
		return n == 0
	default:
		return false
	}
}
