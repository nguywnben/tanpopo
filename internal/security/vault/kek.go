// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package vault implements Tanpopo's three-tier AES-256-GCM context-bound
// envelope encryption vault (Master KEK -> Org DEK -> Project DEK),
// short-lived RAM DEK caching with explicit clear(buf) zeroization,
// write-only SecretMetadata DTOs, and capability-gated UnsealWithCapability.
//
// DAG Boundary (Tier 3): May import stdlib, internal/pkg, and internal/storage/*.
package vault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
)

const (
	// EnvMasterKey is the canonical environment variable holding a 256-bit Master KEK
	// (64 hex characters or 43/44 base64 characters).
	EnvMasterKey = "TANPOPO_MASTER_KEY"
	// EnvMasterKeyFile optionally references a local 0400/0600 file containing the 256-bit Master KEK.
	EnvMasterKeyFile = "TANPOPO_MASTER_KEY_FILE"

	// KeySizeBytes is the required symmetric AES-256 key length (32 bytes / 256 bits).
	KeySizeBytes = 32
	// NonceSizeBytes is the required AES-256-GCM nonce length (12 bytes / 96 bits).
	NonceSizeBytes = 12
)

var (
	// ErrInvalidMasterKey indicates a missing, malformed, wrong-length, or all-zero Master KEK.
	ErrInvalidMasterKey = errors.New("vault: invalid 256-bit master key")
	// ErrInsecureKeyfilePermissions indicates a local Master KEK file grants group/other permissions or is a symlink.
	ErrInsecureKeyfilePermissions = errors.New("vault: master key file must be a regular non-symlink file with 0400/0600 permissions")
	// ErrKEKNotFound indicates a requested KEK identifier is not loaded in the provider.
	ErrKEKNotFound = errors.New("vault: master key ID not found")
)

// KEKProvider abstracts root key wrapping/unwrapping for static keys and future KMS/Vault adapters.
type KEKProvider interface {
	ActiveKeyID(ctx context.Context) (string, error)
	WrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, plaintextDEK []byte) (nonce, ciphertext []byte, err error)
	UnwrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, nonce, ciphertext []byte) ([]byte, error)
}

// StaticKEKProvider manages one active and zero or more staged/previous 256-bit Master KEKs in memory.
type StaticKEKProvider struct {
	mu          sync.RWMutex
	activeKeyID string
	keys        map[string][]byte
}

// NewStaticKEKProvider constructs a fail-closed StaticKEKProvider requiring a valid 32-byte non-zero key.
func NewStaticKEKProvider(keyID string, rawKey []byte) (*StaticKEKProvider, error) {
	if err := validateValidKeyID(keyID); err != nil {
		return nil, err
	}
	if err := validate256BitKey(rawKey); err != nil {
		return nil, err
	}
	cp := make([]byte, KeySizeBytes)
	copy(cp, rawKey)
	return &StaticKEKProvider{
		activeKeyID: keyID,
		keys: map[string][]byte{
			keyID: cp,
		},
	}, nil
}

// RegisterKey adds an additional KEK version (such as a staged rotation target kek_v2 or legacy kek_v1).
// When setActive is true, activeKeyID updates to keyID for newly minted Org DEKs.
func (p *StaticKEKProvider) RegisterKey(keyID string, rawKey []byte, setActive bool) error {
	if err := validateValidKeyID(keyID); err != nil {
		return err
	}
	if err := validate256BitKey(rawKey); err != nil {
		return err
	}
	cp := make([]byte, KeySizeBytes)
	copy(cp, rawKey)

	p.mu.Lock()
	defer p.mu.Unlock()
	if old, exists := p.keys[keyID]; exists {
		clear(old)
	}
	p.keys[keyID] = cp
	if setActive {
		p.activeKeyID = keyID
	}
	return nil
}

// RetireKey scrubs and removes an old KEK version from memory once Data Plane cache eviction completes.
func (p *StaticKEKProvider) RetireKey(keyID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if keyID == p.activeKeyID {
		return fmt.Errorf("%w: cannot retire active master key %q", ErrInvalidMasterKey, keyID)
	}
	if old, exists := p.keys[keyID]; exists {
		clear(old)
		delete(p.keys, keyID)
	}
	return nil
}

// Close scrubs all Master KEK material from RAM.
func (p *StaticKEKProvider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, k := range p.keys {
		clear(k)
		delete(p.keys, id)
	}
	p.activeKeyID = ""
}

