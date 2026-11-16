// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"gopkg.in/yaml.v3"
)

type openAPIDoc struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]openAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

type openAPISchema struct {
	Type       any                      `yaml:"type"`
	Required   []string                 `yaml:"required"`
	Enum       []any                    `yaml:"enum"`
	Ref        string                   `yaml:"$ref"`
	AllOf      []openAPISchema          `yaml:"allOf"`
	Properties map[string]openAPISchema `yaml:"properties"`
	Items      *openAPISchema           `yaml:"items"`
}

func loadAdminOpenAPISpec(t *testing.T) openAPIDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "openapi", "admin-v1.yaml"))
	if err != nil {
		t.Fatalf("read openapi/admin-v1.yaml: %v", err)
	}
	var doc openAPIDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal openapi/admin-v1.yaml: %v", err)
	}
	return doc
}

func resolveSchema(t *testing.T, doc openAPIDoc, schemaName string) ([]string, map[string]openAPISchema) {
	t.Helper()
	sc, ok := doc.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("schema %q not found in openapi/admin-v1.yaml", schemaName)
	}
	reqSet := make(map[string]bool)
	props := make(map[string]openAPISchema)

	var collect func(s openAPISchema)
	collect = func(s openAPISchema) {
		if s.Ref != "" {
			refName := strings.TrimPrefix(s.Ref, "#/components/schemas/")
			target, ok := doc.Components.Schemas[refName]
			if !ok {
				t.Fatalf("broken $ref %q", s.Ref)
			}
			collect(target)
		}
		for _, sub := range s.AllOf {
			collect(sub)
		}
		for _, r := range s.Required {
			reqSet[r] = true
		}
		for k, v := range s.Properties {
			props[k] = v
		}
	}
	collect(sc)

	required := make([]string, 0, len(reqSet))
	for r := range reqSet {
		required = append(required, r)
	}
	return required, props
}

func assertPayloadMatchesSchema(t *testing.T, doc openAPIDoc, schemaName string, rawJSON []byte) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(rawJSON, &obj); err != nil {
		t.Fatalf("decode JSON for schema %s: %v\npayload=%s", schemaName, err, string(rawJSON))
	}
	required, props := resolveSchema(t, doc, schemaName)
	for _, reqField := range required {
		val, exists := obj[reqField]
		if !exists {
			t.Fatalf("schema %s missing required field %q in payload: %s", schemaName, reqField, string(rawJSON))
		}
		_ = val
	}
	for fieldName, propSchema := range props {
		val, exists := obj[fieldName]
		if !exists || val == nil {
			continue
		}
		checkPropEnums(t, doc, schemaName+"."+fieldName, propSchema, val)
	}
}

