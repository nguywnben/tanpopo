# `TP-IMP-0024` — Admin Control Plane REST API (Part 1): Bootstrap, Session/CSRF, Organization/Project/Environment, RBAC & `Virtual Keys` (`ETag`/`409`)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0024` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0009`, `TP-IMP-0023` — `GAP-01, 07, 11`)* |
| **Size** | `L` (`internal/admin/http/server.go`, `bootstrap.go`, `auth.go`, `tenancy.go`, `vkeys.go`, `pagination.go`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Control-plane `/admin/v1/*` surface, `ETag/409` optimistic concurrency control, cursor pagination & per-action authorization)* |
| **Technical Owner** | `Staff Control-Plane / Security Engineer` |

---

## 1. Intent
Implement the HTTP server framework for the **Admin Control Plane (`/admin/v1/*`)** together with its foundational endpoint groups (first-run `Bootstrap`, Login/Session/CSRF, `Organizations` / `Projects` / `Environments` management, 6-role `RBAC` assignment, and `Virtual Keys` lifecycle management), strictly adhering to the `OpenAPI 3.1` specification locked in `TP-IMP-0002`, optimistic concurrency control via `ETag` + `If-Match` returning `409 optimistic_lock_conflict` (`TP-FR-0901`), and `org_id`-bound HMAC-signed opaque cursor pagination (`GAP-11`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/admin/http/server.go` (middleware mandating explicit `ActionPermission` binding on every `/admin/v1/*` route, `CSRF` verification, brute-force login rate limiting on `/admin/v1/auth/login`, and `CanonicalError` normalization).
  - `internal/admin/http/etag.go` (emitting `ETag: "v<revision>"` across all configuration resources; rejecting `PUT/PATCH/DELETE` requests with missing or mismatched `If-Match` headers via `428 Precondition Required` / `409 Conflict`).
  - `internal/admin/http/pagination.go` (`base64url`-encoded `cursor` + HMAC signature bound to `org_id` and query filters to block cross-organization cursor tampering; default `limit` `50`, maximum `200`).
  - `internal/admin/http/bootstrap.go`, `auth.go`, `tenancy.go`, `rbac.go`, `vkeys.go`.
- **Out-of-Scope:** Endpoints for managing Extensions, Connections, Routes, Budgets, and Traces (covered in `TP-IMP-0025`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0502`, `TP-FR-0503`, `TP-FR-0901`, `AUTH-02`..`AUTH-07`, `AUTH-11`, `AUTH-13`, `AUTH-15`, `GAP-07`, `GAP-11`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0023`.
- **Design Refs:** [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md).

## 4. API, Wire & Data Contracts
- **Show-Once Plaintext Key Rule (`Show-Once Plaintext Key`):** Only the `201 Created` response of `POST /admin/v1/projects/{projectId}/virtual-keys` and `POST .../rotate` ever includes the `secret_key` field once; every subsequent list or detail `GET` request returns only `{ id, name, key_prefix, last_four, state, expires_at }`.
- **Optimistic Concurrency Control (`Optimistic Concurrency Control`):** When two administrators concurrently read `ETag: "v3"` and submit updates, the second writer MUST receive `409 optimistic_lock_conflict` along with the current resource snapshot for reconciliation, never silently overwriting concurrent changes.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a freshly started server, When querying `GET /admin/v1/bootstrap/status` (`initialized: false`), submitting `POST /admin/v1/bootstrap/complete` with a valid `Bootstrap Token` to create `ORG_OWNER` (`AUTH-02`), and provisioning a `Project`, `Environment`, and `Virtual Key` (`vk_tp_...`), Then every mutation records a complete entry in `audit_events` (`TP-IMP-0023`) and updates `ConfigSnapshot` immediately.
- **`AC-2` (`GAP-11` Cursor Pagination Determinism):** Given 125 `Virtual Keys`, When iterating sequentially with `limit=50` across 3 pages via `next_cursor`, Then exactly all 125 distinct records are returned without duplicates or omissions even when a new record is inserted into the first page concurrently.
- **`NEG-1` (`AUTH-03`, `AUTH-07`, `AUTH-13` & `AUTH-15`):** Missing `X-CSRF-Token` returns `403`; presenting `vk_tp_...` on `/admin/v1/*` returns `401 invalid_credential_plane` (`AUTH-07`); supplying a forged `org_id` in a JSON body is blocked (`AUTH-13`); using `Org A`'s `next_cursor` or resource ID from an `Org B` session returns `400 invalid_pagination_cursor` / `404 resource_not_found` (`AUTH-15`).
- **`NEG-2` (`Stale If-Match`):** Submitting `PATCH` with `If-Match: "v1"` against a resource currently at `"v2"` returns `409 optimistic_lock_conflict`.
- **`NEG-3` (`Brute-Force Login Rate Limit`):** Submitting more than 10 consecutive failed password attempts from the same IP address returns `429 auth_rate_limited` and records a security audit event.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/admin/http/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/admin/http/...`)*
- Race detector (`-race`): `go test -v -race ./internal/admin/http/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** A route-reflection test (`TestAllAdminRoutesHaveExplicitPermission`) inspects the HTTP routing table and fails CI if any `/admin/v1/*` route (other than `/bootstrap/status` and `/auth/login`) lacks an explicit `RBAC` permission constant.
- **Rollback:** Decoupled HTTP transport layer sitting cleanly above the `iam` and `audit` domain services.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (finalized `OpenAPI 3.1` specification closing `GAP-07`), `TP-IMP-0009`, and `TP-IMP-0023` are complete.
- **DoD:** 100% conformance against the `OpenAPI 3.1` schema, all `AUTH-02..07, 11, 13, 15` cases passing, and `REQUIRES_STRONG_REVIEW` sign-off achieved.
