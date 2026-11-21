# `TP-IMP-0042` Execution & Verification Report — Tier-3 Isolated Sidecar (`ConnectRPC` UDS/`mTLS`) & Inter-Agent `A2A` Plane (`SM-09`)

> **Unit:** `TP-IMP-0042` (`M4` / `EP-08` · `REQUIRES_STRONG_REVIEW` · `GATE-M4` Closure)
> **Branch / Workflow:** `develop` -> PR -> `main` (under `DEC-088` & `DEC-091`)
> **Author:** `Gemini 4 Hydrogen`
> **Independent Strong Reviewers:**
> - `code-reviewer` (`1203b35d61d2b79404bede8c662d7c77`): **`APPROVE`** (Critical: 0, Required Changes: 0)
> - `security-auditor` (`f693ea4e9a8a9dabe41c2439986f96f9`): **`PASS`** (Critical: 0, High: 0, Medium: 0, Low: 0)

---

## 1. Delivered Scope

1. **Tier-3 Isolated Sidecar Runtime (`ADR-0002`, `CR-02`, `TP-FR-0308`, `AUTH-08`, `AC-1`, `NEG-2`):**
   - [`internal/extension/sidecar/{client.go,mtls_creds.go}`](../../internal/extension/sidecar/client.go), [`internal/ingress/http/{server.go,chat_completions.go}`](../../internal/ingress/http/server.go), [`cmd/tanpopo/serve.go`](../../cmd/tanpopo/serve.go), [`deploy/helm/tanpopo/values.yaml`](../../deploy/helm/tanpopo/values.yaml): Added default-disabled (`runtime.sidecar.enabled=false` fails closed with `ErrSidecarDisabled`) ConnectRPC `ExtensionRuntimeService` Tier-3 runtime over TLS 1.3 mTLS (`EphemeralCA`, `RotatingCertSource`, exact `spiffe://tanpopo.internal/...` SAN verification) or `0600` Unix domain sockets (`ConfineSocketPath` symlink-aware containment + `VerifyUDSSocketPermissions`).
   - Enforces zero host secret leakage (`SecretSlotRefs` carries opaque `slot_ref_<slot>` handles), `verifyManifestParity` on on-disk Tier-3 manifests, caller `context.Canceled` isolation on the per-sidecar circuit breaker, accurate `c.failures` audit logging (`extension.sidecar.circuit_open`), and production `SidecarRuntimeFactory` wiring in `serve.go`.
2. **Dedicated Inter-Agent `A2A` Plane (`TP-FR-0405`, `TP-FR-0406`, `DEC-003`, `CR-08`, `CT-23`, `SM-09`, `AC-2`, `NEG-1`):**
   - [`internal/ingress/a2a/{registry.go,task_coordinator.go}`](../../internal/ingress/a2a/task_coordinator.go), [`internal/finops/pricing/calculator.go`](../../internal/finops/pricing/calculator.go), [`db/migrations/sqlite/0022_a2a_plane.sql`](../../db/migrations/sqlite/0022_a2a_plane.sql), [`db/migrations/postgres/0002_a2a_task_settlement_artifacts.sql`](../../db/migrations/postgres/0002_a2a_task_settlement_artifacts.sql), [`db/spec-check/{sqlite_schema.sql,postgres_schema.sql}`](../../db/spec-check/postgres_schema.sql): Implemented fail-closed default-disabled A2A discovery (`GET /.well-known/agent.json`, `GET /a2a/agents`) and task coordination (`POST /a2a/tasks`, `GET /a2a/tasks/{taskId}` JSON/SSE, `POST /a2a/tasks/{taskId}/cancel`), removing HTTP Data Plane `/transition` exposure (`404`) and enforcing `caller_virtual_key_id == caller.Key.ID` ownership.
   - Enforces `hop_count <= 5` + `call_chain` cycle detection (`400 delegation_depth_exceeded`), fail-closed `AllowedModels` narrowing (`callerPrivilegeAllowsCard`), hierarchical residency intersection (`X-Tanpopo-Data-Region` ∩ parent region ∩ effective policy `AllowedDataRegions`), atomic parent-child sub-budget carving (`FOR UPDATE OF t` on PG16 / write-lock on SQLite via `sumCommittedChildSubBudgetsTx`), fail-closed `DirectMicroUSD` FinOps settlement/release before committing terminal state with durable `settled_cost_micros` & `artifacts_json` persistence, AUTH-07 Data Plane credential plane enforcement, `AllowedRoutes`, `ActiveStreamRegistry`, and `KillSwitch` integration.
   - Added Admin Control Plane endpoints (`GET`/`POST /admin/v1/a2a/agents`, `GET /admin/v1/a2a/tasks`) in [`internal/admin/http/a2a.go`](../../internal/admin/http/a2a.go) with `requireActiveOrgAndProject`, `https://` + non-wildcard CIDR validation, HMAC keyset cursor pagination (`parsePageParams` / `encodePaginationCursor`), and full [`openapi/{admin-v1.yaml,dataplane-v1.yaml}`](../../openapi/dataplane-v1.yaml) + `openapi_contract_test.go` conformance (`ETag`, `A2aAgentResource`, `A2aTaskResource`, `/.well-known/agent.json`).
