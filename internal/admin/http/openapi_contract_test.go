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
	Const      any                      `yaml:"const"`
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

func mergeOpenAPISchema(t *testing.T, doc openAPIDoc, s openAPISchema) openAPISchema {
	t.Helper()
	merged := openAPISchema{
		Type:       s.Type,
		Enum:       s.Enum,
		Const:      s.Const,
		Items:      s.Items,
		Properties: make(map[string]openAPISchema),
	}
	reqSet := make(map[string]bool)

	var collect func(cur openAPISchema)
	collect = func(cur openAPISchema) {
		if cur.Ref != "" {
			refName := strings.TrimPrefix(cur.Ref, "#/components/schemas/")
			target, ok := doc.Components.Schemas[refName]
			if !ok {
				t.Fatalf("broken $ref %q", cur.Ref)
			}
			collect(target)
		}
		for _, sub := range cur.AllOf {
			collect(sub)
		}
		if merged.Type == nil && cur.Type != nil {
			merged.Type = cur.Type
		}
		if len(merged.Enum) == 0 && len(cur.Enum) > 0 {
			merged.Enum = cur.Enum
		}
		if merged.Const == nil && cur.Const != nil {
			merged.Const = cur.Const
		}
		if merged.Items == nil && cur.Items != nil {
			merged.Items = cur.Items
		}
		for _, r := range cur.Required {
			reqSet[r] = true
		}
		for k, v := range cur.Properties {
			merged.Properties[k] = v
		}
	}
	collect(s)

	for r := range reqSet {
		merged.Required = append(merged.Required, r)
	}
	return merged
}

func assertPayloadMatchesSchema(t *testing.T, doc openAPIDoc, schemaName string, rawJSON []byte) {
	t.Helper()
	var val any
	if err := json.Unmarshal(rawJSON, &val); err != nil {
		t.Fatalf("decode JSON for schema %s: %v\npayload=%s", schemaName, err, string(rawJSON))
	}
	sc, ok := doc.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("schema %q not found in openapi/admin-v1.yaml", schemaName)
	}
	validateValueAgainstSchema(t, doc, schemaName, sc, val)
}

func schemaAllowsType(schemaType any) (allowed []string, allowsNull bool) {
	switch tv := schemaType.(type) {
	case string:
		if tv == "null" {
			return nil, true
		}
		return []string{tv}, false
	case []any:
		for _, item := range tv {
			if s, ok := item.(string); ok {
				if s == "null" {
					allowsNull = true
				} else {
					allowed = append(allowed, s)
				}
			}
		}
	case []string:
		for _, s := range tv {
			if s == "null" {
				allowsNull = true
			} else {
				allowed = append(allowed, s)
			}
		}
	}
	return allowed, allowsNull
}

func valueMatchesTypeName(val any, typeName string) bool {
	switch typeName {
	case "string":
		_, ok := val.(string)
		return ok
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "number":
		_, ok := val.(float64)
		return ok
	case "integer":
		f, ok := val.(float64)
		return ok && f == float64(int64(f))
	case "array":
		_, ok := val.([]any)
		return ok
	case "object":
		_, ok := val.(map[string]any)
		return ok
	default:
		return true
	}
}