func checkPropEnums(t *testing.T, doc openAPIDoc, fieldPath string, prop openAPISchema, val any) {
	t.Helper()
	if prop.Ref != "" {
		refName := strings.TrimPrefix(prop.Ref, "#/components/schemas/")
		if target, ok := doc.Components.Schemas[refName]; ok {
			checkPropEnums(t, doc, fieldPath, target, val)
		}
		return
	}
	if len(prop.Enum) > 0 {
		sVal, isStr := val.(string)
		if isStr {
			found := false
			for _, e := range prop.Enum {
				if es, ok := e.(string); ok && es == sVal {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s value %q is outside OpenAPI enum %v", fieldPath, sVal, prop.Enum)
			}
		}
	}
	if prop.Items != nil {
		if arr, ok := val.([]any); ok {
			for _, elem := range arr {
				checkPropEnums(t, doc, fieldPath+"[]", *prop.Items, elem)
			}
		}
	}
}

func TestOpenAPIRouteAndPayloadConformancePart1(t *testing.T) {
	t.Parallel()
	doc := loadAdminOpenAPISpec(t)
	h := newAdminTestHarness(t)
	ctx := context.Background()

	// 1. Every registered route on srv MUST exist in openapi/admin-v1.yaml with the exact method.
	for _, rt := range h.srv.Routes() {
		pathItem, ok := doc.Paths[rt.Path]
		if !ok {
			t.Fatalf("registered route %s %s missing from openapi/admin-v1.yaml paths", rt.Method, rt.Path)
		}
		methodKey := strings.ToLower(rt.Method)
		if _, ok := pathItem[methodKey]; !ok {
			t.Fatalf("registered method %s missing from openapi/admin-v1.yaml path %s", rt.Method, rt.Path)
		}
	}

	// 2. Verify live JSON responses against OpenAPI components.schemas.
	rec := h.doPublic(t, http.MethodGet, "/admin/v1/bootstrap/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bootstrap/status status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "BootstrapStatusResponse", rec.Body.Bytes())

	// Seed provider_adapter for optional atomic initial_connection check.
	seedTestAdapter(t, h)

	token := h.readBootstrapToken(t)
	bootBody := map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "owner-spec@example.test",
		"owner_password":                 "StrongBootstrapPass#2026!",
		"org_name":                       "Spec Org",
		"project_name":                   "spec-proj",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":       "adp_openai_spec",
			"name":             "openai-primary",
			"billing_mode":     "COMMERCIAL_API",
			"endpoint_url":     "https://api.openai.com/v1",
			"allow_private_ip": false,
			"secret_value":     "sk-spec-dummy-credential-1234",
			"model_id":         "gpt-4o-mini",
			"data_region":      "US",
		},
	}
	rec = h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", bootBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /bootstrap/complete status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "BootstrapCompleteResponse", rec.Body.Bytes())

	var bootResp bootstrapCompleteResponse
	mustDecodeJSON(t, rec.Body.Bytes(), &bootResp)
	if bootResp.InitialVirtualKey == nil {
		t.Fatalf("expected initial_virtual_key in BootstrapCompleteResponse")
	}
	vkCreatedRaw, _ := json.Marshal(bootResp.InitialVirtualKey)
	assertPayloadMatchesSchema(t, doc, "VirtualKeyCreatedResponse", vkCreatedRaw)

	cookies := rec.Result().Cookies()
	sessionCookie := findCookie(t, cookies, "__Host-tanpopo_admin_session")

	// CanonicalErrorEnvelope check via replay -> 410.
	recErr := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", bootBody)
	if recErr.Code != http.StatusGone {
		t.Fatalf("expected 410 replay, got %d body=%s", recErr.Code, recErr.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "CanonicalErrorEnvelope", recErr.Body.Bytes())
	var errEnv CanonicalErrorEnvelope
	mustDecodeJSON(t, recErr.Body.Bytes(), &errEnv)
	if errEnv.Error.Code != "bootstrap_already_completed" {
		t.Fatalf("unexpected error code: %s", errEnv.Error.Code)
	}
	if recErr.Header().Get(RequestIDHeader) != errEnv.Error.RequestID {
		t.Fatalf("header request_id %q != body request_id %q", recErr.Header().Get(RequestIDHeader), errEnv.Error.RequestID)
	}

	// AdminSessionResponse check.
	recSess := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/auth/session", sessionCookie, bootResp.CSRFToken, "", nil)
	if recSess.Code != http.StatusOK {
		t.Fatalf("GET /auth/session status=%d body=%s", recSess.Code, recSess.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "AdminSessionResponse", recSess.Body.Bytes())

	// Organization, Project, Environment checks.
	recOrg := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/organizations", sessionCookie, "", "", nil)
	if recOrg.Code != http.StatusOK {
		t.Fatalf("GET /organizations status=%d body=%s", recOrg.Code, recOrg.Body.String())
	}
	var orgList paginatedListEnvelope[json.RawMessage]
	mustDecodeJSON(t, recOrg.Body.Bytes(), &orgList)
	if len(orgList.Data) != 1 {
		t.Fatalf("expected 1 org, got %d", len(orgList.Data))
	}
	assertPayloadMatchesSchema(t, doc, "Organization", orgList.Data[0])

	recProj := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", sessionCookie, bootResp.CSRFToken, "", map[string]any{
		"slug": "extra-proj",
		"name": "Extra Project",
	})
	if recProj.Code != http.StatusCreated {
		t.Fatalf("POST /projects status=%d body=%s", recProj.Code, recProj.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "Project", recProj.Body.Bytes())
	var createdProj projectDTO
	mustDecodeJSON(t, recProj.Body.Bytes(), &createdProj)

	recEnv := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+createdProj.ID+"/environments", sessionCookie, bootResp.CSRFToken, "", map[string]any{
		"slug": "prod",
		"name": "Production",
	})
	if recEnv.Code != http.StatusCreated {
		t.Fatalf("POST /environments status=%d body=%s", recEnv.Code, recEnv.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "Environment", recEnv.Body.Bytes())
	var createdEnv environmentDTO
	mustDecodeJSON(t, recEnv.Body.Bytes(), &createdEnv)

	// MemberSummary check.
	recMember := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", sessionCookie, bootResp.CSRFToken, "", map[string]any{
		"email":       "dev-spec@example.test",
		"kind":        "HUMAN",
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{createdProj.ID},
	})
	if recMember.Code != http.StatusCreated {
		t.Fatalf("POST /members status=%d body=%s", recMember.Code, recMember.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "MemberSummary", recMember.Body.Bytes())

	// VirtualKeySummary & CryptoShredReceipt checks.
	recVK := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys", sessionCookie, "", "", nil)
	if recVK.Code != http.StatusOK {
		t.Fatalf("GET /virtual-keys status=%d body=%s", recVK.Code, recVK.Body.String())
	}
	var vkList paginatedListEnvelope[json.RawMessage]
	mustDecodeJSON(t, recVK.Body.Bytes(), &vkList)
	if len(vkList.Data) == 0 {
		t.Fatalf("expected virtual keys in list")
	}
	assertPayloadMatchesSchema(t, doc, "VirtualKeySummary", vkList.Data[0])

	// Ensure createdProj can be shredded and matches CryptoShredReceipt schema.
	_, err := h.vkeys.Create(ctx, mustVKeyCreateParams(bootResp.OrgID, createdProj.ID, createdEnv.ID, bootResp.OwnerPrincipalID, "to-shred"))
	if err != nil {
		t.Fatalf("Create VKey: %v", err)
	}
	recDelProj := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/projects/"+createdProj.ID, sessionCookie, bootResp.CSRFToken, `"v1"`, map[string]any{
		"confirm_slug": createdProj.Slug,
		"reason":       "Decommissioning test project per spec audit",
	})
	if recDelProj.Code != http.StatusOK {
		t.Fatalf("DELETE /projects status=%d body=%s", recDelProj.Code, recDelProj.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "CryptoShredReceipt", recDelProj.Body.Bytes())
}

