# 09 — Enterprise Readiness, Scale & Compliance Architecture Research

- **Document ID:** `TEEP-01-DOC-09`
- **Research Execution Date:** `2026-11-27`
- **Epistemic Guardrail (`COND-PO-01`):** Measured RC9 baseline numbers are cited from [`docs/quality/benchmark-evidence.md`](../../quality/benchmark-evidence.md) and [`docs/architecture/spike-results.md`](../../architecture/spike-results.md) (`[VERIFIED_FACT]`). Every projected throughput, latency, offboarding, or availability target below is explicitly marked **`HYPOTHESIS / UNVERIFIED`** for Stage-3/4 verification. Zero enterprise certifications or unexecuted benchmarks are claimed.

---

## 1. Large-Scale Multi-Tenancy, 7-Level Hierarchy & Noisy-Neighbor Isolation

### 1.1 Backward-Compatible 7-Tier Organizational Hierarchy (`Enterprise -> Org -> BU -> Dept -> Team -> Project -> Env`)
- **Problem:** RC9's flat `Organization -> Project -> Environment -> VirtualKey` spine (`48` composite `(org_id, id)` FK tables) lacks Business Unit / Department / Team delegation, matrix cost-center chargeback, and multi-subsidiary holding-company rollup views.
- **Portable Expand-Only Architecture (`SQLite` + `PostgreSQL 16+` Parity):**
  - Preserve `organizations (org_)` as the hard cryptographic and database boundary (`Org DEK` + `FORCE RLS` + Valkey `{<org_id>}` slot + JCS `audit_events` chain).
  - Add optional `enterprises (ent_)` umbrella above `organizations.enterprise_id` for holding-company executive rollups across legally separate subsidiaries (e.g., *Acme EU GmbH* vs *Acme US Inc*).
  - Add `org_units (ou_)` inside `organizations` (`unit_kind IN ('BUSINESS_UNIT', 'DEPARTMENT', 'TEAM', 'WORKSPACE', 'CUSTOM')`, `materialized_path VARCHAR(280)` bounded `depth <= 6`, `cost_center_code`, `gl_account_code`) referenced by `projects.org_unit_id` and `role_bindings.scope_type = 'ORG_UNIT'`.
  - **Zero Hot-Path Penalty:** `ConfigSnapshot` pre-flattens each `(org_id, project_id, env_id)` policy/budget ancestry chain into a slice of `<= 8` pointers at compile time (`<= 500 ns/op`, `0 allocs/op` — **`HYPOTHESIS / UNVERIFIED`** vs measured `256.2 ns/op` 4-tier baseline).