func validateValueAgainstSchema(t *testing.T, doc openAPIDoc, fieldPath string, rawSchema openAPISchema, val any) {
	t.Helper()
	prop := mergeOpenAPISchema(t, doc, rawSchema)

	allowedTypes, allowsNull := schemaAllowsType(prop.Type)
	if val == nil {
		if len(allowedTypes) > 0 && !allowsNull {
			t.Fatalf("%s is null but OpenAPI type %v is non-nullable", fieldPath, prop.Type)
		}
		return
	}
	if len(allowedTypes) > 0 {
		matched := false
		for _, tp := range allowedTypes {
			if valueMatchesTypeName(val, tp) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("%s value %#v (%T) does not match OpenAPI type %v", fieldPath, val, val, prop.Type)
		}
	}

	if prop.Const != nil {
		switch c := prop.Const.(type) {
		case int:
			if f, ok := val.(float64); !ok || f != float64(c) {
				t.Fatalf("%s value %#v does not equal OpenAPI const %#v", fieldPath, val, prop.Const)
			}
		default:
			if val != prop.Const {
				t.Fatalf("%s value %#v does not equal OpenAPI const %#v", fieldPath, val, prop.Const)
			}
		}
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
				validateValueAgainstSchema(t, doc, fieldPath+"[]", *prop.Items, elem)
			}
		}
	}

	if obj, ok := val.(map[string]any); ok {
		for _, reqField := range prop.Required {
			if _, exists := obj[reqField]; !exists {
				t.Fatalf("%s missing required field %q in payload: %#v", fieldPath, reqField, obj)
			}
		}
		if len(prop.Properties) > 0 {
			for fieldName, subSchema := range prop.Properties {
				subVal, exists := obj[fieldName]
				if !exists {
					continue
				}
				validateValueAgainstSchema(t, doc, fieldPath+"."+fieldName, subSchema, subVal)
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

func TestOpenAPIOAuthRequestBodyIsOnlyOnOAuthStart(t *testing.T) {
	doc := loadAdminOpenAPISpec(t)
	logout := doc.Paths["/admin/v1/auth/logout"]["post"].(map[string]any)
	if _, ok := logout["requestBody"]; ok {
		t.Fatal("logout operation must not declare the OAuth start request body")
	}
	start := doc.Paths["/admin/v1/connections/{connectionId}/oauth/start"]["post"].(map[string]any)
	if _, ok := start["requestBody"]; !ok {
		t.Fatal("OAuth start operation must declare its request body")
	}
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

	// Verify all registered v0.2/v0.3 Control Plane routes and 100% OpenAPI conformance.
	const expectedRoutes = 107
	if got := len(h.srv.Routes()); got != expectedRoutes {
		t.Fatalf("expected %d registered Control Plane routes, got %d", expectedRoutes, got)
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

	assertETag := func(label string, rec *httptest.ResponseRecorder) string {
		t.Helper()
		etag := strings.TrimSpace(rec.Result().Header.Get("ETag"))
		if etag == "" {
			t.Fatalf("%s missing required OpenAPI ETag response header", label)
		}
		return etag
	}

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
	assertETag("POST /secrets", recSec)
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
		"allowed_cidrs":    []string{"127.0.0.1/32"},
		"secret_bindings":  map[string]string{"api_key": secMeta.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections status=%d body=%s", recConn.Code, recConn.Body.String())
	}
	assertETag("POST /connections", recConn)
	assertPayloadMatchesSchema(t, doc, "ProviderConnection", recConn.Body.Bytes())
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	recTest := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recTest.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test status=%d body=%s", recTest.Code, recTest.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ConnectionTestResponse", recTest.Body.Bytes())

	// Also verify failing POST /connections/{id}/test conforms to ConnectionTestResponse (valid=false, non-null failure_code/sanitized_message).
	recFailConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "contract-fail-conn",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     "http://127.0.0.1:1/v1",
		"data_region":      "ON_PREM",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
	})
	if recFailConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections failing target status=%d body=%s", recFailConn.Code, recFailConn.Body.String())
	}
	var failConn providerConnectionDTO
	mustDecodeJSON(t, recFailConn.Body.Bytes(), &failConn)
	recFailTest := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+failConn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recFailTest.Code != http.StatusOK {
		t.Fatalf("failing POST /connections/{id}/test status=%d body=%s", recFailTest.Code, recFailTest.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "ConnectionTestResponse", recFailTest.Body.Bytes())
	var failProbe connectionTestResponseDTO
	mustDecodeJSON(t, recFailTest.Body.Bytes(), &failProbe)
	if failProbe.Valid || failProbe.FailureCode == nil || failProbe.SanitizedMessage == nil {
		t.Fatalf("expected valid=false with failure_code and sanitized_message, got %+v", failProbe)
	}
	// Reload latest version (validation test bumped version) then delete throwaway failConn.
	recFailList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/connections", ownerCookie, "", "", nil)
	var failList paginatedListEnvelope[providerConnectionDTO]
	mustDecodeJSON(t, recFailList.Body.Bytes(), &failList)
	for _, c := range failList.Data {
		if c.ID == failConn.ID {
			failConn = c
			break
		}
	}
	recDelFailConn := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/connections/"+failConn.ID, ownerCookie, boot.CSRFToken, FormatETag(failConn.Version), nil)
	if recDelFailConn.Code != http.StatusNoContent {
		t.Fatalf("DELETE /connections/{id} status=%d body=%s", recDelFailConn.Code, recDelFailConn.Body.String())
	}

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
	assertETag("POST /models", recMod)
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
	assertETag("POST /deployments", recDep)
	assertPayloadMatchesSchema(t, doc, "ModelDeployment", recDep.Body.Bytes())
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)

	recDepPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recDepPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /deployments status=%d body=%s", recDepPatch.Code, recDepPatch.Body.String())
	}
	depETag := assertETag("PATCH /deployments", recDepPatch)
	assertPayloadMatchesSchema(t, doc, "ModelDeployment", recDepPatch.Body.Bytes())
	mustDecodeJSON(t, recDepPatch.Body.Bytes(), &dep)

	// Group G: Marketplace, Extensions & Lockfile
	recMkt := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/marketplace/catalog", ownerCookie, "", "", nil)
	if recMkt.Code != http.StatusOK {
		t.Fatalf("GET /marketplace/catalog status=%d body=%s", recMkt.Code, recMkt.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /marketplace/catalog", recMkt.Body.Bytes(), "data", "offline_cached", "has_more")
	assertPayloadMatchesSchema(t, doc, "MarketplaceCatalogResponse", recMkt.Body.Bytes())
	var marketplace marketplaceCatalogResponse
	mustDecodeJSON(t, recMkt.Body.Bytes(), &marketplace)
	if len(marketplace.Data) == 0 || marketplace.Data[0].SourceURI == "" || len(marketplace.Data[0].ExpectedSHA256) != 64 {
		t.Fatalf("marketplace entries must expose a stageable source URI and digest: %+v", marketplace.Data)
	}

	v1URI := "https://marketplace.tanpopo.dev/contract-1.0.0.tpkg"
	v1SHA := h.registerSignedProviderTPKG(t, v1URI, "@tanpopo/contract-adapter", "1.0.0", []string{"api.openai.com"}, false, true)
	v2URI := "https://marketplace.tanpopo.dev/contract-1.0.1.tpkg"
	v2SHA := h.registerSignedProviderTPKG(t, v2URI, "@tanpopo/contract-adapter", "1.0.1", []string{"api.openai.com"}, false, true)
	recIns := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v1URI,
		"expected_sha256": v1SHA,
	})
	if recIns.Code != http.StatusAccepted {
		t.Fatalf("POST /extensions/install status=%d body=%s", recIns.Code, recIns.Body.String())
	}
	insETag := assertETag("POST /extensions/install", recIns)
	assertPayloadMatchesSchema(t, doc, "ExtensionPermissionDiff", recIns.Body.Bytes())
	var extDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recIns.Body.Bytes(), &extDiff)

	recDiff := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/"+extDiff.InstallationID+"/permission-diff", ownerCookie, "", "", nil)
	if recDiff.Code != http.StatusOK {
		t.Fatalf("GET /permission-diff status=%d body=%s", recDiff.Code, recDiff.Body.String())
	}
	assertETag("GET /permission-diff", recDiff)
	assertPayloadMatchesSchema(t, doc, "ExtensionPermissionDiff", recDiff.Body.Bytes())

	recApp := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+extDiff.InstallationID+"/approve-permissions", reviewerTok, insETag, map[string]any{
		"enable_immediately": true,
	})
	if recApp.Code != http.StatusOK {
		t.Fatalf("POST /approve-permissions status=%d body=%s", recApp.Code, recApp.Body.String())
	}
	appETag := assertETag("POST /approve-permissions", recApp)
	assertPayloadMatchesSchema(t, doc, "ExtensionInstallation", recApp.Body.Bytes())

	// Non-escalating upgrade (same hosts) -> 200 + ETag + ExtensionInstallation, then rollback -> 200 + ETag + ExtensionInstallation.
	recUp := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+extDiff.InstallationID+"/upgrade", ownerCookie, boot.CSRFToken, appETag, map[string]any{
		"source_uri":      v2URI,
		"expected_sha256": v2SHA,
	})
	if recUp.Code != http.StatusOK {
		t.Fatalf("POST /extensions/{id}/upgrade status=%d body=%s", recUp.Code, recUp.Body.String())
	}
	upETag := assertETag("POST /extensions/{id}/upgrade", recUp)
	assertPayloadMatchesSchema(t, doc, "ExtensionInstallation", recUp.Body.Bytes())

	recRB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/"+extDiff.InstallationID+"/rollback", ownerCookie, boot.CSRFToken, upETag, map[string]any{
		"reason": "Contract rollback verification",
	})
	if recRB.Code != http.StatusOK {
		t.Fatalf("POST /extensions/{id}/rollback status=%d body=%s", recRB.Code, recRB.Body.String())
	}
	_ = assertETag("POST /extensions/{id}/rollback", recRB)
	assertPayloadMatchesSchema(t, doc, "ExtensionInstallation", recRB.Body.Bytes())

	recLockYAML := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/extensions/lockfile", ownerCookie, "", "", nil)
	if recLockYAML.Code != http.StatusOK {
		t.Fatalf("GET /extensions/lockfile status=%d body=%s", recLockYAML.Code, recLockYAML.Body.String())
	}
	if ct := recLockYAML.Header().Get("Content-Type"); !strings.Contains(ct, "application/yaml") {
		t.Fatalf("GET /extensions/lockfile expected application/yaml Content-Type, got %q", ct)
	}
	reqLockJSON, _ := http.NewRequest(http.MethodGet, "/admin/v1/extensions/lockfile", nil)
	reqLockJSON.AddCookie(ownerCookie)
	reqLockJSON.Header.Set("Accept", "application/json")
	recLockJSON := httptest.NewRecorder()
	h.srv.handler.ServeHTTP(recLockJSON, reqLockJSON)
	if recLockJSON.Code != http.StatusOK {
		t.Fatalf("GET /extensions/lockfile Accept: application/json status=%d body=%s", recLockJSON.Code, recLockJSON.Body.String())
	}
	if ct := recLockJSON.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("GET /extensions/lockfile expected application/json Content-Type, got %q", ct)
	}
	var lockObj map[string]any
	mustDecodeJSON(t, recLockJSON.Body.Bytes(), &lockObj)

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
	assertETag("POST /routes", recRoute)
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
	assertETag("PUT /policies/{id}", recPol)
	assertPayloadMatchesSchema(t, doc, "PolicyDocument", recPol.Body.Bytes())

	recEffPol := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/policies/effective?project_id="+boot.ProjectID, ownerCookie, "", "", nil)
	if recEffPol.Code != http.StatusOK {
		t.Fatalf("GET /policies/effective status=%d body=%s", recEffPol.Code, recEffPol.Body.String())
	}
	assertETag("GET /policies/effective", recEffPol)
	assertPayloadMatchesSchema(t, doc, "PolicyDocument", recEffPol.Body.Bytes())

	// Group I (A2A): Agent registration, listing & Task list schema checks
	recA2aAgent := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id":          boot.ProjectID,
		"agent_slug":          "contract-agent",
		"name":                "Contract Agent",
		"description":         "Contract test agent",
		"endpoint_url":        "https://agent.example.test/a2a",
		"data_region":         "US",
		"max_delegation_hops": 3,
		"skills":              []map[string]any{{"id": "analyze", "name": "Analyze"}},
	})
	if recA2aAgent.Code != http.StatusCreated {
		t.Fatalf("POST /a2a/agents status=%d body=%s", recA2aAgent.Code, recA2aAgent.Body.String())
	}
	assertETag("POST /a2a/agents", recA2aAgent)
	assertPayloadMatchesSchema(t, doc, "A2aAgentResource", recA2aAgent.Body.Bytes())
	var a2aAgentDTO struct {
		AgentID string `json:"agent_id"`
	}
	mustDecodeJSON(t, recA2aAgent.Body.Bytes(), &a2aAgentDTO)

	recA2aAgentList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/agents", ownerCookie, "", "", nil)
	if recA2aAgentList.Code != http.StatusOK {
		t.Fatalf("GET /a2a/agents status=%d body=%s", recA2aAgentList.Code, recA2aAgentList.Body.String())
	}
	var a2aAgentEnv paginatedListEnvelope[json.RawMessage]
	mustDecodeJSON(t, recA2aAgentList.Body.Bytes(), &a2aAgentEnv)
	if len(a2aAgentEnv.Data) == 0 {
		t.Fatalf("expected non-empty GET /a2a/agents list")
	}
	assertPayloadMatchesSchema(t, doc, "A2aAgentResource", a2aAgentEnv.Data[0])

	recA2aVK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
		"name":       "a2a-contract-vk",
	})
	if recA2aVK.Code != http.StatusCreated {
		t.Fatalf("POST /virtual-keys for A2A status=%d body=%s", recA2aVK.Code, recA2aVK.Body.String())
	}
	var a2aVK struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recA2aVK.Body.Bytes(), &a2aVK)

	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO a2a_tasks(
			id, org_id, project_id, env_id, agent_endpoint_id, caller_virtual_key_id,
			trace_root_id, hop_count, call_chain_ids_json, sub_budget_cap_micros,
			settled_cost_micros, reservation_id, enforced_region, state, version, created_at_ms, updated_at_ms
		) VALUES ('tsk_ct_1', ?, ?, ?, ?, ?, 'req_ct_a2a', 1, '["`+a2aAgentDTO.AgentID+`"]', 50000, 12000, 'res_ct_1', 'US', 'WORKING', 1, ?, ?)`,
		boot.OrgID, boot.ProjectID, boot.EnvID, a2aAgentDTO.AgentID, a2aVK.ID, h.fk.Now().UTC().UnixMilli(), h.fk.Now().UTC().UnixMilli(),
	); err != nil {
		t.Fatalf("insert a2a_tasks contract row: %v", err)
	}
	recA2aTasks := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/tasks", ownerCookie, "", "", nil)
	if recA2aTasks.Code != http.StatusOK {
		t.Fatalf("GET /a2a/tasks status=%d body=%s", recA2aTasks.Code, recA2aTasks.Body.String())
	}
	var a2aTaskEnv paginatedListEnvelope[json.RawMessage]
	mustDecodeJSON(t, recA2aTasks.Body.Bytes(), &a2aTaskEnv)
	if len(a2aTaskEnv.Data) == 0 {
		t.Fatalf("expected non-empty GET /a2a/tasks list")
	}
	assertPayloadMatchesSchema(t, doc, "A2aTaskResource", a2aTaskEnv.Data[0])

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
	assertETag("PUT /budgets/{id}", recBdg)
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
	var usageCounts struct {
		Data []usageChargebackRowDTO `json:"data"`
	}
	mustDecodeJSON(t, recUsage.Body.Bytes(), &usageCounts)
	var foundExactCounts bool
	for _, row := range usageCounts.Data {
		if row.GroupKey == "contract-proj" {
			if row.ExactRequestCount != 1 || row.EstimatedRequestCount != 0 {
				t.Fatalf("exact and estimated request counts must remain separate, got %+v", row)
			}
			foundExactCounts = true
		}
	}
	if !foundExactCounts {
		t.Fatalf("usage summary omitted the test project's exact request counts: %+v", usageCounts.Data)
	}

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
	recKSList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/kill-switches?active_only=true", ownerCookie, "", "", nil)
	if recKSList.Code != http.StatusOK {
		t.Fatalf("GET /kill-switches status=%d body=%s", recKSList.Code, recKSList.Body.String())
	}
	var activeSwitches struct {
		Data []struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
			Active  bool   `json:"active"`
		} `json:"data"`
	}
	mustDecodeJSON(t, recKSList.Body.Bytes(), &activeSwitches)
	if len(activeSwitches.Data) == 0 || !activeSwitches.Data[0].Active {
		t.Fatalf("expected a listed active switch, got %s", recKSList.Body.String())
	}
	recClear := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/kill-switches/"+activeSwitches.Data[0].ID, ownerCookie, boot.CSRFToken, "", map[string]any{
		"expected_version": activeSwitches.Data[0].Version,
		"clear_reason":     "contract test recovery",
	})
	if recClear.Code != http.StatusOK {
		t.Fatalf("DELETE /kill-switches/{id} status=%d body=%s", recClear.Code, recClear.Body.String())
	}
	assertPayloadMatchesSchema(t, doc, "KillSwitchResource", recClear.Body.Bytes())
	recProjectBR := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=PROJECT&target_id="+boot.ProjectID, ownerCookie, "", "", nil)
	if recProjectBR.Code != http.StatusOK {
		t.Fatalf("GET /blast-radius PROJECT status=%d body=%s", recProjectBR.Code, recProjectBR.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /blast-radius PROJECT", recProjectBR.Body.Bytes(), "organizations", "projects", "virtual_keys", "connections")
	initialVKey, err := h.vkeys.Create(ctx, mustVKeyCreateParams(boot.OrgID, boot.ProjectID, boot.EnvID, boot.OwnerPrincipalID, "blast-radius-contract"))
	if err != nil {
		t.Fatalf("create virtual key for blast-radius contract test: %v", err)
	}
	recVKeyBR := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=VIRTUAL_KEY&target_id="+initialVKey.Summary.ID, ownerCookie, "", "", nil)
	if recVKeyBR.Code != http.StatusOK {
		t.Fatalf("GET /blast-radius VIRTUAL_KEY status=%d body=%s", recVKeyBR.Code, recVKeyBR.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /blast-radius VIRTUAL_KEY", recVKeyBR.Body.Bytes(), "organizations", "projects", "virtual_keys", "connections")
	recExtensionBR := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=EXTENSION&target_id="+extDiff.InstallationID, ownerCookie, "", "", nil)
	if recExtensionBR.Code != http.StatusOK {
		t.Fatalf("GET /blast-radius EXTENSION status=%d body=%s", recExtensionBR.Code, recExtensionBR.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /blast-radius EXTENSION", recExtensionBR.Body.Bytes(), "organizations", "projects", "virtual_keys", "connections")

	recBR := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=PROVIDER_CONNECTION&target_id="+conn.ID, ownerCookie, "", "", nil)
	if recBR.Code != http.StatusOK {
		t.Fatalf("GET /blast-radius status=%d body=%s", recBR.Code, recBR.Body.String())
	}
	assertRequiredJSONKeys(t, "GET /blast-radius", recBR.Body.Bytes(), "organizations", "projects", "virtual_keys", "connections")

	// List & Update endpoint DTO schema conformance checks across Part 2 resource groups.
	assertListFirstItemSchema := func(path, schemaName string) {
		t.Helper()
		rec := h.doAuthedCookie(t, http.MethodGet, path, ownerCookie, "", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		assertRequiredJSONKeys(t, "GET "+path, rec.Body.Bytes(), "data", "has_more")
		var listResp paginatedListEnvelope[json.RawMessage]
		mustDecodeJSON(t, rec.Body.Bytes(), &listResp)
		if len(listResp.Data) == 0 {
			t.Fatalf("GET %s returned empty data list for schema %s check", path, schemaName)
		}
		assertPayloadMatchesSchema(t, doc, schemaName, listResp.Data[0])
	}

	assertListFirstItemSchema("/admin/v1/provider-adapters", "ProviderAdapterSummary")
	assertListFirstItemSchema("/admin/v1/secrets", "SecretMetadata")
	assertListFirstItemSchema("/admin/v1/connections", "ProviderConnection")
	assertListFirstItemSchema("/admin/v1/models", "LogicalModel")
	assertListFirstItemSchema("/admin/v1/deployments", "ModelDeployment")
	assertListFirstItemSchema("/admin/v1/extensions/installations", "ExtensionInstallation")
	assertListFirstItemSchema("/admin/v1/routes", "RouteResource")
	assertListFirstItemSchema("/admin/v1/budgets", "BudgetResource")
	assertListFirstItemSchema("/admin/v1/traces", "TraceSummary")

	// PATCH /connections/{id} and PUT /routes/{id} return canonical schemas + ETags.
	recConnList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/connections", ownerCookie, "", "", nil)
	if recConnList.Code != http.StatusOK {
		t.Fatalf("GET /connections status=%d body=%s", recConnList.Code, recConnList.Body.String())
	}
	var connList paginatedListEnvelope[providerConnectionDTO]
	mustDecodeJSON(t, recConnList.Body.Bytes(), &connList)
	connVer := conn.Version
	for _, c := range connList.Data {
		if c.ID == conn.ID {
			connVer = c.Version
			break
		}
	}
	recConnPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(connVer), map[string]any{
		"name": "contract-conn-renamed",
	})
	if recConnPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /connections/{id} status=%d body=%s", recConnPatch.Code, recConnPatch.Body.String())
	}
	connETag := assertETag("PATCH /connections/{id}", recConnPatch)
	assertPayloadMatchesSchema(t, doc, "ProviderConnection", recConnPatch.Body.Bytes())

	var createdRoute routeResourceDTO
	mustDecodeJSON(t, recRoute.Body.Bytes(), &createdRoute)
	recRoutePut := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/routes/"+createdRoute.ID, ownerCookie, boot.CSRFToken, FormatETag(createdRoute.Version), map[string]any{
		"slug":                 "contract-route",
		"env_id":               boot.EnvID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"targets":              []map[string]any{{"deployment_id": dep.ID, "priority_order": 1, "weight": 100}},
	})
	if recRoutePut.Code != http.StatusOK {
		t.Fatalf("PUT /routes/{id} status=%d body=%s", recRoutePut.Code, recRoutePut.Body.String())
	}
	routeETag := assertETag("PUT /routes/{id}", recRoutePut)
	assertPayloadMatchesSchema(t, doc, "RouteResource", recRoutePut.Body.Bytes())

	// Extension policy PUT -> 200 + ETag + Organization schema.
	var approvedIns extensionInstallationDTO
	mustDecodeJSON(t, recApp.Body.Bytes(), &approvedIns)
	recExtPolPut := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/organizations/"+boot.OrgID+"/extension-policies", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"min_plugin_trust": "VERIFIED_ONLY",
		"policies":         []map[string]any{{"package_id": approvedIns.PackageID, "enabled": true}},
	})
	if recExtPolPut.Code != http.StatusOK {
		t.Fatalf("PUT /extension-policies status=%d body=%s", recExtPolPut.Code, recExtPolPut.Body.String())
	}
	assertETag("PUT /extension-policies", recExtPolPut)
	assertPayloadMatchesSchema(t, doc, "Organization", recExtPolPut.Body.Bytes())

	recExpJSON := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/export?format=json&group_by=project", ownerCookie, "", "", nil)
	if recExpJSON.Code != http.StatusOK {
		t.Fatalf("GET /usage/export?format=json status=%d body=%s", recExpJSON.Code, recExpJSON.Body.String())
	}
	var exportedRows []json.RawMessage
	mustDecodeJSON(t, recExpJSON.Body.Bytes(), &exportedRows)
	if len(exportedRows) == 0 {
		t.Fatalf("expected exported UsageChargebackRow entries")
	}
	assertPayloadMatchesSchema(t, doc, "UsageChargebackRow", exportedRows[0])

	recExpCSV := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/usage/export?format=csv&group_by=project", ownerCookie, "", "", nil)
	if recExpCSV.Code != http.StatusOK {
		t.Fatalf("GET /usage/export?format=csv status=%d body=%s", recExpCSV.Code, recExpCSV.Body.String())
	}
	if !strings.Contains(recExpCSV.Body.String(), "group_key,exact_micro_usd,estimated_micro_usd,exact_request_count,estimated_request_count,unmetered_request_count") {
		t.Fatalf("unexpected CSV header: %s", recExpCSV.Body.String())
	}

	// DELETE routes conformance (reverse dependency order): route -> deployment -> connection -> secret -> extension.
	recDelRoute := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/routes/"+createdRoute.ID, ownerCookie, boot.CSRFToken, routeETag, nil)
	if recDelRoute.Code != http.StatusNoContent {
		t.Fatalf("DELETE /routes/{id} status=%d body=%s", recDelRoute.Code, recDelRoute.Body.String())
	}
	recDelDep := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, depETag, nil)
	if recDelDep.Code != http.StatusNoContent {
		t.Fatalf("DELETE /deployments/{id} status=%d body=%s", recDelDep.Code, recDelDep.Body.String())
	}
	recDelConn := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, connETag, nil)
	if recDelConn.Code != http.StatusNoContent {
		t.Fatalf("DELETE /connections/{id} status=%d body=%s", recDelConn.Code, recDelConn.Body.String())
	}
	recDelSec := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/secrets/"+secMeta.CredentialID, ownerCookie, boot.CSRFToken, FormatETag(secMeta.Version), nil)
	if recDelSec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /secrets/{id} status=%d body=%s", recDelSec.Code, recDelSec.Body.String())
	}
	// Stage + approve a throwaway extension with enable_immediately=false (INSTALLED_DISABLED) so SM-01 permits DELETE /extensions/{id} -> 204.
	v3URI := "https://marketplace.tanpopo.dev/contract-uninstall-1.0.0.tpkg"
	v3SHA := h.registerSignedProviderTPKG(t, v3URI, "@tanpopo/contract-uninstall", "1.0.0", []string{"api.openai.com"}, false, true)
	recInsDis := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/extensions/install", ownerCookie, boot.CSRFToken, "", map[string]any{
		"source_uri":      v3URI,
		"expected_sha256": v3SHA,
	})
	if recInsDis.Code != http.StatusAccepted {
		t.Fatalf("POST /extensions/install (for delete) status=%d body=%s", recInsDis.Code, recInsDis.Body.String())
	}
	var disDiff extensionPermissionDiffDTO
	mustDecodeJSON(t, recInsDis.Body.Bytes(), &disDiff)
	recAppDis := h.doBearer(t, http.MethodPost, "/admin/v1/extensions/"+disDiff.InstallationID+"/approve-permissions", reviewerTok, assertETag("POST /extensions/install (delete)", recInsDis), map[string]any{
		"enable_immediately": false,
	})
	if recAppDis.Code != http.StatusOK {
		t.Fatalf("POST /approve-permissions (disabled) status=%d body=%s", recAppDis.Code, recAppDis.Body.String())
	}
	recDelExt := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/extensions/"+disDiff.InstallationID, ownerCookie, boot.CSRFToken, assertETag("POST /approve-permissions (disabled)", recAppDis), nil)
	if recDelExt.Code != http.StatusNoContent {
		t.Fatalf("DELETE /extensions/{id} status=%d body=%s", recDelExt.Code, recDelExt.Body.String())
	}
}
