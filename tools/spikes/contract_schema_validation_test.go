package spikes_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

func TestContractProtoCompilesCleanly(t *testing.T) {
	protoDir := filepath.Join("..", "..", "proto")
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{protoDir},
		}),
	}
	files, err := compiler.Compile(context.Background(), "tanpopo/extension/v1/runtime.proto")
	if err != nil {
		t.Fatalf("runtime.proto failed to compile via protocompile: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("expected compiled FileDescriptor for runtime.proto")
	}
	fd := files[0]
	services := fd.Services()
	if services.Len() == 0 {
		t.Fatalf("expected >= 1 gRPC/ConnectRPC service in runtime.proto")
	}

	foundMethods := make(map[string]bool)
	for i := 0; i < services.Len(); i++ {
		svc := services.Get(i)
		methods := svc.Methods()
		for j := 0; j < methods.Len(); j++ {
			foundMethods[string(methods.Get(j).Name())] = true
		}
	}
	requiredRPCs := []string{
		"TransformRequest",
		"TransformResponse",
		"TransformStreamChunk",
		"ExtractUsage",
		"NormalizeError",
		"InspectNativeRequest",
		"BuildValidationProbe",
		"VerifyValidationResult",
	}
	for _, rpcName := range requiredRPCs {
		if !foundMethods[rpcName] {
			t.Errorf("runtime.proto missing required RPC %q (found: %v)", rpcName, foundMethods)
		}
	}
	t.Logf("PROTO VERIFIED: runtime.proto compiled cleanly with %d services and %d RPC methods", services.Len(), len(foundMethods))
}

func collectInternalRefs(node any, out *[]string) {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if k == "$ref" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, "#/") {
					*out = append(*out, s)
				}
			} else {
				collectInternalRefs(val, out)
			}
		}
	case []any:
		for _, item := range v {
			collectInternalRefs(item, out)
		}
	}
}

