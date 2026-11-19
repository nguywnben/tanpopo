// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	marketplaceIndexURLEnv       = "TANPOPO_MARKETPLACE_INDEX_URL"
	marketplaceRevocationsURLEnv = "TANPOPO_MARKETPLACE_REVOCATIONS_URL"
	marketplaceFeedKeysEnv       = "TANPOPO_MARKETPLACE_FEED_PUBLIC_KEYS"
)

func startMarketplaceRuntime(ctx context.Context, store *registry.SQLStore, lifecycle *installer.LifecycleService, snaps *snapshot.Manager, audit port.AuditSink) <-chan struct{} {
	done := make(chan struct{})
	keys, keyErr := configuredMarketplaceKeyring(os.Getenv(marketplaceFeedKeysEnv))
	indexURL := strings.TrimSpace(os.Getenv(marketplaceIndexURLEnv))
	revocationsURL := strings.TrimSpace(os.Getenv(marketplaceRevocationsURLEnv))
	pollFeeds := keyErr == nil && (indexURL != "" || revocationsURL != "")
	if keyErr != nil && (indexURL != "" || revocationsURL != "") {
		log.Printf("marketplace feed polling disabled: invalid public key configuration: %v", keyErr)
	}
	var indexSyncer *registry.IndexSyncer
	if pollFeeds && indexURL != "" {
		indexSyncer = &registry.IndexSyncer{URL: indexURL, Keys: keys, Store: store, Now: time.Now}
	}
	revocationWatcher := &registry.RevocationWatcher{
		URL: revocationsURL, Keys: keys, Store: store, Now: time.Now,
		Actions: runtimeRevocationActions{installer: lifecycle, snapshots: snaps, audit: audit},
	}
	go func() {
		defer close(done)
		runMarketplaceRuntime(ctx, indexSyncer, revocationWatcher, pollFeeds)
	}()
	return done
}

func runMarketplaceRuntime(ctx context.Context, indexSyncer *registry.IndexSyncer, revocationWatcher *registry.RevocationWatcher, pollFeeds bool) {
	runMarketplaceRuntimeWithInterval(ctx, indexSyncer, revocationWatcher, pollFeeds, 10*time.Minute)
}

func runMarketplaceRuntimeWithInterval(ctx context.Context, indexSyncer *registry.IndexSyncer, revocationWatcher *registry.RevocationWatcher, pollFeeds bool, interval time.Duration) {
	run := func() {
		if err := revocationWatcher.Reconcile(ctx); err != nil {
			log.Printf("marketplace revocation reconciliation failed; pending actions remain durable: %v", err)
		}
		if !pollFeeds {
			return
		}
		if indexSyncer != nil {
			if _, err := indexSyncer.Sync(ctx); err != nil {
				log.Printf("marketplace index sync failed; serving verified cache when available: %v", err)
			}
		}
		if revocationWatcher.URL != "" {
			if err := revocationWatcher.Poll(ctx); err != nil {
				log.Printf("marketplace revocation poll failed; pending actions remain durable: %v", err)
			}
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func configuredMarketplaceKeyring(raw string) (registry.Keyring, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("public signing keys are required")
	}
	var encoded map[string]string
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&encoded); err != nil || len(encoded) == 0 {
		return nil, errors.New("expected a non-empty JSON object of key IDs to base64 public keys")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("trailing JSON value")
	}
	keys := make(registry.Keyring, len(encoded))
	for id, value := range encoded {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if strings.TrimSpace(id) == "" || err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid Ed25519 public key for key ID %q", id)
		}
		keys[id] = ed25519.PublicKey(decoded)
	}
	return keys, nil
}

type runtimeRevocationActions struct {
	installer interface {
		QuarantineInstallation(context.Context, string, string, string) error
	}
	snapshots *snapshot.Manager
	audit     port.AuditSink
}

type vaultOCIRegistryCredentialResolver struct {
	db    *sql.DB
	vault *vault.Store
	cap   vault.UnsealCapability
}

