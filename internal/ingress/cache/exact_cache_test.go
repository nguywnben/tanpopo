// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package cache

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestExactCacheTenantIsolationDefaultDisabledAndCryptoShredAC3(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "cache.db")})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	masterRaw := sha256.Sum256([]byte("cache-master-kek"))
	kek, err := vault.NewStaticKEKProvider("kek_cache_v1", masterRaw[:])
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	vs, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}

	ec, err := NewExactCache(Config{
		DB:    st.RawDB(),
		Vault: vs,
		NowMs: func() int64 { return 1760000000000 },
	})
	if err != nil {
		t.Fatalf("NewExactCache: %v", err)
	}

	orgA, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "cache-org-a", DisplayName: "Cache Org A"})
	orgB, _ := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "cache-org-b", DisplayName: "Cache Org B"})
	pA1, _ := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p1", DisplayName: "P1"})
	pA2, _ := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "p2", DisplayName: "P2"})
	pB1, _ := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "pb", DisplayName: "PB"})
	eA1, _ := st.Tenancy().CreateEnvironment(ctx, orgA.ID, pA1.ID, port.CreateEnvironmentParams{Slug: "prod"})
	eA2, _ := st.Tenancy().CreateEnvironment(ctx, orgA.ID, pA2.ID, port.CreateEnvironmentParams{Slug: "prod"})
	eB1, _ := st.Tenancy().CreateEnvironment(ctx, orgB.ID, pB1.ID, port.CreateEnvironmentParams{Slug: "prod"})

	promptQ := []byte(`{"model":"gpt-4o","temperature":0,"messages":[{"role":"user","content":"identical prompt Q"}]}`)
	ttl := int64(300)
	enabledPolicy := engine.EffectivePolicy{HasResponseCache: true, ResponseCacheEnabled: true, ResponseCacheTTLSeconds: &ttl}
	defaultPolicy := engine.EffectivePolicy{} // default disabled!

	// 1. Default disabled policy MUST NOT enable caching.
	elDefault, err := ec.EvaluateEligibility(http.Header{}, defaultPolicy, KeyInput{
		OrgID: orgA.ID, ProjectID: pA1.ID, EnvID: eA1.ID, ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	if err != nil || elDefault.Eligible {
		t.Fatalf("expected default policy to keep cache disabled, got eligible=%v err=%v", elDefault.Eligible, err)
	}

	// 2. Cache-Control: no-store MUST bypass cache.
	noStoreHdrs := http.Header{"Cache-Control": []string{"private, no-store"}}
	elNoStore, err := ec.EvaluateEligibility(noStoreHdrs, enabledPolicy, KeyInput{
		OrgID: orgA.ID, ProjectID: pA1.ID, EnvID: eA1.ID, ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	if err != nil || elNoStore.Eligible {
		t.Fatalf("expected Cache-Control: no-store bypass, got eligible=%v err=%v", elNoStore.Eligible, err)
	}

	// 3. Tool-calling or error responses MUST NOT be cached.
	if IsCacheableResponse(http.StatusInternalServerError, []byte(`{"error":"boom"}`), 1<<20) {
		t.Fatalf("500 response must never be cacheable")
	}
	if IsCacheableResponse(http.StatusOK, []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1"}]}}]}`), 1<<20) {
		t.Fatalf("tool_calls response must never be cacheable")
	}

	// 4. Org A / P1 enables caching & stores prompt Q -> same project HIT, sibling P2 or Org B -> deterministic MISS!
	elP1, err := ec.EvaluateEligibility(http.Header{}, enabledPolicy, KeyInput{
		OrgID: orgA.ID, ProjectID: pA1.ID, EnvID: eA1.ID, PolicyRevision: "rev_1", ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	if err != nil || !elP1.Eligible {
		t.Fatalf("EvaluateEligibility p1 failed: %+v err=%v", elP1, err)
	}
	cachedPayload := []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"cached P1"}}]}`)
	if !IsCacheableResponse(http.StatusOK, cachedPayload, elP1.MaxEntryBytes) {
		t.Fatalf("expected clean 200 payload to be cacheable")
	}
	if err := ec.Store(ctx, orgA.ID, pA1.ID, eA1.ID, "dpl_1", elP1.CacheKeySHA256, cachedPayload, ttl); err != nil {
		t.Fatalf("Store p1: %v", err)
	}

	// Same project HIT.
	got, err := ec.Lookup(ctx, orgA.ID, pA1.ID, eA1.ID, elP1.CacheKeySHA256)
	if err != nil || string(got) != string(cachedPayload) {
		t.Fatalf("expected P1 cache HIT, got err=%v body=%q", err, string(got))
	}

	// Byte-identical prompt from Org A / P2 and Org B / PB -> distinct key AND cross-tenant lookup MISS!
	elP2, _ := ec.EvaluateEligibility(http.Header{}, enabledPolicy, KeyInput{
		OrgID: orgA.ID, ProjectID: pA2.ID, EnvID: eA2.ID, PolicyRevision: "rev_1", ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	elPB, _ := ec.EvaluateEligibility(http.Header{}, enabledPolicy, KeyInput{
		OrgID: orgB.ID, ProjectID: pB1.ID, EnvID: eB1.ID, PolicyRevision: "rev_1", ModelDeploymentID: "dpl_1", RequestBody: promptQ,
	})
	if elP2.CacheKeySHA256 == elP1.CacheKeySHA256 || elPB.CacheKeySHA256 == elP1.CacheKeySHA256 {
		t.Fatalf("cache keys collided across tenants/projects!")
	}
	if _, err := ec.Lookup(ctx, orgA.ID, pA2.ID, eA2.ID, elP2.CacheKeySHA256); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("expected P2 MISS, got %v", err)
	}
	if _, err := ec.Lookup(ctx, orgA.ID, pA2.ID, eA2.ID, elP1.CacheKeySHA256); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("expected cross-project key lookup under P2 to MISS, got %v", err)
	}
	if _, err := ec.Lookup(ctx, orgB.ID, pB1.ID, eB1.ID, elPB.CacheKeySHA256); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("expected Org B MISS, got %v", err)
	}

	// 5. Shred Project 1 DEK -> cache rows cascaded / unseal impossible (AC-3).
	metaP1, err := vs.EnsureProjectDEK(ctx, orgA.ID, pA1.ID)
	if err != nil {
		t.Fatalf("EnsureProjectDEK: %v", err)
	}
	if err := vs.ShredProjectDEK(ctx, orgA.ID, pA1.ID, vault.ShredRequest{RequestID: "shred-cache-p1", ExpectedGeneration: metaP1.KeyGeneration}); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}
	if _, err := ec.Lookup(ctx, orgA.ID, pA1.ID, eA1.ID, elP1.CacheKeySHA256); err == nil {
		t.Fatalf("expected lookup failure after P1 Project DEK shred")
	}

	// 6. Swapping GuardrailHooks MUST change ComputePolicyRevision.
	eff1 := engine.EffectivePolicy{
		HasResponseCache:     true,
		ResponseCacheEnabled: true,
		GuardrailHooks: []snapshot.GuardrailHookSpec{
			{ExtensionID: "ext_guard_a", FailurePosture: "FAIL_CLOSED"},
		},
	}
	eff2 := engine.EffectivePolicy{
		HasResponseCache:     true,
		ResponseCacheEnabled: true,
		GuardrailHooks: []snapshot.GuardrailHookSpec{
			{ExtensionID: "ext_guard_b", FailurePosture: "FAIL_OPEN"},
		},
	}
	if ComputePolicyRevision(eff1) == ComputePolicyRevision(eff2) {
		t.Fatalf("expected distinct ComputePolicyRevision when guardrail hooks change")
	}
}
