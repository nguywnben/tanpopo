# `TP-IMP-0009` — Local Admin Auth, 6-Role Canonical `RBAC` Middleware, `Virtual Key` Lifecycle (`vk_tp_`) & Immutable `ConfigSnapshot`

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0009` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Status** | **`READY_FOR_REVIEW`** *(Implemented `2026-11-14`; `CGO_ENABLED=0` tests + `check.go --all` + `BenchmarkSnapshotLookup` `0 allocs/op` + fresh `security-auditor` [`d98bfc91`] & `code-reviewer` [`ab0a518a`] `PASS`; awaiting remote CI `-race` run & Product Owner `REQUIRES_STRONG_REVIEW` acceptance — downstream `0011+` stay `BLOCKED`)* |
| **Size** | `L` (`go.mod`, `internal/iam/*`, `internal/config/snapshot/*`, `db/migrations/sqlite/0005_iam_keys.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Authentication, multi-tenant authorization, `< 1s` key revocation & uniform `404` anti-oracle)* |
| **Technical Owner** | `Principal Security / IAM Engineer` |

---

## 1. Intent
Implement core identity authentication, **6-role canonical RBAC (`OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`)**, `vk_tp_<env>_<32_base62>` `Virtual Key` lifecycle storing raw `SHA-256` digests across two columns (`active_key_hash`, `previous_key_hash`) per canonical state machine `SM-05`, and the lock-free `Copy-on-Write ConfigSnapshot` (`atomic.Pointer`) tailing `config_changelog` (`TP-IMP-0007`) so hot inference requests read configuration with `0` SQL queries and invalidate revoked keys in `< 1 second` on single-node deployments (`ADR-0006`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (add `golang.org/x/crypto` for `argon2id`).
  - `internal/iam/bootstrap/token.go` (mint one-time `Bootstrap Token` to a `0600` file on an empty DB; permanently burn upon creating the first `ORG_OWNER`/`OPERATOR` and return `410 bootstrap_already_completed` on all subsequent attempts — `AUTH-02`).
  - `internal/iam/auth/local.go` (`Argon2id` password hashing, admin session cookies with `HttpOnly; SameSite=Strict; Secure` + double-submit `X-CSRF-Token` enforcement — `AUTH-03`; hard credential-plane separation rejecting `Virtual Key` tokens on `/admin/v1/*` and rejecting Admin session cookies on `/v1/*` with `401 invalid_credential_plane` — `AUTH-07`).
  - `internal/iam/rbac/authorizer.go` (per-action `Authorize(actor, action, scope)` across all 6 canonical roles; ignore spoofed `org_id` fields in JSON request bodies by deriving `org_id` strictly from authenticated session context — `AUTH-13`; return uniform `404 resource_not_found` across cross-org lookups to prevent ID enumeration — `AUTH-15`).
  - `internal/iam/vkey/service.go` (`SM-05` state machine: `CREATED_ACTIVE -> ROTATING_DUAL_VALID -> QUOTA_PAUSED | SUSPENDED | REVOKED | EXPIRED`; enforce environment scope so `vk_tp_staging_...` targeting `prod` returns `403 environment_scope_denied` — `AUTH-04`; dual revocation modes: standard revoke vs emergency revoke severing in-flight streams via `context.CancelFunc`).
  - `internal/config/snapshot/manager.go` (`atomic.Pointer[ConfigSnapshot]`, tailing `config_changelog` for `< 1s` atomic snapshot swaps).
  - `db/migrations/sqlite/0005_iam_keys.sql` (`principals` with partial unique index `WHERE org_id IS NULL` for `OPERATOR`, `role_bindings`, `virtual_keys`).
- **Out-of-Scope:** `Enterprise OIDC SSO` federation & `Four-Eyes` dual control (owned by `TP-IMP-0041`); multi-node `Valkey Pub/Sub` revocation propagation (owned by `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0501`, `TP-FR-0502`, `TP-FR-0503`, `TP-NFR-0404`, `SLO-07`, `TP-BR-0101`..`0104`, `SM-05`, `AUTH-02`..`AUTH-07`, `AUTH-11`, `AUTH-13`, `AUTH-15`.
- **Dependencies:** `TP-IMP-0007`, `TP-IMP-0008`.
- **Design Refs:** [`ADR-0006`](../../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Sections 2 & 3, [`business-entity-map.md`](../../requirements/business-entity-map.md) (`SM-05`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`AUTH-02`..`15`).

## 4. API, Wire & Data Contracts
- **Canonical `Virtual Key` Format (`CR-06`):** `vk_tp_<env>_<32_base62_chars>`; persist `active_key_hash = SHA-256(rawKey)` (`32 bytes`) and on rotation move the prior digest into `previous_key_hash` alongside `grace_expires_at_ms`; store only `key_prefix` + `last_four` for audit display. Compare digests using `subtle.ConstantTimeCompare`.
- **`ConfigSnapshot` Contract:** Read-only immutable struct exposing `O(1)` lookup maps from `sha256_hash -> VirtualKeyView` and `(org_id, project_id, env_id) -> EffectiveConfig` with `0` `Mutex` locks on the read path.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`AUTH-02`):** Given an unbootstrapped server, When consuming the `Bootstrap Token` to create the initial `ORG_OWNER`, Then a second call presenting the same `Bootstrap Token` is hard-rejected with `410 bootstrap_already_completed`.
- **`AC-2` (`AUTH-05`):** Given `Virtual Key K1` in `CREATED_ACTIVE`, When rotated with `grace_seconds = 300`, Then state transitions to `ROTATING_DUAL_VALID` keeping both `K1` (`previous_key_hash`) and successor `K2` (`active_key_hash`) valid for 300 seconds; once `FakeClock.Advance(301s)` fires, `K1` is rejected with `401 virtual_key_expired`.
- **`AC-3` (`AUTH-06` & `SLO-07`):** Given an open SSE stream authenticated with `Virtual Key K1` (even if `K1` is currently `QUOTA_PAUSED`), When `RevokeVirtualKey(K1, emergency=true)` is invoked, Then state transitions directly to `REVOKED`, `ConfigSnapshot` swaps via `config_changelog` in `< 1 second`, the active SSE stream aborts immediately, and subsequent requests receive `401 virtual_key_revoked`.
- **`NEG-1` (`AUTH-04` & `AUTH-07` Scope / Plane Crossover):** Using `vk_tp_staging_...` against a `prod` environment → `403 environment_scope_denied`; using `vk_tp_live_...` on `/admin/v1/*` or an Admin session cookie on `/v1/chat/completions` → `401 invalid_credential_plane`.
- **`NEG-2` (`AUTH-11`, `AUTH-13` & `AUTH-15`):** `DEVELOPER` or `FINOPS_VIEWER` invoking plugin capability approval → `403 insufficient_role_permission`; injecting `"org_id": "<Org_B>"` in a JSON body from an `Org A` session is ignored/overridden by session context (`AUTH-13`); probing an `Org B` resource ID returns a uniform `404 resource_not_found` (`AUTH-15`).
- **`NEG-3` (`AUTH-03` CSRF):** Submitting a state-mutating request authenticated via browser session cookie with a missing or mismatched `X-CSRF-Token` → rejected with `403 csrf_validation_failed`.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/iam/... ./internal/config/snapshot/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/iam/... ./internal/config/snapshot/...`
- Data-race verification (`-race`) & `0 allocs/op` benchmark:
  - `go test -v -race ./internal/iam/... ./internal/config/snapshot/...`
  - `go test -bench=BenchmarkSnapshotLookup -benchmem ./internal/config/snapshot/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Never persist or emit `rawKey`, plaintext passwords, or raw session tokens in DB tables, logs, or OTel spans; every Admin endpoint defaults to `Deny` unless explicitly mapped to an `Action` constant.
- **Rollback:** Additive schema `0005_iam_keys.sql`; keys can be emergency-suspended via kill-switch flags without data loss.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (locking the 6 roles & `SM-05`), `TP-IMP-0007`, and `TP-IMP-0008` are complete.
- **DoD:** `100%` green across `AUTH-02..07, 11, 13, 15` under both `CGO_ENABLED=0` and `-race`, `0 allocs/op` on `ConfigSnapshot` hot-path lookups, and `REQUIRES_STRONG_REVIEW` sign-off.
