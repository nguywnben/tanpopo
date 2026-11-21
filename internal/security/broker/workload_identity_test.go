// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package broker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
)

func TestWorkloadIdentityAWSIRSAAndGCPAllowlistAndCaching(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "oidc-token")
	if err := os.WriteFile(tokenFile, []byte("eyJhbGciOiJSUzI1NiJ9.payload.sig\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	var awsCalls, gcpSTSCalls, gcpIAMCalls atomic.Int32
	mockCloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/aws-sts":
			awsCalls.Add(1)
			if err := r.ParseForm(); err != nil || r.FormValue("Action") != "AssumeRoleWithWebIdentity" || r.FormValue("WebIdentityToken") == "" {
				http.Error(w, "invalid sts request", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"AssumeRoleWithWebIdentityResponse": map[string]any{
					"AssumeRoleWithWebIdentityResult": map[string]any{
						"Credentials": map[string]any{
							"AccessKeyId":     "ASIAEXAMPLE123",
							"SecretAccessKey": "ephemeralAwsSecret4567890",
							"SessionToken":    "FwoGZXIvYXdzEBYaDKSession",
							"Expiration":      "2030-01-01T00:15:00Z",
						},
					},
				},
			})
		case "/gcp-sts":
			gcpSTSCalls.Add(1)
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["subjectToken"] == "" {
				http.Error(w, "bad gcp sts", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "sts-federated-tok",
				"expires_in":   1800,
			})
		case "/v1/projects/-/serviceAccounts/vertex-invoker@proj.iam.gserviceaccount.com:generateAccessToken":
			gcpIAMCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer sts-federated-tok" {
				http.Error(w, "missing federated token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken": "ya29.ephemeral-vertex-token",
				"expireTime":  "2030-01-01T00:30:00Z",
			})
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer mockCloud.Close()

	u, _ := url.Parse(mockCloud.URL)
	localPol := &egress.Policy{
		AllowedHosts:   []string{u.Host},
		AllowPrivateIP: true,
		AllowedCIDRs:   []string{"127.0.0.0/8"},
		RequireTLS:     false,
	}

	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	exchanger := NewWorkloadIdentityExchanger(func() time.Time { return now })
	defer exchanger.Close()

	awsBinding := WorkloadIdentityBinding{
		BindingID:         "wif_bedrock",
		OrgID:             "org_1",
		Mode:              WorkloadIdentityAWSIRSA,
		AllowedHosts:      []string{"bedrock-runtime.us-east-1.amazonaws.com"},
		TokenFilePath:     tokenFile,
		AllowedTokenRoots: []string{tmpDir},
		RoleARN:           "arn:aws:iam::123456789012:role/tanpopo-bedrock",
		STSEndpoint:       mockCloud.URL + "/aws-sts",
		EgressPolicy:      localPol,
	}

	// Host outside allowlist fails closed BEFORE hitting STS.
	if _, err := exchanger.ExchangeHostCredentials(ctx, awsBinding, "evil-exfiltrator.example"); !errors.Is(err, egress.ErrHostDenied) {
		t.Fatalf("expected ErrHostDenied on unallowed host, got %v", err)
	}
	if awsCalls.Load() != 0 {
		t.Fatalf("expected 0 STS calls on denied host, got %d", awsCalls.Load())
	}

	// TokenFilePath outside AllowedTokenRoots fails closed BEFORE reading file or contacting STS.
	unconfinedBinding := awsBinding
	unconfinedBinding.TokenFilePath = "/etc/passwd"
	unconfinedBinding.AllowedTokenRoots = nil
	if _, err := exchanger.ExchangeHostCredentials(ctx, unconfinedBinding, "bedrock-runtime.us-east-1.amazonaws.com"); !errors.Is(err, ErrWorkloadIdentityConfig) {
		t.Fatalf("expected ErrWorkloadIdentityConfig for unconfined token_file_path, got %v", err)
	}
	if awsCalls.Load() != 0 {
		t.Fatalf("expected 0 STS calls on unconfined token path, got %d", awsCalls.Load())
	}

	// Allowed Bedrock host succeeds and caches result cleanly.
	creds1, err := exchanger.ExchangeHostCredentials(ctx, awsBinding, "bedrock-runtime.us-east-1.amazonaws.com")
	if err != nil {
		t.Fatalf("ExchangeHostCredentials AWS 1: %v", err)
	}
	defer creds1.Zeroize()
	awsJSON, err := creds1.AWSCredentialJSON()
	if err != nil {
		t.Fatalf("AWSCredentialJSON: %v", err)
	}
	if _, err := decodeAWSCredentials(awsJSON); err != nil {
		t.Fatalf("decodeAWSCredentials failed on exchanged AWS IRSA creds: %v", err)
	}
	creds2, err := exchanger.ExchangeHostCredentials(ctx, awsBinding, "bedrock-runtime.us-east-1.amazonaws.com")
	if err != nil {
		t.Fatalf("ExchangeHostCredentials AWS 2: %v", err)
	}
	creds2.Zeroize()
	if awsCalls.Load() != 1 {
		t.Fatalf("expected 1 cached STS call, got %d", awsCalls.Load())
	}

	// GCP Workload Identity Federation -> STS + ServiceAccount generateAccessToken for Vertex AI.
	gcpBinding := WorkloadIdentityBinding{
		BindingID:                 "wif_vertex",
		OrgID:                     "org_1",
		Mode:                      WorkloadIdentityGCPFederation,
		AllowedHosts:              []string{"us-central1-aiplatform.googleapis.com"},
		TokenFilePath:             tokenFile,
		AllowedTokenRoots:         []string{tmpDir},
		Audience:                  "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/tp/providers/oidc",
		ServiceAccountEmail:       "vertex-invoker@proj.iam.gserviceaccount.com",
		GCPSTSEndpoint:            mockCloud.URL + "/gcp-sts",
		GCPIAMCredentialsEndpoint: mockCloud.URL,
		EgressPolicy:              localPol,
	}
	gcpCreds, err := exchanger.ExchangeHostCredentials(ctx, gcpBinding, "us-central1-aiplatform.googleapis.com")
	if err != nil {
		t.Fatalf("ExchangeHostCredentials GCP: %v", err)
	}
	defer gcpCreds.Zeroize()
	if string(gcpCreds.BearerToken) != "ya29.ephemeral-vertex-token" {
		t.Fatalf("unexpected GCP bearer token: %q", string(gcpCreds.BearerToken))
	}
	if gcpSTSCalls.Load() != 1 || gcpIAMCalls.Load() != 1 {
		t.Fatalf("unexpected GCP call counts sts=%d iam=%d", gcpSTSCalls.Load(), gcpIAMCalls.Load())
	}
}