// ActiveKeyID returns the currently active Master KEK identifier.
func (p *StaticKEKProvider) ActiveKeyID(context.Context) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.activeKeyID == "" {
		return "", ErrInvalidMasterKey
	}
	return p.activeKeyID, nil
}

// WrapDEK seals a 256-bit Org DEK under the named Master KEK binding orgID and dekVersion in AAD.
func (p *StaticKEKProvider) WrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, plaintextDEK []byte) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := validate256BitKey(plaintextDEK); err != nil {
		return nil, nil, fmt.Errorf("vault: invalid org DEK plaintext: %w", err)
	}
	aad, err := orgDEKWrapAAD(kekKeyID, orgID, dekVersion)
	if err != nil {
		return nil, nil, err
	}
	kekCopy, err := p.copyKey(kekKeyID)
	if err != nil {
		return nil, nil, err
	}
	defer clear(kekCopy)

	return sealAESGCM(kekCopy, aad, plaintextDEK)
}

// UnwrapDEK decrypts a wrapped 256-bit Org DEK under kekKeyID verifying its exact orgID/dekVersion AAD.
func (p *StaticKEKProvider) UnwrapDEK(ctx context.Context, kekKeyID, orgID string, dekVersion int64, nonce, ciphertext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	aad, err := orgDEKWrapAAD(kekKeyID, orgID, dekVersion)
	if err != nil {
		return nil, err
	}
	kekCopy, err := p.copyKey(kekKeyID)
	if err != nil {
		return nil, err
	}
	defer clear(kekCopy)

	out, err := openAESGCM(kekCopy, aad, nonce, ciphertext)
	if err != nil {
		return nil, err
	}
	if err := validate256BitKey(out); err != nil {
		clear(out)
		return nil, err
	}
	return out, nil
}

func (p *StaticKEKProvider) copyKey(keyID string) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	raw, ok := p.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrKEKNotFound, keyID)
	}
	cp := make([]byte, len(raw))
	copy(cp, raw)
	return cp, nil
}

// ParseMasterKeyString decodes a 256-bit Master KEK from 64 hex characters or base64 encoding.
func ParseMasterKeyString(encoded string) ([]byte, error) {
	return parseMasterKeyBytes([]byte(encoded))
}

func parseMasterKeyBytes(rawEncoded []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(rawEncoded)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: master key value is empty", ErrInvalidMasterKey)
	}
	if len(trimmed) == KeySizeBytes*2 {
		decoded := make([]byte, KeySizeBytes)
		if n, err := hex.Decode(decoded, trimmed); err == nil && n == KeySizeBytes {
			if verr := validate256BitKey(decoded); verr != nil {
				clear(decoded)
				return nil, verr
			}
			return decoded, nil
		}
		clear(decoded)
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded := make([]byte, enc.DecodedLen(len(trimmed)))
		n, err := enc.Decode(decoded, trimmed)
		if err == nil && n == KeySizeBytes {
			key := decoded[:n]
			if verr := validate256BitKey(key); verr != nil {
				clear(decoded)
				return nil, verr
			}
			return key, nil
		}
		clear(decoded)
	}
	return nil, fmt.Errorf("%w: expected 32-byte key (64 hex or base64)", ErrInvalidMasterKey)
}

// LoadStaticKEKFromEnv loads a 256-bit Master KEK from TANPOPO_MASTER_KEY or falls back to
// a 0400/0600 file at TANPOPO_MASTER_KEY_FILE. Passing nil lookupEnv defaults to os.LookupEnv.
func LoadStaticKEKFromEnv(keyID string, lookupEnv func(string) (string, bool)) (*StaticKEKProvider, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if rawVal, ok := lookupEnv(EnvMasterKey); ok && strings.TrimSpace(rawVal) != "" {
		rawKey, err := ParseMasterKeyString(rawVal)
		if err != nil {
			return nil, err
		}
		defer clear(rawKey)
		return NewStaticKEKProvider(keyID, rawKey)
	}
	if pathVal, ok := lookupEnv(EnvMasterKeyFile); ok && strings.TrimSpace(pathVal) != "" {
		return LoadStaticKEKFromFile(keyID, strings.TrimSpace(pathVal))
	}
	return nil, fmt.Errorf("%w: neither %s nor %s is configured", ErrInvalidMasterKey, EnvMasterKey, EnvMasterKeyFile)
}

