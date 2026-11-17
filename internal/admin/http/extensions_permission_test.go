package http

import (
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

func TestPermissionSetFromManifestIncludesEscalationFields(t *testing.T) {
	manifest := installer.ExtensionManifest{
		Runtime: &installer.ManifestRuntime{
			Quotas: &installer.RuntimeQuotas{MaxMemoryMib: 12, MaxCpuMsPerCall: 15, MaxStreamChunkCpuMs: 4},
		},
		Capabilities: []installer.ManifestCap{{AuthMethods: []installer.ManifestAuthMethod{{ID: "signed", Mode: "host_crypto_sign"}}}},
		SecretSlots:  []installer.ManifestSlotSpec{{Slot: "api_key", RequiredForAuthMethods: []string{"signed"}}},
		Adapter:      map[string]any{"auth": []any{map[string]any{"secretSlot": "api_key", "mode": "host_blind_inject"}}},
		Permissions: installer.ManifestPerms{
			NetworkEgress: installer.ManifestNetworkEgress{AllowedHosts: []string{"API.EXAMPLE.TEST"}, AllowPrivateIP: true, RequireTLS: false},
			Hooks:         []string{"provider:transform_request"},
			PayloadAccess: &installer.ManifestPayloadAccess{InspectRequestBody: true},
		},
	}
	candidate := permissionSetFromManifest(manifest, installer.ExecutionTierWasmSandboxT2)
	if len(candidate.AllowedHosts) != 1 || candidate.AllowedHosts[0] != "api.example.test" {
		t.Fatalf("normalized allowed hosts = %#v", candidate.AllowedHosts)
	}
	if !candidate.AllowPrivateIP || candidate.RequireTLS || !candidate.InspectRequestBody || candidate.InspectResponseBody {
		t.Fatalf("network and payload permissions were not preserved: %+v", candidate)
	}
	if candidate.SecretBindingModes["api_key"] != "host_blind_inject,host_crypto_sign" || len(candidate.Hooks) != 1 || candidate.Hooks[0] != "provider:transform_request" {
		t.Fatalf("binding modes or hooks were not preserved: %+v", candidate)
	}
	if candidate.MaxMemoryMib != 12 || candidate.MaxCpuMsPerCall != 15 || candidate.MaxStreamChunkCpuMs != 4 {
		t.Fatalf("resource quotas were not preserved: %+v", candidate)
	}

	active := installer.PermissionSet{
		ExecutionTier:       installer.ExecutionTierWasmSandboxT2,
		SecretSlots:         []string{"api_key"},
		SecretBindingModes:  map[string]string{"api_key": "host_blind_inject"},
		RequireTLS:          true,
		MaxMemoryMib:        8,
		MaxCpuMsPerCall:     10,
		MaxStreamChunkCpuMs: 2,
	}
	delta := installer.ComputePermissionDelta(active, candidate)
	if !delta.HasEscalation() || len(delta.AddedHooks) != 1 || !delta.BindingModeChanged || !delta.PrivateIPEscalated || !delta.TLSDowngraded || !delta.PayloadAccessEscalated || !delta.MemoryIncreased || !delta.CpuQuotaIncreased {
		t.Fatalf("expected full permission expansion to be represented, got %+v", delta)
	}
}
