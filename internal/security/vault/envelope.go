// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// MaxCacheTTL is the hard security ceiling on cached in-memory DEKs (SPEC-SEC-03).
	MaxCacheTTL = 5 * time.Minute
	// RecordKindCred identifies project-scoped secret_credentials records under ProjectRecordAAD.
	RecordKindCred = "cred"
	// RecordKindDebugBlob identifies opt-in debug_payload_blobs records under ProjectRecordAAD.
	RecordKindDebugBlob = "debug_blob"
	// RecordKindCacheEntry identifies opt-in response_cache_entries records under ProjectRecordAAD.
	RecordKindCacheEntry = "cache_entry"
)

var (
	// ErrInvalidInput indicates malformed vault arguments or unsupported secret types/TTLs.
	ErrInvalidInput = errors.New("vault: invalid input")
	// ErrDecryptionFailed indicates AES-256-GCM authentication or ciphertext integrity failure.
	ErrDecryptionFailed = errors.New("vault: AEAD decryption failed")
	// ErrMissingUnsealCapability indicates UnsealWithCapability was called with an unminted/zero-value capability.
	ErrMissingUnsealCapability = errors.New("vault: unseal capability required")
	// ErrPostShredCleanupFailed indicates the DEK row was deleted and cached RAM zeroized,
	// but the post-shred WAL/disk cleanup failed (e.g. busy reader) or has not yet completed;
	// callers must retry shred / FlushPendingShredCleanups to complete physical erasure.
	ErrPostShredCleanupFailed = errors.New("vault: post-shred cleanup failed; retry shred to complete physical erasure")
	// ErrPendingShredCleanup indicates an earlier committed shred for the target scope has not yet
	// completed verified WAL/disk cleanup (pending crash recovery or retry).
	ErrPendingShredCleanup = errors.New("vault: prior shred WAL cleanup is still pending")
)

func validateAADSegment(label, val string) error {
	if strings.TrimSpace(val) == "" || strings.ContainsAny(val, ":\x00\r\n") {
		return fmt.Errorf("%w: invalid %s for AEAD AAD", ErrInvalidInput, label)
	}
	return nil
}

// OrgSecretAAD builds Canonical AAD Formula 1 (SPEC-SEC-03 / ARCH-DATA-01 Section 3.1):
// "tanpopo:v1:cred:" + org_id + ":" + credential_id + ":" + secret_kind.
func OrgSecretAAD(orgID, credentialID, secretKind string) ([]byte, error) {
	if err := validateAADSegment("org_id", orgID); err != nil {
		return nil, err
	}
	if err := validateAADSegment("credential_id", credentialID); err != nil {
		return nil, err
	}
	if err := validateAADSegment("secret_kind", secretKind); err != nil {
		return nil, err
	}
	return []byte("tanpopo:v1:cred:" + orgID + ":" + credentialID + ":" + secretKind), nil
}

// ProjectRecordAAD builds Canonical AAD Formula 2 (SPEC-SEC-03 / ARCH-DATA-01 Section 3.1):
// "tanpopo:v1:proj:" + org_id + ":" + project_id + ":" + record_kind + ":" + record_id.
func ProjectRecordAAD(orgID, projectID, recordKind, recordID string) ([]byte, error) {
	if err := validateAADSegment("org_id", orgID); err != nil {
		return nil, err
	}
	if err := validateAADSegment("project_id", projectID); err != nil {
		return nil, err
	}
	if recordKind != RecordKindCred && recordKind != RecordKindDebugBlob && recordKind != RecordKindCacheEntry {
		return nil, fmt.Errorf("%w: unsupported project AAD record_kind %q", ErrInvalidInput, recordKind)
	}
	if err := validateAADSegment("record_id", recordID); err != nil {
		return nil, err
	}
	return []byte("tanpopo:v1:proj:" + orgID + ":" + projectID + ":" + recordKind + ":" + recordID), nil
}