func assertRequiredJSONKeys(t *testing.T, label string, rawJSON []byte, keys ...string) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(rawJSON, &obj); err != nil {
		t.Fatalf("%s invalid JSON: %v\npayload=%s", label, err, string(rawJSON))
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			t.Fatalf("%s missing required field %q in payload=%s", label, k, string(rawJSON))
		}
	}
}

func TestOpenAPIRouteAndPayloadConformancePart2(t *testing.T) {
	t.Parallel()
	doc := loadAdminOpenAPISpec(t)
	h := newPart2TestHarness(t, 20)
	ctx := context.Background()

	// Verify exact 68 MVP v0.1 Control Plane routes and 100% OpenAPI conformance.
	if got := len(h.srv.Routes()); got != 68 {
		t.Fatalf("expected 68 registered Part 1+2 routes, got %d", got)
	}
	for _, rt := range h.srv.Routes() {
		pathItem, ok := doc.Paths[rt.Path]
		if !ok {
			t.Fatalf("registered route %s %s missing from openapi/admin-v1.yaml paths", rt.Method, rt.Path)
		}
		if _, ok := pathItem[strings.ToLower(rt.Method)]; !ok {
			t.Fatalf("registered method %s missing from openapi/admin-v1.yaml path %s", rt.Method, rt.Path)
		}
	}

	boot, ownerCookie := h.bootstrapOwner(t, "Contract Org", "contract-proj")
	reviewerTok := "tok_contract_reviewer"
	reviewerID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'sec@example.test', 'ACTIVE', 1000, 1000)`,
		reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_ct_rev', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, 1000)`,
		boot.OrgID, reviewerID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert reviewer role_binding: %v", err)
	}
	h.registerBearerToken(reviewerTok, &rbac.Actor{
		PrincipalID:     reviewerID,
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleSecurityReviewer}},
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o-mini"}]}`))
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)
	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())

	// Group E: Provider Adapters, Secrets & Connections
	recAdp := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/provider-adapters/"+adpID, ownerCookie, "", "", nil)
	if recAdp.Code != http.StatusOK {
		t.Fatalf("GET /provider-adapters/{id} status=%d body=%s", recAdp.Code, recAdp.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ProviderAdapterSummary", recAdp.Body.Bytes())

	recSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "contract-key",
		"secret_kind":  "API_KEY",
		"secret_value": "sk-contract-12345678",
	})
	if recSec.Code != http.StatusCreated {
		t.Fatalf("POST /secrets status=%d body=%s", recSec.Code, recSec.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "SecretMetadata", recSec.Body.Bytes())
	var secMeta secretMetadataDTO
	mustDecodeJSON(t, recSec.Body.Bytes(), &secMeta)

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "contract-conn",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     upstream.URL,
		"data_region":      "US",
		"allow_private_ip": true,
		"secret_bindings":  map[string]string{"api_key": secMeta.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections status=%d body=%s", recConn.Code, recConn.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ProviderConnection", recConn.Body.Bytes())
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	recTest := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recTest.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test status=%d body=%s", recTest.Code, recTest.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ConnectionTestResponse", recTest.Body.Bytes())

	// Group F: Logical Models & Deployments
	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "gpt-4o-mini",
		"display_name":      "GPT-4o Mini",
		"context_window":    128000,
		"max_output_tokens": 16384,
		"assurance_tier":    "VERIFIED",
		"capabilities":      map[string]any{"streaming": true},
	})
	if recMod.Code != http.StatusCreated {
		t.Fatalf("POST /models status=%d body=%s", recMod.Code, recMod.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "LogicalModel", recMod.Body.Bytes())
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":              boot.EnvID,
		"logical_model_id":    mod.ID,
		"connection_id":       conn.ID,
		"upstream_model_name": "gpt-4o-mini",
		"data_region":         "US",
		"rate_card": map[string]any{
			"input_token_nano_usd":  150,
			"output_token_nano_usd": 600,
		},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments status=%d body=%s", recDep.Code, recDep.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ModelDeployment", recDep.Body.Bytes())
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)

	recDepPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recDepPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /deployments status=%d body=%s", recDepPatch.Code, recDepPatch.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ModelDeployment", recDepPatch.Body.Bytes())

	// Group G: Marketplace, Extensions & Lockfile
	recMkt := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/marketplace/catalog", ownerCookie, "", "", nil)
	if recMkt.Code != http.StatusOK {
		t.Fatalf("GET /marketplace/catalog status=%d body=%s", recMkt.Code, recMkt.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /marketplace/catalog", recMkt.Body.Bytes(), "data", "offline_cached", "has_more")

	v1URI := "https://marketplace.tanpopo.dev/contract-1.0.0.tpkg"
	v1SHA := h.registerSignedProviderTPKG(t, v1URI, "@tanpopo/contract-adapter", "1.0.0", []string{"api.openai.com"}, false, true)
	recIns := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recIns.Code != http.StatusAccepted {
		t.Fatalf("POST /extensions/install status=%d body=%s", recIns.Code, recIns.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ExtensionPermissionDiff", recIns.Body.Bytes())
	var extDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recIns.Body.Bytes(), &extDiff)
	insETag := recIns.Result().Header.Get("ETag")

	recDiff := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/"+extDiff.InstallationID+"/permission-diff", ownerCookie, "", "", nil)
	if recDiff.Code != http.StatusOK {
		t.Fatalf("GET /permission-diff status=%d body=%s", recDiff.Code, recDiff.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ExtensionPermissionDiff", recDiff.Body.Bytes())

	recApp := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+extDiff.InstallationID+"/approve-permissions", reviewerTok, insETag, map[string]any{
		"enable_immediately": true,
	})
	if recApp.Code != http.StatusOK {
		t.Fatalf("POST /approve-permissions status=%d body=%s", recApp.Code, recApp.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ExtensionInstallation", recApp.Body.Bytes())

	recLock := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/lockfile", ownerCookie, "", "", nil)
	if recLock.Code != http.StatusOK {
		t.Fatalf("GET /extensions/lockfile status=%d body=%s", recLock.Code, recLock.Body.String())
	}

	// Group H: Routes, Explain & Hierarchical Policies
	recRoute := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":                 "contract-route",
		"env_id":               boot.EnvID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"targets":              []map[string]any{{"deployment_id": dep.ID, "priority_order": 1, "weight": 100}},
	})
	if recRoute.Code != http.StatusCreated {
		t.Fatalf("POST /routes status=%d body=%s", recRoute.Code, recRoute.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "RouteResource", recRoute.Body.Bytes())

	recExp := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes/explain", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":       boot.EnvID,
		"target_model": "contract-route",
	})
	if recExp.Code != http.StatusOK {
		t.Fatalf("POST /routes/explain status=%d body=%s", recExp.Code, recExp.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "RouteExplainResponse", recExp.Body.Bytes())

	polID := idgen.MustNew(idgen.PrefixPolicy)
	recPol := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/policies/"+polID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":               "ORGANIZATION",
		"scope_id":                 boot.OrgID,
		"allowed_data_regions":     []string{"US"},
		"min_plugin_trust":         "VERIFIED_ONLY",
		"min_capability_assurance": "TESTED",
	})
	if recPol.Code != http.StatusOK {
		t.Fatalf("PUT /policies status=%d body=%s", recPol.Code, recPol.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "PolicyDocument", recPol.Body.Bytes())

	recEffPol := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/policies/effective?project_id="+boot.ProjectID, ownerCookie, "", "", nil)
	if recEffPol.Code != http.StatusOK {
		t.Fatalf("GET /policies/effective status=%d body=%s", recEffPol.Code, recEffPol.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "PolicyDocument", recEffPol.Body.Bytes())

	// Group J & D & K: Budgets, Usage, Traces, Debug Payload, Overview, Alerts, Audit, Kill-Switch, Blast-Radius
	now := h.fk.Now().UTC().UnixMilli()
	bdgID := idgen.MustNew(idgen.PrefixBudget)
	recBdg := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/"+bdgID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":            "PROJECT",
		"scope_id":              boot.ProjectID,
		"period":                "MONTHLY",
		"hard_limit_micros":     1000,
		"soft_warning_percents": []int{50, 80, 100},
	})
	if recBdg.Code != http.StatusOK {
		t.Fatalf("PUT /budgets status=%d body=%s", recBdg.Code, recBdg.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "BudgetResource", recBdg.Body.Bytes())

	// Force high utilization + unmetered spend + trace + debug blob for AlertItem & TraceSummary checks.
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE budget_ledgers SET settled_exact_micros = 900 WHERE id = ?`, bdgID); err != nil {
		t.Fatalf("update budget_ledgers: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO usage_settlements (
			id, settled_at_ms, request_id, org_id, project_id, env_id, virtual_key_id,
			plane, logical_model_slug, billing_mode, usage_accuracy, total_cost_micros
		) VALUES ('stl_ct_1', ?, 'req_ct_1', ?, ?, ?, 'vk_ct', 'PROVIDER', 'contract-route', 'COMMERCIAL_API', 'EXACT', 900)`,
		now, boot.OrgID, boot.ProjectID, boot.EnvID,
	); err != nil {
		t.Fatalf("insert usage_settlements: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id, connection_id,
			entry_point, route_slug, selected_deployment_id, http_status,
			proxy_overhead_us, ttft_ms, total_duration_ms, waterfall_stages_json
		) VALUES ('req_ct_1', ?, ?, ?, ?, ?, 'vk_ct', ?, 'unified_http', 'contract-route', ?, 200, 120, 10, 25, '[{"stage":"auth","duration_ms":1.5,"outcome":"ok"}]')`,
		now, now-25, boot.OrgID, boot.ProjectID, boot.EnvID, conn.ID, dep.ID,
	); err != nil {
		t.Fatalf("insert request_traces_meta: %v", err)
	}
	nonce, ct, err := h.vault.SealProjectBlob(ctx, boot.OrgID, boot.ProjectID, vault.RecordKindDebugBlob, "req_ct_1", []byte(`{"prompt":"contract"}`))
	if err != nil {
		t.Fatalf("SealProjectBlob: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO debug_payload_blobs (request_id, org_id, project_id, nonce, encrypted_payload_aead, expires_at_ms, created_at_ms)
		VALUES ('req_ct_1', ?, ?, ?, ?, ?, ?)`,
		boot.OrgID, boot.ProjectID, nonce, ct, now+3600_000, now,
	); err != nil {
		t.Fatalf("insert debug blob: %v", err)
	}

	recUsage := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/summary?group_by=project", ownerCookie, "", "", nil)
	if recUsage.Code != http.StatusOK {
		t.Fatalf("GET /usage/summary status=%d body=%s", recUsage.Code, recUsage.Body.String())
	}
	var usageList struct {
		Data []json.RawMessage `json:"data"`
	}
	mustDecodeJSON(t, recUsage.Body.Bytes(), &usageList)
	if len(usageList.Data) == 0 {
		t.Fatalf("expected UsageChargebackRow items")
	}
	assertPayloadMatchesSchema(t, doc, "UsageChargebackRow", usageList.Data[0])

	recTr := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/traces/req_ct_1", ownerCookie, "", "", nil)
	if recTr.Code != http.StatusOK {
		t.Fatalf("GET /traces/{id} status=%d body=%s", recTr.Code, recTr.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "TraceSummary", recTr.Body.Bytes())

	recDbg := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/traces/req_ct_1/debug-payload", ownerCookie, "", "", nil)
	if recDbg.Code != http.StatusOK {
		t.Fatalf("GET /traces/{id}/debug-payload status=%d body=%s", recDbg.Code, recDbg.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /traces/{id}/debug-payload", recDbg.Body.Bytes(), "request_id", "expires_at", "payload")

	recOv := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/overview/summary", ownerCookie, "", "", nil)
	if recOv.Code != http.StatusOK {
		t.Fatalf("GET /overview/summary status=%d body=%s", recOv.Code, recOv.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "OverviewSummaryResponse", recOv.Body.Bytes())

	recAlerts := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/alerts/active", ownerCookie, "", "", nil)
	if recAlerts.Code != http.StatusOK {
		t.Fatalf("GET /alerts/active status=%d body=%s", recAlerts.Code, recAlerts.Body.String())
	}
	var alertList paginatedListEnvelope[json.RawMessage]
	mustDecodeJSON(t, recAlerts.Body.Bytes(), &alertList)
	if len(alertList.Data) == 0 {
		t.Fatalf("expected active alerts")
	}
	assertPayloadMatchesSchema(t, doc, "AlertItem", alertList.Data[0])

	recAud := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/audit-events", ownerCookie, "", "", nil)
	if recAud.Code != http.StatusOK {
		t.Fatalf("GET /audit-events status=%d body=%s", recAud.Code, recAud.Body.String())
	}
	var audList paginatedListEnvelope[json.RawMessage]
	mustDecodeJSON(t, recAud.Body.Bytes(), &audList)
	if len(audList.Data) == 0 {
		t.Fatalf("expected audit events")
	}
	assertPayloadMatchesSchema(t, doc, "AuditEventResource", audList.Data[0])

	recVer := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/audit-events/verify", ownerCookie, boot.CSRFToken, "", nil)
	if recVer.Code != http.StatusOK {
		t.Fatalf("POST /audit-events/verify status=%d body=%s", recVer.Code, recVer.Body.String())
	}
	assertRequiredJSONKeys(t, "POST /audit-events/verify", recVer.Body.Bytes(), "intact", "checked_events_count")

	recKS := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/kill-switches", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_level":         "PROVIDER_CONNECTION",
		"target_id":            conn.ID,
		"abort_active_streams": true,
		"reason":               "Contract check kill-switch",
	})
	if recKS.Code != http.StatusOK {
		t.Fatalf("POST /kill-switches status=%d body=%s", recKS.Code, recKS.Body.String())
	}
	assertRequiredJSONKeys(t, "POST /kill-switches", recKS.Body.Bytes(), "config_epoch", "severed_streams_count", "affected_entities_count")

	recBR := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=PROVIDER_CONNECTION&target_id="+conn.ID, ownerCookie, "", "", nil)
	if recBR.Code != http.StatusOK {
		t.Fatalf("GET /blast-radius status=%d body=%s", recBR.Code, recBR.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /blast-radius", recBR.Body.Bytes(), "organizations", "projects", "virtual_keys", "connections")
}

