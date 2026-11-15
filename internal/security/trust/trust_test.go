// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package trust_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestEvaluateOrgExtensionTrustKeyWithdrawalAndLegacyOptIn(t *testing.T) {
	t.Parallel()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fp := trust.PublicKeyFingerprint(pub)
	activeKR := trust.TrustedKeyring{
		PublisherPublicKeys: map[string]ed25519.PublicKey{fp: pub},
	}
	withdrawnKR := trust.TrustedKeyring{}

	baseIn := trust.OrgExtensionTrustInput{
		MinPluginTrust:        port.MinTrustAnyApproved,
		CanonicalName:         "@third/plugin",
		ActiveVersionID:       "exv_1",
		SignatureVerified:     true,
		SigningKeyFingerprint: fp,
		EnterpriseSigned:      false,
		OrgPolicyEnabled:      sql.NullInt64{Int64: 1, Valid: true},
		OrgOwnerOptIn:         false, // e.g., SECURITY_REVIEWER enabled before key withdrawal
	}

	// While publisher key is active: verified -> child projects do not require ORG_OWNER opt-in.
	vActive := trust.EvaluateOrgExtensionTrust(activeKR, baseIn)
	if vActive.DerivedPublicTrust != trust.TierVerified || vActive.RequiresOrgOwnerChildOptIn || vActive.MinTrustDenied {
		t.Fatalf("unexpected active keyring verdict: %+v", vActive)
	}

	// Once publisher key is withdrawn: drops to unverified_external; prior non-owner enabled=1 MUST NOT satisfy ORG_OWNER opt-in.
	vWithdrawn := trust.EvaluateOrgExtensionTrust(withdrawnKR, baseIn)
	if vWithdrawn.DerivedPublicTrust != trust.TierUnverifiedExternal || !vWithdrawn.RequiresOrgOwnerChildOptIn || vWithdrawn.MinTrustDenied {
		t.Fatalf("expected child project blocked without ORG_OWNER opt-in after key withdrawal, got %+v", vWithdrawn)
	}

	// Under VERIFIED_ONLY, withdrawing publisher key denies org and child scopes via MinTrustErr.
	verOnlyIn := baseIn
	verOnlyIn.MinPluginTrust = port.MinTrustVerifiedOnly
	vStrict := trust.EvaluateOrgExtensionTrust(withdrawnKR, verOnlyIn)
	if !vStrict.MinTrustDenied || !errors.Is(vStrict.MinTrustErr, trust.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ErrOrgTrustPolicyDenied under VERIFIED_ONLY after key withdrawal, got %+v", vStrict)
	}

	// Explicit ORG_OWNER opt-in clears child block under ANY_APPROVED.
	ownerIn := baseIn
	ownerIn.OrgOwnerOptIn = true
	vOwner := trust.EvaluateOrgExtensionTrust(withdrawnKR, ownerIn)
	if vOwner.RequiresOrgOwnerChildOptIn || !vOwner.HasValidOrgOwnerOptIn {
		t.Fatalf("expected ORG_OWNER opt-in to unlock child scope, got %+v", vOwner)
	}
}

func TestEvaluateOrgExtensionTrustEnterpriseKeyWithdrawalAndInstallStates(t *testing.T) {
	t.Parallel()
	pubPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubFP := trust.PublicKeyFingerprint(pubPub)
	entFP := trust.PublicKeyFingerprint(entPub)

	activeEnt := trust.TrustedKeyring{
		PublisherPublicKeys:  map[string]ed25519.PublicKey{"pub": pubPub},
		EnterprisePublicKeys: map[string]ed25519.PublicKey{"ent": entPub},
	}
	withdrawnEnt := trust.TrustedKeyring{
		PublisherPublicKeys: map[string]ed25519.PublicKey{"pub": pubPub},
	}

	// Single enterprise-signed package under VERIFIED_OR_ENTERPRISE_SIGNED and ANY_APPROVED.
	soleEnt := trust.OrgExtensionTrustInput{
		MinPluginTrust:           port.MinTrustVerifiedOrEnterpriseSigned,
		CanonicalName:            "@corp/ent-only",
		ActiveVersionID:          "exv_ent",
		SignatureVerified:        false,
		SigningKeyFingerprint:    strings.ToUpper(entFP), // Verify case-insensitive hex normalization.
		EnterpriseSigned:         true,
		EnterpriseKeyFingerprint: strings.ToUpper(entFP),
	}
	vOK := trust.EvaluateOrgExtensionTrust(activeEnt, soleEnt)
	if !vOK.DerivedEnterpriseSigned || vOK.MinTrustDenied || vOK.RequiresOrgOwnerChildOptIn {
		t.Fatalf("expected active enterprise key to permit verified_or_enterprise_signed and child scopes: %+v", vOK)
	}
	vRevoked := trust.EvaluateOrgExtensionTrust(withdrawnEnt, soleEnt)
	if vRevoked.DerivedEnterpriseSigned || !vRevoked.MinTrustDenied || !vRevoked.RequiresOrgOwnerChildOptIn {
		t.Fatalf("expected withdrawn enterprise key to fail closed even with DB enterprise_signed=1: %+v", vRevoked)
	}

	// Dual-signed package (publisher withdrawn AND enterprise withdrawn individually).
	otherEntPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dualWithdrawnEnt := trust.TrustedKeyring{
		PublisherPublicKeys:  map[string]ed25519.PublicKey{}, // publisher withdrawn
		EnterprisePublicKeys: map[string]ed25519.PublicKey{"other": otherEntPub},
	}
	dualIn := trust.OrgExtensionTrustInput{
		MinPluginTrust:           port.MinTrustAnyApproved,
		CanonicalName:            "@corp/dual-signed",
		ActiveVersionID:          "exv_dual",
		SignatureVerified:        true,
		SigningKeyFingerprint:    pubFP,
		EnterpriseSigned:         true,
		EnterpriseKeyFingerprint: entFP,
	}
	vDual := trust.EvaluateOrgExtensionTrust(dualWithdrawnEnt, dualIn)
	if vDual.DerivedEnterpriseSigned || !vDual.RequiresOrgOwnerChildOptIn {
		t.Fatalf("expected withdrawn enterprise key on dual-signed package to block child opt-in bypass: %+v", vDual)
	}

	for _, st := range []string{"ENABLED_ACTIVE", "ROLLED_BACK", "UPDATING_PENDING_RECONSENT"} {
		if !trust.IsUsableExtensionState(st) {
			t.Fatalf("expected state %q to be usable", st)
		}
	}
	for _, st := range []string{"QUARANTINED", "INSTALLED_DISABLED", "UNINSTALLED", ""} {
		if trust.IsUsableExtensionState(st) {
			t.Fatalf("expected state %q to be non-usable", st)
		}
	}
}