func resolveJSONPointer(root map[string]any, ref string) bool {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	var cur any = root
	for _, rawPart := range parts {
		part := strings.ReplaceAll(strings.ReplaceAll(rawPart, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		next, exists := m[part]
		if !exists {
			return false
		}
		cur = next
	}
	return true
}

func TestOpenAPISpecsParseAndResolveAllInternalRefs(t *testing.T) {
	specs := []struct {
		relPath       string
		requiredPaths []string
	}{
		{
			relPath: filepath.Join("..", "..", "openapi", "dataplane-v1.yaml"),
			requiredPaths: []string{
				"/v1/chat/completions",
				"/v1/responses",
				"/v1/embeddings",
				"/v1/models",
				"/v1/rerank",
				"/mcp",
			},
		},
		{
			relPath: filepath.Join("..", "..", "openapi", "admin-v1.yaml"),
			requiredPaths: []string{
				"/admin/v1/bootstrap/status",
				"/admin/v1/bootstrap/complete",
				"/admin/v1/organizations",
				"/admin/v1/playground/sessions",
				"/admin/v1/routes/explain",
			},
		},
	}

	for _, sp := range specs {
		raw, err := os.ReadFile(sp.relPath)
		if err != nil {
			t.Fatalf("read %s: %v", sp.relPath, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("YAML parse error in %s: %v", sp.relPath, err)
		}
		ver, _ := doc["openapi"].(string)
		if !strings.HasPrefix(ver, "3.1") {
			t.Fatalf("%s expected openapi 3.1.x, got %q", sp.relPath, ver)
		}
		pathsMap, ok := doc["paths"].(map[string]any)
		if !ok || len(pathsMap) == 0 {
			t.Fatalf("%s missing non-empty paths object", sp.relPath)
		}
		for _, reqP := range sp.requiredPaths {
			if _, exists := pathsMap[reqP]; !exists {
				t.Errorf("%s missing required path %q", sp.relPath, reqP)
			}
		}
		var refs []string
		collectInternalRefs(doc, &refs)
		for _, r := range refs {
			if !resolveJSONPointer(doc, r) {
				t.Errorf("%s has unresolved internal $ref %q", sp.relPath, r)
			}
		}
		t.Logf("OPENAPI VERIFIED: %s (%s) parsed cleanly with %d paths and %d resolved internal $refs",
			filepath.Base(sp.relPath), ver, len(pathsMap), len(refs))
	}
}

func TestPhysicalSQLiteAndPostgresSchemaParityAndConstraints(t *testing.T) {
	sqlitePath := filepath.Join("..", "..", "db", "spec-check", "sqlite_schema.sql")
	pgPath := filepath.Join("..", "..", "db", "spec-check", "postgres_schema.sql")

	sqliteSQL, err := os.ReadFile(sqlitePath)
	if err != nil {
		t.Fatalf("read sqlite_schema.sql: %v", err)
	}
	pgSQL, err := os.ReadFile(pgPath)
	if err != nil {
		t.Fatalf("read postgres_schema.sql: %v", err)
	}

	requiredTables := []string{
		"organizations",
		"projects",
		"environments",
		"org_encryption_keys",
		"project_encryption_keys",
		"principals",
		"role_bindings",
		"virtual_keys",
		"extension_packages",
		"extension_versions",
		"extension_installations",
		"org_extension_policies",
		"extension_revocations",
		"secret_credentials",
		"provider_connections",
		"connection_secret_bindings",
		"oauth_authorization_sessions",
		"oauth_token_states",
		"model_deployments",
		"route_policies",
		"policies",
		"mcp_servers",
		"a2a_tasks",
		"approval_requests",
		"budget_ledgers",
		"budget_reservations",
		"valkey_hold_checkpoints",
		"usage_settlements",
		"usage_rollups_hourly",
		"audit_events",
		"config_changelog",
		"distributed_leases",
		"webhook_subscriptions",
	}

	for _, tbl := range requiredTables {
		if !strings.Contains(string(sqliteSQL), tbl) {
			t.Errorf("sqlite_schema.sql missing canonical table %q", tbl)
		}
		if !strings.Contains(string(pgSQL), tbl) {
			t.Errorf("postgres_schema.sql missing canonical table %q", tbl)
		}
	}
	if !strings.Contains(string(pgSQL), "FORCE ROW LEVEL SECURITY") {
		t.Errorf("postgres_schema.sql missing FORCE ROW LEVEL SECURITY declarations")
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		t.Fatalf("enable foreign_keys: %v", err)
	}
	if _, err := db.Exec(string(sqliteSQL)); err != nil {
		t.Fatalf("sqlite_schema.sql failed live execution on modernc.org/sqlite: %v", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		t.Fatalf("re-enable foreign_keys: %v", err)
	}

	// Verify composite (org_id, id) cross-tenant foreign key blocks cross-org environment injection!
	if _, err := db.Exec(`INSERT INTO organizations (id, slug, display_name, min_plugin_trust, created_at_ms, updated_at_ms) VALUES
		('org_a', 'acme', 'Acme', 'VERIFIED_OR_ENTERPRISE_SIGNED', 1000, 1000),
		('org_b', 'beta', 'Beta', 'OFFICIAL_ONLY', 1000, 1000);`); err != nil {
		t.Fatalf("seed organizations: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, org_id, slug, display_name, created_at_ms, updated_at_ms) VALUES
		('prj_a1', 'org_a', 'core', 'Core A', 1000, 1000),
		('prj_b1', 'org_b', 'core', 'Core B', 1000, 1000);`); err != nil {
		t.Fatalf("seed projects: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO environments (id, org_id, project_id, slug, display_name, created_at_ms, updated_at_ms) VALUES
		('env_cross_tenant_bad', 'org_a', 'prj_b1', 'prod', 'Prod', 1000, 1000);`); err == nil {
		t.Fatalf("CRITICAL TENANT ISOLATION BUG: cross-org composite FK allowed org_a environment to bind to org_b project!")
	}

	t.Logf("SQL SCHEMA CHECK: sqlite_schema.sql executed live on pure-Go modernc.org/sqlite (cross-org composite FK verified); postgres_schema.sql passed static table-name & FORCE RLS text checks across %d canonical tables (NOTE: live PostgreSQL 16 DDL parsing, partitioning & RLS execution remain UNVERIFIED until TP-IMP-0039)", len(requiredTables))
}