### 1.2 Five-Layer Noisy-Neighbor Containment
1. **Hierarchical Token, Stream & Byte Bulkheads:** Shared aggregate `ORG` / `ORG_UNIT` / `PROJECT` Valkey Lua buckets (closing RC9's per-key-only clamping gap) + `max_inflight_input_bytes` to prevent `128k+` context prefill starvation.
2. **Adaptive Concurrency Control (`Gradient2`):** Per-deployment `RTT_min / RTT_sample` EWMA shedding `BATCH_BACKFILL` traffic before `INTERACTIVE` or `MISSION_CRITICAL` SSE streams hit upstream `429`/`503` cascades.
3. **Shuffle Sharding (`k = 2` of `N = 16` Pods):** Deterministic double-hashing restricts any noisy tenant's Wasm/GC blast radius to `2/16` pods (`0.83%` exact co-tenant collision probability).
4. **Deficit Round-Robin (`DRR`) Fair Queuing:** Per-tenant virtual queues on Wasm instance pools and async settlement batchers under `> 70%` contention.
5. **Dedicated Data-Plane Cell Pools (`--pool-group=tier0-pci`):** Physical CPU/memory/egress isolation subscribing to filtered `config_changelog` feeds from a shared Control Plane.

---

## 2. Enterprise Identity, Policy-as-Code & Secretless Workload Auth (`CGO_ENABLED=0`, `Apache-2.0` Clean)

| Capability | Pure-Go / Permissive Stack | Key Security & Operational Design Requirements |
|---|---|---|
| **Multi-Org OIDC + Hardened SAML 2.0** | Existing `internal/iam/oidc` (wired per-org) + `crewjam/saml` (`BSD-2-Clause`) + `russellhaering/goxmldsig` (`Apache-2.0`) | Strict pre-parser blocking DOCTYPE/XXE & `<NameID>` comment truncation; signed-DOM validation defeating `XSW 1–8`; `tp:v1:{org}:saml_jti:*` replay cache; persistent encrypted DB/Valkey Admin sessions replacing RC9's single-node RAM map; WebAuthn/Passkey + TOTP MFA for local break-glass accounts. |
| **SCIM 2.0 Automated Lifecycle (`RFC 7643`/`7644`)** | `elimity-com/scim` (`MIT`, pure Go) behind `scim_tp_*` SHA-256 bearer tokens | Normalizes Entra ID `"True"`/`"False"` PATCH quirks & Okta group diffs. Atomic `< 5s` (`HYPOTHESIS / UNVERIFIED`) offboarding kill-chain: `PATCH active=false` / `DELETE` suspends principal, revokes all cookies, Admin PATs, personal MCP OAuth grants, and active SSE/MCP streams via `tp:v1:pubsub:revocations`. |
| **Two-Stage Policy-as-Code (`Native` + Candidate `Cedar`, `[DESIGN_PROPOSAL — TEEP-03]`)** | Stage 1: Native `policy/engine` (`256.2 ns/op` `[RUNTIME_VERIFIED]`) + Stage 2 candidate: `cedar-policy/cedar-go` (`v1.9+`, `Apache-2.0`, pure Go) + optional `google/cel-go` | Non-Turing-complete, schema-validated ABAC (`Principal`, `Workload`, `Action`, `ModelDeployment`, `McpTool`, `A2aAgent`). Monotonic **`forbid` overrides `permit`** ordering prevents delegated `BU_ADMIN` or `TEAM_ADMIN` rules from widening ancestor org policies within evaluated policy stages. |
| **Scoped Admin Service Accounts & PATs** | `sak_tp_*` & `pat_tp_*` (`SHA-256` stored, wired into `ServerConfig.TokenVerifier`) | Unblocks `DEF-004` remote CLI, CI/CD, and Terraform (`If-Match` guarded); mandatory `expires_at_ms <= 365d`, CIDR allowlists, fine-grained `(resource, action)` scopes, and strict plane rejection (`AUTH-07`) on `/v1/*`. |
| **Secretless Workload Identity Federation (`WIF`)** | `spiffe/go-spiffe/v2` (`Apache-2.0`) + K8s Projected SA OIDC + Cloud/CI OIDC (`workload_identity_bindings`) | Direct Data-Plane verification against SSRF-guarded cached JWKS / SPIFFE Trust Bundles, plus high-RPS `/v1/sts/token` exchange minting 5-minute `wst_tp_*` session tokens (`O(1)` SHA-256 lookup `< 500 ns` `HYPOTHESIS / UNVERIFIED`) — eliminating static `vk_tp_*` secret sprawl. |

---

## 3. Regulatory, Privacy & Sovereign Compliance Control Architecture (`2026–2027` Technical Controls)

1. **EU AI Act Technical Controls (`Regulation (EU) 2024/1689` — Aug 2025 GPAI, Aug 2, 2026 Art. 50, High-Risk Art. 12/14/19/26):**
   - Supports `>= 180-day` immutable metadata retention (`Art. 19`), model/adapter digest + policy + `SM-06` human-oversight logging (`Art. 12`, `Art. 14`), and machine-readable output provenance headers (`X-Tanpopo-AI-Generated: true`, `X-Tanpopo-Model-Provenance`, `Art. 50`) to assist customer compliance programs.
2. **GDPR Art. 17 ("Right to Erasure") vs Immutable Audit/Financial Logs (`Pseudonymization & Salt-Shredding Proposal`):**
   - `C2`/`C3` tables store `0` prompt/completion bytes by default. Subject identifiers in `audit_events` and `usage_settlements` are written as `psu_ + hex(HMAC-SHA256(subject_link_key, subject_id))` where `subject_link_key` lives in mutable `subject_privacy_keys`. Deleting a subject destroys `subject_privacy_keys` + purges opt-in debug/cache blobs while keeping `RFC 8785 JCS` audit hashes and `micro-USD` financial ledgers intact (supporting customer `GDPR Art. 17(3)(b)/(e)` & `Recital 26` workflows, subject to organizational DPO review).
   - Adds optional hourly **External Chain-Head Witnesses** (KMS-signed or WORM S3 Object Lock) to detect tail-truncation by privileged DBAs.
3. **HIPAA, SOC 2 Type II, ISO/IEC 42001:2023 & Dual-Custody Legal Hold (`SEC 17a-4`) Evidence Controls:**
   - Connection-level `baa_covered` & `upstream_zdr_verified` routing fences (auto-injecting `store: false`), reversible in-RAM PII session tokenization (`<PERSON_01>`), **`RFC 9180 HPKE` public-key-only encryption** to external WORM legal-hold buckets (`0` private archival decryption keys on gateway nodes), and 1-command signed `compliance-pack export` archives.

---

## 4. High Availability, High-Throughput Scalability Proposal (`5,000–20,000+ RPS` `[HYPOTHESIS]`) & Multi-Region Cells

### 4.1 Resolving RC9's Four Measured Throughput Bottlenecks (`109.48 req/s` SQLite / `56.86 req/s` 2-vCPU HA `[RUNTIME_VERIFIED]`)

| RC9 Bottleneck (`[RUNTIME_VERIFIED]` / `[SOURCE_VERIFIED]`) | Exact Root Cause in Code | Candidate Architectural Solution (`[DESIGN_PROPOSAL — TEEP-03]`) | Safety / FinOps Invariant Preserved |
|---|---|---|---|
| **1. Per-Request TCP/TLS Handshakes** | `blind_broker.go:876,885` creates a new `http.Transport` with `DisableKeepAlives: true` per call (`TIME_WAIT` exhaustion in `< 6s` at 5k RPS). | **Epoch-Fenced Pooled Keep-Alive & HTTP/2 Transports** keyed by `(org_id, conn_id, policy_hash, origin)` (`MaxIdleConnsPerHost: 256`). | Pre-write hook re-verifies `ConfigSnapshot` active status + pinned peer `netip.Addr` against `SSRFGuard`; `config_changelog` delta calls `CloseIdleConnections()` immediately. |
| **2. Hot-Path SQL Ledger Serialization** | Every request runs 2 synchronous SQL TXs (`Reserve` + `Settle`) hitting `SetMaxOpenConns(1)` (`sqlite/store.go:104`) or PG hot-row `FOR UPDATE` + `fsync`. | **Hierarchical Escrow-Backed Pod Budget Leases:** Pods lease bounded slices (`min(USD 5, 2% headroom)`, `TTL=10s`) into `atomic.Int64` (`~30 ns` `[HYPOTHESIS]`); cut over to Valkey Lua below `10%` Low-Water Mark. | Leased `micro-USD` is pre-escrowed in Valkey/PG so aggregate pod allocations stay bounded within configured hard ceilings (`[DESIGN_PROPOSAL]`). |
| **3. Per-Request Disk `fsync` on Traces & Settlements** | Synchronous row-by-row `INSERT` on `usage_settlements`, `request_traces_meta`, `idempotency_records`, and `response_cache_entries`. | **Split SQLite `readerDB`/`writerDB` + `5–10 ms` Group-Commit WAL / `pgx.CopyFrom` Batcher** (`1 fsync` per `256` settlements) + Valkey fast-path for idempotency & L1/L2 cache + async ClickHouse/Parquet `TELEMETRY_EXPORTER`. | Durable group-commit ack before settlement completion; avoids OLTP B-tree bloat at `10^8+` requests/day. |
| **4. `O(N_orgs)` Control-Plane Reloads (`ARCH-OBS-01`)** | `snapshot/manager.go:857` runs `2 + 7 * N_orgs` RLS queries across every pod on every epoch bump. | **Incremental Epoch Delta Reload** (`WHERE config_epoch > last_epoch`) + signed OCI/Valkey snapshot bundles. | `O(1)` delta sync (`< 5 ms` `HYPOTHESIS / UNVERIFIED`) across `10,000+` organizations. |

### 4.2 Recommended Multi-Region Active-Active Topology
- **Sovereign Cell-Based Data Planes:** Regional cells (`eu-central-1`, `us-east-1`, `ap-northeast-1`) terminate regional traffic locally (`0` cross-border prompt/trace bytes, surviving transoceanic WAN partitions) backed by signed `ConfigSnapshot` bundles and dynamically rebalanced **Regional Budget Envelopes** (`SUM(RegionEnvelopes) <= OrgHardLimit`).

---

## 5. Evidence-Backed Quality Attribute Scenarios (`QAS-ENT-01` .. `QAS-ENT-12` — `HYPOTHESIS / UNVERIFIED`)

| Scenario ID | Attribute | Workload / Fault Stimulus | Target Architectural Response & Falsifiable Metric (`HYPOTHESIS / UNVERIFIED`) |
|---|---|---|---|
| **`QAS-ENT-01`** | **Cluster Throughput & Overhead** | `10,000 RPS` mixed `/v1/chat/completions` (`70%` Tier-1, `30%` Tier-2 Wasm) across `4x` `4-vCPU` DP pods + PG16 + 3-shard Valkey (`20 ms` mock upstream). | Sustained **`>= 10,000 RPS`** (`>= 2,500 RPS/pod`), gateway internal overhead **`p95 < 2.5 ms`**, **`p99 < 8.0 ms`**, `0.00%` internal `5xx`, `0` ceiling overspend (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-02`** | **Standalone SQLite Unlock** | `1,500 RPS` short requests on single `2-vCPU / 2 GiB` Mode A node with split read/write pools + `5 ms` group-commit + pooled broker. | Rises from measured `109.48 req/s` baseline to **`>= 1,500 req/s`** with `p95 < 15 ms` internal wait (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-03`** | **SCIM 2.0 Offboarding SLA** | IdP sends `PATCH /scim/v2/Users/{id}` (`active: false`) while user holds active cookies, PATs, MCP OAuth grants, and 3 live SSE streams across 6 pods. | `100%` of sessions, PATs, OAuth grants, and live SSE/MCP streams terminated cluster-wide in **`p99 < 5.0 s`** (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-04`** | **Noisy-Neighbor Isolation** | 1 tenant bursts `20,000 RPS` of `256 KiB` payloads hitting a `14 ms` Tier-2 Wasm guardrail on a 16-pod `k=2` shuffle-sharded cluster. | `99%` of co-tenant orgs observe **`< 0.5 ms` `p99` overhead delta** and `0.00%` dropped requests (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-05`** | **Secretless WIF Resilience** | `500` K8s pods present `JWT-SVID` / SA tokens while upstream JWKS endpoint returns `503` for `10 minutes`. | Stale-while-revalidate JWKS cache (`<= 60m` known `kid`) sustains **`100%` auth availability** at `p95 < 0.15 ms` (`< 0.01 ms` with `wst_tp_*`) (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-06`** | **Budget Exhaustion Precision** | `100` concurrent callers across `8` pods race against `USD 5.00` remaining budget (`< 10%` Low-Water Mark). | Pods flush local lease remainders and serialize via Valkey Lua `EVALSHA` with **`0` `micro-USD` unauthorized pre-hold overrun** (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-07`** | **GDPR Art. 17 vs Audit Chain** | DPO executes `DELETE /admin/v1/privacy/subjects/{id}` across `10,000` historical `audit_events` and `usage_settlements` rows. | `subject_privacy_keys` destroyed in **`< 15 s`**; `VerifyAuditChain` passes `100%` (`0` broken hashes, `0` linkable PII) (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-08`** | **Transoceanic WAN Partition** | `4-hour` complete WAN cut isolates `eu-central-1` cell from global Control Plane. | EU cell serves against local PG16/Valkey & EU quota envelope at **`99.99%` cell availability** and `0` cross-region egress (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-09`** | **10,000-Org Control-Plane Scale** | Global extension quarantine + VKey rotation on a cluster hosting `10,000` organizations (`ARCH-OBS-01` remediation). | Quarantine DB commit + Valkey broadcast completes in **`p99 < 50 ms`** (`O(1)` vs RC9 `~47.5 s`) and incremental `Snapshot.Refresh` in **`p99 < 25 ms`** (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-10`** | **Zero-Drop Rolling Upgrade** | Rolling restart of `4` Data-Plane pods under `2,000 RPS` active SSE streaming (`15s` streams). | `preStop` readiness fence + graceful SSE drain eliminates RC9's `1/575` (`0.17%`) drop rate, achieving **`0` dropped requests (`100.00%` completion)** (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-11`** | **PostgreSQL Vault KEK Rotation Integrity** | `OPERATOR` attempts `POST /admin/v1/ops/vault/retire-kek` on a multi-org PG16 `FORCE RLS` cluster while `>= 1` org DEK remains wrapped under the retiring KEK, then re-runs after full DEK rewrap across `4` DP pods. | First call deterministically rejects with **`409 org_deks_not_rewrapped`** (`0` orphaned tenant DEKs); post-rewrap retirement scrubs KEK material from all `4` pods within **`p99 < 5.0 s`** (`HYPOTHESIS / UNVERIFIED`). |
| **`QAS-ENT-12`** | **Air-Gap Zero-Egress Bundle & Update Lifecycle** | Disconnected enclave (`0` WAN routes, packet-capture active) boots Console across `15` locales, imports a `.tpbundle` + offline advisory/release manifest, and runs `doctor`. | `100%` signature, advisory, and Console workflows succeed with **`0` external DNS/TCP/HTTPS egress attempts** (`HYPOTHESIS / UNVERIFIED`). |
