package catalog

import (
	"errors"
	"testing"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestSelectPrimaryCredentialPrefersAPIKeyRegardlessOfSlotOrder(t *testing.T) {
	apiKey := SecretBindingInput{SlotName: "api_key", CredentialID: "credential-api"}
	organization := SecretBindingInput{SlotName: "organization", CredentialID: "credential-org"}
	cases := []struct {
		name     string
		bindings []SecretBindingInput
	}{
		{name: "api key first", bindings: []SecretBindingInput{apiKey, organization}},
		{name: "organization first", bindings: []SecretBindingInput{organization, apiKey}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectPrimaryCredential(nil, nil, false, tc.bindings)
			if err != nil {
				t.Fatalf("selectPrimaryCredential() error = %v", err)
			}
			if got == nil || *got != apiKey.CredentialID {
				t.Fatalf("primary credential = %v, want %q", got, apiKey.CredentialID)
			}
		})
	}
}

func TestSelectPrimaryCredentialKeepsExplicitConflictCheck(t *testing.T) {
	explicit := "credential-explicit"
	bindings := []SecretBindingInput{{SlotName: "api_key", CredentialID: "credential-api"}}
	if _, err := selectPrimaryCredential(nil, &explicit, true, bindings); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected explicit credential conflict to remain ErrInvalidInput, got %v", err)
	}
}

func TestSelectPrimaryCredentialPreservesLegacyPrimaryOnSecondaryBindingMerge(t *testing.T) {
	current := "credential-legacy"
	bindings := []SecretBindingInput{{SlotName: "organization", CredentialID: "credential-org"}}
	got, err := selectPrimaryCredential(&current, nil, false, bindings)
	if err != nil {
		t.Fatalf("selectPrimaryCredential() error = %v", err)
	}
	if got == nil || *got != current {
		t.Fatalf("primary credential = %v, want preserved legacy credential %q", got, current)
	}
}
