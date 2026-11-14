# `TP-IMP-0041` — `Enterprise OIDC SSO` Federation, `Four-Eyes` Dual Control, External `Cloud KMS / Vault`, `Workload Identity` & Opt-In `Response Cache`

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0041` |
| **Milestone / Epic** | `M4` (`v0.3 Clustered HA, Enterprise Security & A2A`) · `EP-08` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0034`, `TP-IMP-0039` — `GAP-01, 09`)* |
| **Size** | `L` (`go.mod`, `internal/iam/oidc/*`, `internal/security/foureyes/*`, `internal/security/vault/kms/*`, `internal/ingress/cache/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(`OIDC JWT/JWKS` verification, `Four-Eyes` dual-control approvals, external KMS integration & strict cross-tenant `Response Cache` isolation `FMEA-03`)* |
| **Technical Owner** | `Principal Enterprise Security Architect` |

---

## 1. Intent
Deliver the enterprise security capability suite (**100% open-source under `Apache-2.0`** per Tanpopo's core strategic differentiator, with zero proprietary SSO/RBAC paywalls — `SD-G1-05`):
1. **`Enterprise OIDC SSO` Login & Group-to-6-Role `RBAC` Mapping (`TP-FR-0504`, `AUTH-01`):** Verify `ID Token` signatures against key-rotating `JWKS` endpoints, strictly validate `iss`, `aud`, `exp`, and `nonce`, and map IdP `groups` claims deterministically to `(org_id, role)` bindings with zero cross-organization privilege escalation.
2. **`Four-Eyes Dual Control` Governance (`AUTH-12`):** When dual-control policy is enabled for an organization, high-impact administrative mutations (activating an `unverified-external` plugin in `prod`, rotating/destroying root keys, clearing an incident Kill-Switch) require 2 distinct administrators (`initiator_id != approver_id`).
3. **External Root Key Providers (`AWS KMS`, `GCP KMS`, `HashiCorp Vault Transit`), Per-Project Root Key Destruction (`CR-12` Enterprise Path) & Keyless Cloud Federation (`Cloud Workload Identity / IRSA` — `TP-FR-0206`):** Support optional **per-project external KMS/Vault root keys** (wrapping per-project keys directly under distinct external KMS keys rather than relying solely on a single shared instance-wide `Master KEK`), so destroying a project's dedicated external KMS key renders even historical pre-shred cold backups holding that database snapshot permanently undecryptable without impacting sibling organizations/projects or rotating the global `Master KEK` (`DEC-017` / `CR-12`).
4. **Default-Disabled `Opt-In Exact-Match Response Cache` (`CR-09`, `CT-28`, `FMEA-03`):** Disabled by default (`disabled`); when explicitly enabled by a project for deterministic requests (`temperature == 0`, non-streaming or controlled replay), cache keys MUST compute **`SHA-256(JCS({ org_id, project_id, env_id, policy_revision, model_deployment_id, tools_schema_hash, canonical_request }))`** and seal cached entries under the `Project DEK`, **categorically preventing** cross-organization or cross-project response leakage.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `go.sum` (adding minimal OIDC/JWKS & external KMS client dependencies).
  - `internal/iam/oidc/provider.go` (`OIDC Authorization Code + PKCE + Nonce` flow, `JWKS` caching routed through `SSRFGuardDialer`).
  - `internal/security/foureyes/service.go` (dual-administrator change-request queue `admin_change_requests`).
  - `internal/security/vault/kms/*.go` (`KEKProvider` implementations for external KMS/Vault wrapping `Org DEKs`).
  - `internal/security/broker/workload_identity.go` (exchanging ambient workload OIDC tokens for short-lived `Bedrock` / `Vertex AI` credentials governed by explicit Host-level allowlists).
  - `internal/ingress/cache/exact_cache.go` (response cache strictly isolated by `(org_id, project_id)` + encrypted under `Project DEK`).
- **Out-of-Scope:** Fuzzy vector-based `Semantic Similarity Cache` — intentionally excluded from `v1.0` due to cross-context sensitive-data leakage risks.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0206`, `TP-FR-0504`, `AUTH-01`, `AUTH-02`, `AUTH-12`, `CT-28`, `FMEA-03`, `CR-09`.
- **Dependencies:** `TP-IMP-0034`, `TP-IMP-0039`.
- **Design Refs:** [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Sections 2.3 & 4, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-03`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`AUTH-01`, `AUTH-12`, `CT-28`).

## 4. API, Wire & Data Contracts
- **Anti-Cache-Poisoning & Zero-Leakage Invariants (`FMEA-03` & `CT-28`):**
  - Error responses, truncated streams, responses containing side-effecting tool calls, and requests carrying `Cache-Control: no-store` are never cached.
  - Two projects `P1` and `P2` submitting byte-identical prompts produce 2 completely distinct cache keys encrypted under 2 distinct `Project DEK` keys — guaranteeing `0%` cross-read capability.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`AUTH-01` OIDC Login & Group-to-Role Sync):** Authenticating via `FakeOAuthIdP` (OIDC mode) verifies `RS256/ES256` signatures, maps the `tanpopo-sec-reviewers` group to the canonical `SECURITY_REVIEWER` role, and automatically revokes privileges when group membership is removed on session refresh.
- **`AC-2` (`AUTH-12` Four-Eyes Enforcement):** With `Four-Eyes` enabled, when `Admin 1` initiates activation of an external plugin, the mutation pauses in `PENDING_SECOND_APPROVER`; if `Admin 1` attempts self-approval, it fails with `403 self_approval_forbidden`; activation succeeds only once `Admin 2` approves.
- **`AC-3` (`CT-28`, `FMEA-03` & `CR-12` Per-Project External Root Key Destruction):** When `Org A / Project 1` enables caching and submits prompt `Q`, a subsequent byte-identical prompt `Q` from `Org B` or `Org A / Project 2` results in a deterministic **`CACHE MISS`**; shredding `Project 1`'s `Project DEK` (and, when external KMS/Vault per-project root keys are enabled per `DEC-017` / `CR-12`, destroying `Project 1`'s dedicated external KMS root key) immediately renders all `Project 1` cache entries and historical cold snapshots cryptographically undecryptable without impacting sibling projects or rotating global keys.
- **`NEG-1` (`OIDC Token Forgery`):** Presenting an `ID Token` with `alg: "none"`, mismatched `aud`, mismatched `nonce`, or an unknown signing key absent from `JWKS` is hard-rejected with `401 invalid_oidc_token`.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/iam/oidc/... ./internal/security/foureyes/... ./internal/security/vault/... ./internal/ingress/cache/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/iam/oidc/... ./internal/security/foureyes/... ./internal/security/vault/... ./internal/ingress/cache/...`)*
- Race detector (`-race`): `go test -v -race ./internal/iam/oidc/... ./internal/security/foureyes/... ./internal/security/vault/... ./internal/ingress/cache/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `Response Cache` defaults to `enabled = false` at every scope; initial `Bootstrap Token` exchange remains permanently disabled once organization bootstrap completes (`AUTH-02`).
- **Rollback:** Toggling `cache.enabled` or `foureyes.enabled` off reverts cleanly to baseline behavior without data loss.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (adding the formal `Response Cache` requirement ID per `CR-09`), `TP-IMP-0034`, and `TP-IMP-0039` are complete.
- **DoD:** `AUTH-01, 02, 12`, `CT-28`, and `FMEA-03` suites passing under `-race`, with `REQUIRES_STRONG_REVIEW` sign-off achieved.