3. **Web Console v2 (`SCR-10` A2A Agents & Active Tasks) & 15-Language i18n:**
   - [`web/console/src/pages/mcp/McpHubPage.tsx`](../../web/console/src/pages/mcp/McpHubPage.tsx), [`web/console/src/shell/{routes.ts,WorkspaceRouter.tsx}`](../../web/console/src/shell/routes.ts), [`web/console/src/api/`](../../web/console/src/api/managementClient.ts), [`web/console/src/i18n/locales/*.json`](../../web/console/src/i18n/locales/en.json): Added Astryx Neutral `A2A Agents & Active Tasks` tab on `/mcp/servers` (alias `/a2a/agents`) with agent registration drawer, task inspector drawer, and localized UI copy across all 15 bundled locales.

---

## 2. Independent Verification & Test Evidence

- **Static Quality Gates (`CGO_ENABLED=0 go run ./scripts/ci/check.go --all`):** **`PASS`** (`0` errors).
- **Go Vet (`go vet ./...`):** **`PASS`** (`0` issues).
- **Spikes (`cd tools/spikes && CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -count=1 ./...`):** **`PASS`** (`1.326s`).
- **Full Hermetic Short Suite (`CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...`):** **`PASS`** across all Go packages.
- **Web Console Build, Embedded Sync & Accessibility:** `npm --prefix web/console run generate:api && typecheck && test && check:i18n && build && sync:embed` (`6` test files / `33` passed incl. `axe-core` `0` WCAG 2.2 AA violations; `check:i18n` `15` locales; initial JS gzip **`188.89 KB / 250 KB`**; `internal/webui/dist/` synced).
- **Race & Live PostgreSQL 16 (`FORCE RLS` / `NOBYPASSRLS`) Suites (`-race -count=1`):**
  - `internal/extension/sidecar`: **`PASS`** (`1.222s` — mTLS SPIFFE rejection `NEG-2`, cert rotation, `SIGKILL` circuit breaker `AC-1`, `0600` UDS, caller-cancel isolation).
  - `internal/ingress/a2a`: **`PASS`** (`8.279s` — default-disabled `404`, SM-09 SSE lifecycle + FinOps settlement `AC-2`, loop/hop `400` `NEG-1`, cross-key IDOR `404`, parent-child carving & 0-hold spend rejection, live `PG16 FORCE RLS` tenant isolation).
  - `internal/admin/http` (`TestA2A|TestOpenAPI`): **`PASS`** (`8.423s` — Admin A2A registration, cursor pagination, OpenAPI schema/ETag contract parity).
  - `internal/ingress/http` (`TestTier3Sidecar`): **`PASS`** (`5.312s` — Tier-3 default-disabled, mTLS runtime acquisition, symlink/traversal UDS escape rejection).
  - `cmd/tanpopo` (`TestTPIMP0042|TestServe`): **`PASS`** (`3.719s` — Dual-DB SQLite + PG16 `FORCE RLS` end-to-end A2A, model allowlist fail-closed check, and live `KillSwitch` HTTP freeze `503`).
  - `internal/storage/postgres`: **`PASS`** (`21.576s` — all `10` PG16 integration tests including `0002` Expand-Only migration verification).
  - `internal/finops/...`: **`PASS`** (`ledger` `23.789s`, `pricing` `1.148s`, `rollup` `3.053s`).