func projectDEKWrapAAD(orgID, projectID string, orgDEKVersion, projectDEKVersion int64) ([]byte, error) {
	if err := validateAADSegment("org_id", orgID); err != nil {
		return nil, err
	}
	if err := validateAADSegment("project_id", projectID); err != nil {
		return nil, err
	}
	if orgDEKVersion < 1 || projectDEKVersion < 1 {
		return nil, fmt.Errorf("%w: dek versions must be >= 1", ErrInvalidInput)
	}
	return []byte(fmt.Sprintf("tanpopo:v1:proj-dek:%s:%s:%d:%d", orgID, projectID, orgDEKVersion, projectDEKVersion)), nil
}

func generate256BitDEK() ([]byte, error) {
	dek := make([]byte, KeySizeBytes)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("vault: generate 256-bit DEK: %w", err)
	}
	if err := validate256BitKey(dek); err != nil {
		clear(dek)
		return nil, err
	}
	return dek, nil
}

func computeKeyFingerprint(orgID, secretID string, plaintext []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte("tanpopo:v1:fingerprint:"))
	_, _ = h.Write([]byte(orgID))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(secretID))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write(plaintext)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

type orgDEKCacheEntry struct {
	kekKeyID    string
	dekVersion  int64
	dek         []byte
	expiresAtMs int64
}

type projectDEKCacheKey struct {
	orgID     string
	projectID string
}

type projectDEKCacheEntry struct {
	orgDEKVersion     int64
	projectDEKVersion int64
	dek               []byte
	expiresAtMs       int64
}

// dekCache holds short-lived unwrapped Org and Project DEKs in RAM (TTL <= 5m)
// and zeroizes every evicted slice immediately via clear(entry.dek).
// evictGen increments monotonically on every eviction so concurrent in-flight loads
// captured prior to a shred/rotate cannot repopulate stale key material.
type dekCache struct {
	mu       sync.RWMutex
	evictGen uint64
	ttlMs    int64
	orgDEKs  map[string]*orgDEKCacheEntry // key: orgID
	projDEKs map[projectDEKCacheKey]*projectDEKCacheEntry
}

func newDEKCache(ttl time.Duration) (*dekCache, error) {
	if ttl <= 0 {
		ttl = MaxCacheTTL
	}
	if ttl > MaxCacheTTL {
		return nil, fmt.Errorf("%w: DEK cache TTL %v exceeds 5m security maximum", ErrInvalidInput, ttl)
	}
	return &dekCache{
		ttlMs:    ttl.Milliseconds(),
		orgDEKs:  make(map[string]*orgDEKCacheEntry),
		projDEKs: make(map[projectDEKCacheKey]*projectDEKCacheEntry),
	}, nil
}

func (c *dekCache) generation() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictGen
}

func (c *dekCache) getOrgDEK(orgID string, nowMs int64) (kekKeyID string, dekVersion int64, dekCopy []byte, ok bool) {
	c.mu.RLock()
	entry := c.orgDEKs[orgID]
	if entry == nil {
		c.mu.RUnlock()
		return "", 0, nil, false
	}
	if nowMs >= entry.expiresAtMs {
		c.mu.RUnlock()
		c.evictExpiredOrg(orgID, nowMs)
		return "", 0, nil, false
	}
	cp := make([]byte, len(entry.dek))
	copy(cp, entry.dek)
	kekKeyID = entry.kekKeyID
	dekVersion = entry.dekVersion
	c.mu.RUnlock()
	return kekKeyID, dekVersion, cp, true
}

func (c *dekCache) evictExpiredOrg(orgID string, nowMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.orgDEKs[orgID]; entry != nil && nowMs >= entry.expiresAtMs {
		clear(entry.dek)
		delete(c.orgDEKs, orgID)
	}
}

