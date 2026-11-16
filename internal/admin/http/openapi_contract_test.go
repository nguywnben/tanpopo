// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
