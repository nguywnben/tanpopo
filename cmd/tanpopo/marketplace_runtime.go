// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	marketplaceIndexURLEnv      = "TANPOPO_MARKETPLACE_INDEX_URL"
	marketplaceRevocationsURLEnv = "TANPOPO_MARKETPLACE_REVOCATIONS_URL"
	marketplaceFeedKeysEnv      = "TANPOPO_MARKETPLACE_FEED_PUBLIC_KEYS"
)

func startMarketplaceRuntime(ctx context.Context, store *registry.SQLStore, lifecycle *installer.LifecycleService, snaps *snapshot.Manager, audit port.AuditSink) <-chan struct{} {
	done := make(chan struct{})
	keys, keyErr := configuredMarketplaceKeyring(os.Getenv(marketplaceFeedKeysEnv))
	indexURL := strings.TrimSpace(os.Getenv(marketplaceIndexURLEnv))
	revocationsURL := strings.TrimSpace(os.Getenv(marketplaceRevocationsURLEnv))
	if indexURL == "" && revocationsURL == "" {
		close(done)
		return done
	}
	if keyErr != nil {
		log.Printf("marketplace feed polling disabled: invalid public key configuration: %v", keyErr)
		close(done)
		return done
	}
	indexClient, indexErr := guardedFeedClient(indexURL)
	if indexURL != "" && indexErr != nil {
		log.Printf("marketplace index polling disabled: invalid feed URL: %v", indexErr)
		indexURL = ""
	}
	revocationClient, revocationErr := guardedFeedClient(revocationsURL)
	if revocationsURL != "" && revocationErr != nil {
		log.Printf("marketplace revocation polling disabled: invalid feed URL: %v", revocationErr)
		revocationsURL = ""
	}
	if indexURL == "" && revocationsURL == "" {
		close(done)
		return done
	}
	var indexSyncer *registry.IndexSyncer
	if indexURL != "" {
		indexSyncer = &registry.IndexSyncer{Client: indexClient, URL: indexURL, Keys: keys, Store: store, Now: time.Now}
	}
	var revocationWatcher *registry.RevocationWatcher
	if revocationsURL != "" {
		revocationWatcher = &registry.RevocationWatcher{
			Client: revocationClient, URL: revocationsURL, Keys: keys, Store: store, Now: time.Now,
			Actions: runtimeRevocationActions{installer: lifecycle, snapshots: snaps, audit: audit},
		}
	}
	go func() {
		defer close(done)
		run := func() {
			if indexSyncer != nil {
				if _, err := indexSyncer.Sync(ctx); err != nil {
					log.Printf("marketplace index sync failed; serving verified cache when available: %v", err)
				}
			}
			if revocationWatcher != nil {
				if err := revocationWatcher.Poll(ctx); err != nil {
					log.Printf("marketplace revocation poll failed; pending actions remain durable: %v", err)
				}
			}
		}
		run()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
	return done
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

func guardedFeedClient(rawURL string) (*http.Client, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("feed URL must use HTTPS and contain no credentials or fragment")
	}
	port := 443
	if rawPort := u.Port(); rawPort != "" {
		port, err = strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("feed URL has an invalid port")
		}
	}
	return egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy:                   egress.Policy{AllowedHosts: []string{u.Hostname()}, AllowedPorts: []int{port}, RequireTLS: true},
		Resolver:                 egress.NewStdHostResolver(),
		Timeout:                  30 * time.Second,
		MaxRedirects:             0,
		DenyCrossOriginRedirects: true,
	})
}

type runtimeRevocationActions struct {
	installer *installer.LifecycleService
	snapshots *snapshot.Manager
	audit     port.AuditSink
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