func (r vaultOCIRegistryCredentialResolver) CredentialsForRegistry(ctx context.Context, orgID, registryHost string) (*registry.RegistryCredentials, error) {
	if r.db == nil || r.vault == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(registryHost) == "" {
		return nil, errors.New("organization-scoped registry credential lookup is unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM secret_credentials
		WHERE org_id = ? AND owner_project_id IS NULL AND secret_kind = ? AND bound_audience = ?
		ORDER BY id LIMIT 2`, orgID, vault.SecretKindOCIRegistryAuth, registryHost)
	if err != nil {
		return nil, fmt.Errorf("query organization registry credentials: %w", err)
	}
	defer rows.Close()
	var secretID string
	if rows.Next() {
		if err := rows.Scan(&secretID); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if secretID == "" {
		return nil, nil
	}
	if rows.Next() {
		return nil, errors.New("multiple organization registry credentials match the same registry authority")
	}
	secret, err := r.vault.UnsealWithCapability(ctx, r.cap, orgID, secretID)
	if err != nil {
		return nil, fmt.Errorf("unseal organization registry credentials: %w", err)
	}
	defer secret.Zeroize()
	if secret.OrgID != orgID || secret.OwnerProjectID != nil || secret.SecretKind != vault.SecretKindOCIRegistryAuth || secret.BoundAudience == nil || *secret.BoundAudience != registryHost {
		return nil, errors.New("unsealed registry credential metadata does not match its organization and registry authority")
	}
	username, password, allowedCIDRs, err := parseOCIRegistryAuth(secret.Plaintext)
	if err != nil {
		return nil, errors.New("stored OCI registry credentials are malformed")
	}
	return &registry.RegistryCredentials{RegistryHost: registryHost, Username: username, Password: password, AllowedCIDRs: allowedCIDRs}, nil
}

func parseOCIRegistryAuth(raw []byte) (outUsername, outPassword []byte, allowedCIDRs []string, err error) {
	var username, password []byte
	defer func() {
		if err != nil {
			clear(username)
			clear(password)
		}
	}()
	index := skipRegistryJSONSpace(raw, 0)
	if index >= len(raw) || raw[index] != '{' {
		return nil, nil, nil, errors.New("registry credentials must be an object")
	}
	index++
	seen := map[string]bool{}
	afterComma := false
	for {
		index = skipRegistryJSONSpace(raw, index)
		if index < len(raw) && raw[index] == '}' {
			if afterComma {
				return nil, nil, nil, errors.New("trailing comma in registry credential object")
			}
			index++
			break
		}
		keyBytes, next, parseErr := readRegistryJSONString(raw, index)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		key := string(keyBytes)
		clear(keyBytes)
		index = skipRegistryJSONSpace(raw, next)
		if index >= len(raw) || raw[index] != ':' || seen[key] {
			return nil, nil, nil, errors.New("invalid registry credential field")
		}
		seen[key] = true
		afterComma = false
		switch key {
		case "username":
			value, next, parseErr := readRegistryJSONString(raw, skipRegistryJSONSpace(raw, index+1))
			if parseErr != nil {
				return nil, nil, nil, parseErr
			}
			username = value
			index = skipRegistryJSONSpace(raw, next)
		case "password":
			value, next, parseErr := readRegistryJSONString(raw, skipRegistryJSONSpace(raw, index+1))
			if parseErr != nil {
				return nil, nil, nil, parseErr
			}
			password = value
			index = skipRegistryJSONSpace(raw, next)
		case "allowed_cidrs":
			cidrs, next, parseErr := readRegistryCIDRArray(raw, skipRegistryJSONSpace(raw, index+1))
			if parseErr != nil {
				return nil, nil, nil, parseErr
			}
			allowedCIDRs = cidrs
			index = skipRegistryJSONSpace(raw, next)
		default:
			return nil, nil, nil, errors.New("unknown registry credential field")
		}
		if index < len(raw) && raw[index] == ',' {
			index++
			afterComma = true
			continue
		}
		if index < len(raw) && raw[index] == '}' {
			index++
			break
		}
		return nil, nil, nil, errors.New("malformed registry credential object")
	}
	if skipRegistryJSONSpace(raw, index) != len(raw) || len(username) == 0 || len(password) == 0 {
		clear(username)
		clear(password)
		return nil, nil, nil, errors.New("incomplete registry credentials")
	}
	return username, password, allowedCIDRs, nil
}

func readRegistryCIDRArray(raw []byte, start int) ([]string, int, error) {
	if start >= len(raw) || raw[start] != '[' {
		return nil, start, errors.New("allowed_cidrs must be an array of CIDR strings")
	}
	index := start + 1
	var cidrs []string
	afterComma := false
	for {
		index = skipRegistryJSONSpace(raw, index)
		if index < len(raw) && raw[index] == ']' {
			if afterComma {
				return nil, index, errors.New("trailing comma in allowed_cidrs")
			}
			return cidrs, index + 1, nil
		}
		value, next, err := readRegistryJSONString(raw, index)
		if err != nil {
			return nil, index, err
		}
		prefix, err := netip.ParsePrefix(string(value))
		clear(value)
		if err != nil || prefix.Bits() == 0 {
			return nil, index, errors.New("allowed_cidrs must contain valid, non-wildcard CIDRs")
		}
		cidrs = append(cidrs, prefix.Masked().String())
		afterComma = false
		index = skipRegistryJSONSpace(raw, next)
		if index < len(raw) && raw[index] == ',' {
			index++
			afterComma = true
			continue
		}
		if index < len(raw) && raw[index] == ']' {
			return cidrs, index + 1, nil
		}
		return nil, index, errors.New("malformed allowed_cidrs array")
	}
}

func readRegistryJSONString(raw []byte, start int) ([]byte, int, error) {
	if start >= len(raw) || raw[start] != '"' {
		return nil, start, errors.New("expected registry credential string")
	}
	for end := start + 1; end < len(raw); end++ {
		if raw[end] == '\\' || raw[end] < 0x20 {
			return nil, end, errors.New("escaped or control characters are not allowed in registry credentials")
		}
		if raw[end] == '"' {
			return append([]byte(nil), raw[start+1:end]...), end + 1, nil
		}
	}
	return nil, len(raw), errors.New("unterminated registry credential string")
}

func skipRegistryJSONSpace(raw []byte, index int) int {
	for index < len(raw) && (raw[index] == ' ' || raw[index] == '\n' || raw[index] == '\r' || raw[index] == '\t') {
		index++
	}
	return index
}

func (a runtimeRevocationActions) QuarantineInstallation(ctx context.Context, target registry.RevocationTarget) error {
	if a.installer == nil {
		return errors.New("extension installer is unavailable")
	}
	return a.installer.QuarantineInstallation(ctx, target.InstallationID, "signed critical revocation: "+target.Reason, "system_reaper")
}

func (a runtimeRevocationActions) SeverInstallationStreams(_ context.Context, target registry.RevocationTarget) error {
	if a.snapshots == nil || a.snapshots.Streams() == nil {
		return errors.New("active stream registry is unavailable")
	}
	a.snapshots.Streams().AbortExtension(target.InstallationID)
	return nil
}

func (a runtimeRevocationActions) EmitINC03(ctx context.Context, target registry.RevocationTarget) error {
	if a.audit == nil {
		return errors.New("audit sink is unavailable")
	}
	id, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]string{
		"incident": "INC-03", "package_slug": target.PackageSlug,
		"version_or_sha256": target.VersionOrSHA256, "severity": target.Severity,
		"reason": target.Reason,
	})
	if err != nil {
		return err
	}
	return a.audit.Append(ctx, port.AuditEvent{
		ID: id, CreatedAtMs: time.Now().UTC().UnixMilli(), ActorKind: "SYSTEM", ActorID: "system_reaper",
		EntryPoint: "system_reaper", EventType: "INC-03", TargetType: "extension_installation",
		TargetID: target.InstallationID, MetadataJSON: string(metadata),
	})
}
