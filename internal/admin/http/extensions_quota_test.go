package http

import (
	"testing"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
)

func TestHasQuotaEscalation(t *testing.T) {
	active := installer.PermissionSet{
		ExecutionTier:       installer.ExecutionTierWasmSandboxT2,
		MaxMemoryMib:        8,
		MaxCpuMsPerCall:     10,
		MaxStreamChunkCpuMs: 3,
	}
	cases := []struct {
		name      string
		candidate installer.PermissionSet
		want      bool
	}{
		{
			name:      "memory increase",
			candidate: installer.PermissionSet{ExecutionTier: active.ExecutionTier, MaxMemoryMib: 12, MaxCpuMsPerCall: 10, MaxStreamChunkCpuMs: 3},
			want:      true,
		},
		{
			name:      "CPU increase",
			candidate: installer.PermissionSet{ExecutionTier: active.ExecutionTier, MaxMemoryMib: 8, MaxCpuMsPerCall: 20, MaxStreamChunkCpuMs: 3},
			want:      true,
		},
		{
			name:      "unchanged limits",
			candidate: active,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasQuotaEscalation(active, tc.candidate); got != tc.want {
				t.Fatalf("hasQuotaEscalation() = %t, want %t", got, tc.want)
			}
		})
	}
}
