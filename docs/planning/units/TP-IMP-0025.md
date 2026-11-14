# `TP-IMP-0025` — Admin Control Plane REST API (Part 2): Extensions, Connections, Models/Deployments, Routes, Policies, Budgets/Chargeback, Traces & Operations

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0025` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0012`, `TP-IMP-0020`, `TP-IMP-0021`, `TP-IMP-0024` — `GAP-01, 07`)* |
| **Size** | `L` (`internal/admin/http/extensions.go`, `connections.go`, `models.go`, `routes.go`, `policies.go`, `finops.go`, `traces.go`, `ops.go`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Write-only secrets, extension permission approval, financial chargeback export & Kill-Switch activation)* |
| **Technical Owner** | `Staff Control-Plane Engineer` |

---

## 1. Intent
Complete all remaining `/admin/v1/*` endpoint groups powering the `Web Console` and `tanpopo` CLI in `MVP (v0.1)`: `Extensions` lifecycle management (install, inspect permission delta $\Delta P$, approve permissions, upgrade, rollback, quarantine), `Secrets & Provider Connections` (write-only credential storage + live connectivity probing), `Logical Models & Deployments`, `Routes & Policies`, `Budgets & Chargeback Export (CSV/JSON)`, `Request Traces`, `Audit Log / Blast-Radius`, and `Emergency Kill-Switches`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/admin/http/extensions.go` (`/admin/v1/extensions/*`: package upload/install, `permission-diff` inspection, permission approval, upgrade, `< 5s` rollback).
  - `internal/admin/http/connections.go` & `secrets.go` (`/admin/v1/secrets` write-only endpoint that never returns plaintext secrets, `/admin/v1/connections/*` with `POST .../test` live health check routed through `BlindBroker` + `SSRFGuard` and an explicit local-CIDR opt-in flag when confirmed by the administrator in the Setup Wizard).
  - `internal/admin/http/models.go`, `routes.go`, `policies.go` (`ETag/If-Match`-protected CRUD and non-relaxation policy validation returning `422 policy_relaxation_forbidden`).
  - `internal/admin/http/finops.go` (`/admin/v1/budgets/*`, `/admin/v1/usage/summary`, `/admin/v1/usage/export` streaming `CSV` / `JSON` exports with 3 strictly separated columns: `exact_micro_usd`, `estimated_micro_usd`, `unmetered_request_count`).
  - `internal/admin/http/traces.go` & `ops.go` (`/admin/v1/traces`, `/admin/v1/audit-events`, `/admin/v1/audit-events/verify`, `/admin/v1/kill-switches`, `/admin/v1/blast-radius`, `/admin/v1/overview/summary`).
- **Out-of-Scope:** Endpoints for `MCP Servers / Tool Approvals` and `Playground Ephemeral Session` (covered in `TP-IMP-0037` and `TP-IMP-0038` in `M3 / v0.2`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0201`..`0204`, `TP-FR-0304`, `TP-FR-0306`, `TP-FR-0601`, `TP-FR-0701`, `TP-FR-0704`, `TP-FR-0705`, `TP-FR-0802`, `TP-FR-0803`, `TP-FR-0901`, `GAP-07`.
- **Dependencies:** `TP-IMP-0012`, `TP-IMP-0020`, `TP-IMP-0021`, `TP-IMP-0024`.
- **Design Refs:** [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`..`16` Data Contracts).

## 4. API, Wire & Data Contracts
- **CSV Formula Injection Sanitization Invariant (`CSV Injection Sanitization`):** When exporting chargeback reports via `GET /admin/v1/usage/export?format=csv`, any project, key, or model name starting with `=`, `+`, `-`, `@`, `\t`, or `\r` MUST be safely prefixed/escaped to prevent spreadsheet formula execution attacks.
- **Automated Contract Verification:** Every HTTP response across `TP-IMP-0024` and `TP-IMP-0025` is validated automatically in integration tests directly against `openapi/admin-v1.yaml` (`0` missing or mistyped fields).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given the Admin API integration test suite, When executing the complete end-to-end configuration flow from writing secrets -> creating connections -> installing extension packages -> inspecting `permission-diff` -> approving permissions -> creating models/routes/policies/budgets -> exporting CSV chargeback -> toggling Kill-Switches, Then `100%` of responses validate against `openapi/admin-v1.yaml` and every write operation emits a valid audit event.
- **`NEG-1` (`Write-Only Secret Verification`):** Invoking every `GET /admin/v1/secrets*` and `GET /admin/v1/connections*` endpoint and scanning the entire returned JSON payload with `CanaryScanner` confirms `0` leaked secret bytes.
- **`NEG-2` (`CSV Formula Injection`):** Naming a project `=HYPERLINK("http://evil.test","click")` and exporting CSV produces a safely neutralized cell that cannot auto-execute formulas.
- **`NEG-3` (`Role Separation`):** A principal with the `FINOPS_VIEWER` role can read `/admin/v1/usage/summary` but receives `403` when attempting to access `/admin/v1/secrets` or `/admin/v1/kill-switches`.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/admin/http/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/admin/http/...`)*
- Race detector (`-race`): `go test -v -race ./internal/admin/http/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** The `POST /admin/v1/connections/test` endpoint traverses the exact same `SSRFGuardDialer` and enforces strict rate limiting to prevent lower-privileged accounts from abusing "Test Connection" as an internal network scanner (`SSRF Oracle`).
- **Rollback:** Stateless HTTP handler layer atop tested domain modules.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (`openapi/admin-v1.yaml`), `TP-IMP-0012`, `TP-IMP-0020`, `TP-IMP-0021`, and `TP-IMP-0024` are complete.
- **DoD:** OpenAPI contract tests passing across all `v0.1` endpoints, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
