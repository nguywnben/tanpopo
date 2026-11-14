# `TP-IMP-0019` — Hierarchical Policy Engine (Monotonic `Org ∩ Project ∩ Env` Narrowing, `Fail-Closed`) & `RPM/TPM/Concurrency` Rate Limiters

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0019` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0009`, `TP-IMP-0015` — `GAP-01, 11`)* |
| **Size** | `M` (`internal/policy/engine/*`, `internal/ratelimit/*`, `db/migrations/sqlite/0006_policies.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(`Fail-Closed` access-control gate & child-policy relaxation prevention)* |
| **Technical Owner** | `Principal Security / Policy Engineer` |

---

## 1. Intent
Implement the 4-tier hierarchical policy merge engine (`Org ∩ Project ∩ Environment ∩ VirtualKey`) governed by **Monotonic Narrowing (`TP-FR-0701`)**, `Fail-Closed` evaluation across all security gates and model/region/plugin allowlists, and the 3-dimensional single-node in-memory rate limiter (`RPM` — requests/minute, `TPM` — tokens/minute, and `MaxConcurrentStreams` — maximum concurrent active streams) behind a clean `RateLimiterPort` interface ready for `Valkey` in `v0.3`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/policy/engine/merge.go` (set-intersection allowlists `Allowlist = A_org ∩ A_proj ∩ A_env ∩ A_vkey`, set-union denylists `Denylist = D_org ∪ D_proj ∪ D_env ∪ D_vkey`, minimum numeric caps `Limit = min(L_org, L_proj, L_env, L_vkey)`, and write-time validation returning `422 policy_relaxation_forbidden` whenever a child scope attempts to broaden permissions restricted by an ancestor scope).
  - `internal/policy/engine/evaluator.go` (`< 0.2 ms` policy evaluation atop `ConfigSnapshot`, emitting structured denial codes `policy_denied_model`, `policy_denied_region`, `policy_denied_max_context`).
  - `internal/ratelimit/limiter.go` (in-memory `GCRA / Token Bucket` backed by monotonic `Clock` for `RPM` and pre-estimated + post-reconciled `TPM`, plus `defer`-guarded concurrency semaphores for `MaxConcurrentStreams`, returning canonical `429 rate_limit_exceeded` with `Retry-After` and `X-RateLimit-Remaining-*` headers).
  - `db/migrations/sqlite/0006_policies.sql` (`policies`, `org_extension_policies`).
- **Out-of-Scope:** Cluster-wide distributed `Valkey Lua` rate-limit counters (owned by `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0701`, `TP-FR-0702`, `TP-FR-0703`, `TP-BR-0501`, `TP-BR-0502`, `TP-AC-SEC-05`, `GAP-11`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0015`.
- **Design Refs:** [`routing-and-policy.md`](../../specifications/routing-and-policy.md) (`SPEC-CORE-01` Sections 1 & 2), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`GAP-11`, `CR-04`).

## 4. API, Wire & Data Contracts
- **Monotonic Narrowing Invariant (`Monotonic Narrowing`):** If an `Org` restricts `allowed_regions = ["eu-central-1", "eu-west-1"]`, any attempt to persist a `Project` policy containing `"us-east-1"` is rejected at write time with `422 policy_relaxation_forbidden`; even if historical child rows predate a newly tightened `Org` policy, runtime `Org ∩ Project` intersection deterministically strips `"us-east-1"`.
- **`Fail-Closed` Matrix:** If a policy record is corrupted or fails to load from `ConfigSnapshot` → reject with `503 policy_engine_unavailable`, strictly never failing open.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `Org` sets `RPM = 100`, `Project` sets `RPM = 30`, and `VirtualKey` sets `RPM = 10`, When firing 11 requests within the same second (`FakeClock`), Then the first 10 succeed and request #11 is rejected with `429 rate_limit_exceeded` and `Retry-After > 0`.
- **`AC-2`:** Given `MaxConcurrentStreams = 2` on a `VirtualKey`, When opening 2 concurrent stalled SSE streams and submitting stream #3, Then stream #3 receives `429 concurrency_limit_exceeded`; the instant either of the first 2 streams disconnects, a subsequent stream succeeds immediately (`0` leaked slots even on abrupt client TCP resets).
- **`NEG-1` (`GAP-11` Relaxation Attempt):** A `TEAM_ADMIN` submitting a child policy attempting to re-enable a `ProviderAdapter` or model denylisted by `ORG_OWNER` at the `Org` level → rejected with `422 policy_relaxation_forbidden`.
- **`NEG-2` (`TPM Burst Abuse`):** Submitting a single request whose estimated context + `max_tokens` exceeds the key's `TPM` ceiling → rejected prior to dispatching any upstream request.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/policy/... ./internal/ratelimit/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/policy/... ./internal/ratelimit/...`)*
- Data-race (`-race`): `go test -v -race ./internal/policy/... ./internal/ratelimit/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `ReleaseConcurrencySlot` is unconditionally guarded by `sync.Once` inside the HTTP handler's `defer` lifecycle so neither double-release counter underflow nor permanent slot starvation can occur.
- **Rollback:** Additive schema `0006_policies.sql`; default `Org` baseline preserves fail-closed network egress rules.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` (`GAP-11` `AC` closure), `TP-IMP-0009`, and `TP-IMP-0015` are complete.
- **DoD:** Achieves `>= 90%` test coverage across policy intersection algebra & `RPM/TPM/Concurrency` limiters; passes `REQUIRES_STRONG_REVIEW`.