func (c *dekCache) putOrgDEKIfGen(orgID, kekKeyID string, dekVersion int64, dek []byte, nowMs int64, expectedGen uint64) bool {
	cp := make([]byte, len(dek))
	copy(cp, dek)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.evictGen != expectedGen {
		clear(cp)
		return false
	}
	if old := c.orgDEKs[orgID]; old != nil {
		clear(old.dek)
	}
	c.orgDEKs[orgID] = &orgDEKCacheEntry{
		kekKeyID:    kekKeyID,
		dekVersion:  dekVersion,
		dek:         cp,
		expiresAtMs: nowMs + c.ttlMs,
	}
	return true
}

func (c *dekCache) getProjectDEK(orgID, projectID string, nowMs int64) (orgDEKVersion, projectDEKVersion int64, dekCopy []byte, ok bool) {
	key := projectDEKCacheKey{orgID: orgID, projectID: projectID}
	c.mu.RLock()
	entry := c.projDEKs[key]
	if entry == nil {
		c.mu.RUnlock()
		return 0, 0, nil, false
	}
	if nowMs >= entry.expiresAtMs {
		c.mu.RUnlock()
		c.evictExpiredProject(key, nowMs)
		return 0, 0, nil, false
	}
	cp := make([]byte, len(entry.dek))
	copy(cp, entry.dek)
	orgV := entry.orgDEKVersion
	projV := entry.projectDEKVersion
	c.mu.RUnlock()
	return orgV, projV, cp, true
}

func (c *dekCache) evictExpiredProject(key projectDEKCacheKey, nowMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.projDEKs[key]; entry != nil && nowMs >= entry.expiresAtMs {
		clear(entry.dek)
		delete(c.projDEKs, key)
	}
}

func (c *dekCache) putProjectDEKIfGen(orgID, projectID string, orgDEKVersion, projectDEKVersion int64, dek []byte, nowMs int64, expectedGen uint64) bool {
	cp := make([]byte, len(dek))
	copy(cp, dek)
	key := projectDEKCacheKey{orgID: orgID, projectID: projectID}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.evictGen != expectedGen {
		clear(cp)
		return false
	}
	if old := c.projDEKs[key]; old != nil {
		clear(old.dek)
	}
	c.projDEKs[key] = &projectDEKCacheEntry{
		orgDEKVersion:     orgDEKVersion,
		projectDEKVersion: projectDEKVersion,
		dek:               cp,
		expiresAtMs:       nowMs + c.ttlMs,
	}
	return true
}

// evictProject zeroizes and deletes a single project's cached DEK.
func (c *dekCache) evictProject(orgID, projectID string) {
	key := projectDEKCacheKey{orgID: orgID, projectID: projectID}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictGen++
	if entry := c.projDEKs[key]; entry != nil {
		clear(entry.dek)
		delete(c.projDEKs, key)
	}
}

// evictOrg zeroizes and deletes an organization's Org DEK and all of its child Project DEKs.
func (c *dekCache) evictOrg(orgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictGen++
	if entry := c.orgDEKs[orgID]; entry != nil {
		clear(entry.dek)
		delete(c.orgDEKs, orgID)
	}
	for k, entry := range c.projDEKs {
		if k.orgID == orgID {
			clear(entry.dek)
			delete(c.projDEKs, k)
		}
	}
}

// evictByKEKID zeroizes and deletes all cached Org DEKs wrapped under oldKEKID.
func (c *dekCache) evictByKEKID(kekKeyID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictGen++
	for orgID, entry := range c.orgDEKs {
		if entry.kekKeyID == kekKeyID {
			clear(entry.dek)
			delete(c.orgDEKs, orgID)
		}
	}
}

// clearAll zeroizes every cached DEK slice in RAM.
func (c *dekCache) clearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictGen++
	for orgID, entry := range c.orgDEKs {
		clear(entry.dek)
		delete(c.orgDEKs, orgID)
	}
	for k, entry := range c.projDEKs {
		clear(entry.dek)
		delete(c.projDEKs, k)
	}
}