// LoadStaticKEKFromFile reads a 256-bit Master KEK from a local non-symlink file enforcing
// 0400/0600 permissions on POSIX systems and avoiding Lstat-to-Read TOCTOU or heap string copies.
func LoadStaticKEKFromFile(keyID, filePath string) (*StaticKEKProvider, error) {
	if strings.TrimSpace(filePath) == "" || strings.ContainsRune(filePath, 0) {
		return nil, fmt.Errorf("%w: empty or NUL key file path", ErrInvalidMasterKey)
	}
	linfo, err := os.Lstat(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: inspect key file: %v", ErrInvalidMasterKey, err)
	}
	if linfo.Mode()&os.ModeSymlink != 0 || !linfo.Mode().IsRegular() {
		return nil, ErrInsecureKeyfilePermissions
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: open key file: %v", ErrInvalidMasterKey, err)
	}
	defer func() { _ = f.Close() }()

	finfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: stat open key file: %v", ErrInvalidMasterKey, err)
	}
	if !os.SameFile(linfo, finfo) || !finfo.Mode().IsRegular() {
		return nil, ErrInsecureKeyfilePermissions
	}
	if err := checkPOSIXKeyfileMode(runtime.GOOS, finfo.Mode().Perm()); err != nil {
		return nil, err
	}
	// Bound key file read size to prevent OOM on malformed files (1 KiB max for 64 hex / b64 / 32 raw bytes).
	rawBytes, err := io.ReadAll(io.LimitReader(f, 1024))
	if err != nil {
		return nil, fmt.Errorf("%w: read key file: %v", ErrInvalidMasterKey, err)
	}
	defer clear(rawBytes)

	if len(rawBytes) == KeySizeBytes {
		if verr := validate256BitKey(rawBytes); verr != nil {
			return nil, verr
		}
		return NewStaticKEKProvider(keyID, rawBytes)
	}
	parsed, perr := parseMasterKeyBytes(rawBytes)
	if perr != nil {
		return nil, perr
	}
	defer clear(parsed)
	return NewStaticKEKProvider(keyID, parsed)
}

func checkPOSIXKeyfileMode(goos string, perm os.FileMode) error {
	if goos != "windows" && perm&0o177 != 0 {
		return fmt.Errorf("%w: mode %04o grants disallowed permissions", ErrInsecureKeyfilePermissions, perm)
	}
	return nil
}

func validateValidKeyID(keyID string) error {
	if strings.TrimSpace(keyID) == "" || strings.ContainsAny(keyID, ":\x00 \t\r\n") || len(keyID) > 128 {
		return fmt.Errorf("%w: invalid kek_key_id %q", ErrInvalidMasterKey, keyID)
	}
	return nil
}

func validate256BitKey(key []byte) error {
	if len(key) != KeySizeBytes {
		return fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidMasterKey, KeySizeBytes, len(key))
	}
	var zero [KeySizeBytes]byte
	if subtle.ConstantTimeCompare(key, zero[:]) == 1 {
		return fmt.Errorf("%w: all-zero key rejected", ErrInvalidMasterKey)
	}
	return nil
}

func orgDEKWrapAAD(kekKeyID, orgID string, dekVersion int64) ([]byte, error) {
	if err := validateValidKeyID(kekKeyID); err != nil {
		return nil, err
	}
	if err := validateAADSegment("org_id", orgID); err != nil {
		return nil, err
	}
	if dekVersion < 1 {
		return nil, fmt.Errorf("%w: dek_version must be >= 1", ErrInvalidInput)
	}
	return []byte(fmt.Sprintf("tanpopo:v1:org-dek:%s:%s:%d", kekKeyID, orgID, dekVersion)), nil
}

func sealAESGCM(key, aad, plaintext []byte) ([]byte, []byte, error) {
	if err := validate256BitKey(key); err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("vault: init AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("vault: init GCM: %w", err)
	}
	nonce := make([]byte, NonceSizeBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("vault: generate nonce: %w", err)
	}
	var zeroNonce [NonceSizeBytes]byte
	if subtle.ConstantTimeCompare(nonce, zeroNonce[:]) == 1 {
		return nil, nil, errors.New("vault: degenerate zero nonce generated")
	}
	ct := aead.Seal(nil, nonce, plaintext, aad)
	return nonce, ct, nil
}

func openAESGCM(key, aad, nonce, ciphertext []byte) ([]byte, error) {
	if err := validate256BitKey(key); err != nil {
		return nil, err
	}
	if len(nonce) != NonceSizeBytes || len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: invalid nonce/ciphertext length", ErrDecryptionFailed)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault: init AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: init GCM: %w", err)
	}
	pt, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		// Never expose key bytes or plaintext fragments in decryption error messages (NEG-3).
		return nil, fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}
	return pt, nil
}
