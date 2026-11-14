# Milestone Map, Release Exit Criteria & Phase Integration Gates — Tanpopo

> **Milestone Plan (`HANDOFF_VERDICT: GO` | `TP-IMP-0001..0006: DONE` | `READY_FOR_REVIEW: TP-IMP-0007`)**
> - **Document ID:** `PLAN-MS-01` | **Version:** `1.5.0-tp-imp-0007-ready-for-review` | **Date:** `2026-11-14`
> - **Authors:** Technical Program Manager & QA Lead
> - **Live Status:** Milestone `M0` (`TP-IMP-0001`..`0004`) plus `TP-IMP-0005` (`DEC-011`) and `TP-IMP-0006` (`CR-11` / `DEC-012` / `DEC-013` at commit `b929deb` & CI Run `43590868455`) are **`DONE` (`6 / 47`)**; **`TP-IMP-0007` (`1 / 47`) is `READY_FOR_REVIEW`** (`0` `READY` until Product Owner acceptance; `40` downstream units `0008`..`0047` remain `BLOCKED` behind their dependency, review, and `COND-PO-01`..`05` verification gates).
> - **Cross-References:** [`roadmap.md`](./roadmap.md), [`dependency-graph.md`](./dependency-graph.md), [`implementation-index.md`](./implementation-index.md), [`release-scope.md`](../product/release-scope.md), and [`release-readiness.md`](../quality/release-readiness.md).

---

## Overview of the 6 Execution Milestones & Integration Control Gates (`M0` → `M5`)

No downstream milestone may scale execution until the preceding milestone's **Integration Gate** passes `100%` across both automated verification suites and required human sign-offs:

---

### Milestone `M0`: A08 Specification Closure & Empirical Spike Lock (`Spec Closure & PoC Spikes`)
- **Goal:** Resolve all `11 CRITICAL` and `14 MAJOR` findings from Audit A08 once the Product Owner signs off on `CR-01`..`10`, and execute time-boxed empirical `SPIKE-01`..`04` benchmarks to lock the Wasm binary memory ABI (`POC-01`) and Tier-1 declarative template grammar (`POC-02`) before writing the first line of production code.
- **Included Implementation Units:** `TP-IMP-0001` .. `TP-IMP-0004` (`4` units).
- **Requirement / Gap Coverage:** Closes `GAP-01` .. `GAP-13`; empirically validates assumptions `ASM-002`, `ASM-004`, `ASM-005`, `ASM-006`.
- **Measurable Deliverables:**
  1. State machines `SM-01`..`09` free of dead-end states; `srs.md` and `acceptance-criteria.md` populated with `100%` explicit `AC` coverage for `TP-FR-0701`, `0702`, `0901`.
  2. Standalone `proto/tanpopo/extension/v1/runtime.proto` and `openapi/{admin-v1,dataplane-v1}.yaml` extracted and passing strict linters with `0 errors`.
  3. Schema in `data-model.md` completed across all missing tables (`26` canonical tables), composite `(org_id, id)` keys + `FORCE RLS`, `Project DEK` hierarchy, and Valkey `tp:v1:{org_id}:*` hash-tag key schema.
  4. Empirical benchmark report for `SPIKE-01`..`04` recorded in `ADR-0002` and `ADR-0005`.
- **Primary Risk:** Wasm linear-memory boundary in `SPIKE-02` exceeding the `5 ms p95` ceiling on per-chunk SSE transforms → triggers the fallback architecture invoking Wasm only on stream setup/teardown while using a native Host frame splitter mid-stream.
- **Integration Gate `GATE-M0` (`Spec & Spike Gate` — `COMPLETED`):**
  - Automated (Local Pre-CI): `0` compile/ref errors across `runtime.proto` (`protocompile`) and `openapi/*.yaml` (`yaml.v3`), live in-memory DDL execution on `SQLite` (`modernc.org/sqlite`), and static `48`-table / `FORCE RLS` parity checks on `postgres_schema.sql` *(live PostgreSQL 16 DDL execution remains `UNVERIFIED` and gated in `TP-IMP-0039`)*.
  - Human: **Product Owner** approved Stage 1 (`DEC-009`), Stage 2 (`DEC-010`), and **`TP-IMP-0005` Acceptance (`DEC-011`)** at commit `1f4f5a1` & CI Run `43584413913` (`SUCCESS`), promoting **`TP-IMP-0005` (`DONE`)** and unlocking **`TP-IMP-0006` & `TP-IMP-0007` (`READY`)**.

---

### Milestone `M1`: Core Foundation & External Provider Adapter Vertical Slice (`Foundation & Core Proof Vertical Slice`)
- **Goal:** Establish the `Apache-2.0` monorepo scaffold, Internet-blocked hermetic CI pipeline, three-tier envelope encryption vault, virtual key authentication, and `Blind Credential Broker`, and **prove the existential `EXT-ACCEPT-01` path end-to-end: scaffold, offline cassette-test, hot-install, capability-approve, and stream SSE inference through an `External Provider Adapter` (both Tier-1 Declarative and Tier-2 Wasm) without modifying or restarting Tanpopo Core**.
- **Included Implementation Units:** `TP-IMP-0005` .. `TP-IMP-0015` (`11` units).
- **Requirement Coverage (`SRS`):** `TP-FR-0101` (chat SSE), `0103`, `0104`, `0201`..`0204`, `0301`..`0306`, `0501`..`0503` (core), `0702` (SSRF), `0703`, `TP-NFR-0101`, `0102`, `0202`, `0401`..`0403`, `1001`.
- **Measurable Deliverables:**
  1. Clean monorepo layout (`cmd/tanpopo`, `internal/*`, `sdk/*`, `web/console/`), `Stage 0..2 CI` enforcing `testkit/netguard` blocking `100%` of non-loopback IPs outside `127.0.0.1/::1`.
  2. `SQLite WAL` store + `Envelope Vault` (`Master KEK -> Org DEK -> Project DEK`) + `O(1)` `Virtual Key` authentication backed by `ConfigSnapshot`.
  3. `tanpopo plugin init / validate / test / pack` SDK toolchain and hot-reloadable `Declarative` + `wazero` Sandbox + `Blind Broker` runtime.
  4. Automated core vertical-slice suite `EXT-SLICE-M1` (foundational steps of `EXT-ACCEPT-01`: hot-install `.tpkg` → approve capabilities → inject blind secret → invoke synchronous & SSE `/v1/chat/completions` with an unchanged Core binary hash; all 10 steps including `EU` Data Residency Routing, \$1.00 Budget Ledger enforcement, and `Blast-Radius` inspection are locked end-to-end in `TP-IMP-0032` / `GATE-M2`).
- **Primary Risk:** Goroutine leaks on abrupt client SSE disconnects or stalled `wazero` instances under infinite-loop guest modules (`RSK-001`, `RSK-004`).
- **Integration Gate `GATE-M1` (`Vertical Slice Proof Gate`):**
  - Automated: `100%` green across `CT-01`..`07`, `CT-10`..`11`, `CT-14`..`18`, `CT-24`..`26`, `FZ-01`..`03`, `FZ-05`, `EXT-SLICE-M1`, `go test -race ./...`, and `CanaryScanner` (`0` secret leaks).
  - Human: `Security Lead + Platform Architect` perform deep review (`REQUIRES_STRONG_REVIEW`) across all `wazero`, `Blind Broker`, and `SSRFGuard` trust boundaries.

---

### Milestone `M2`: Complete `MVP (v0.1)` — Routing, `Micro-USD` Ledger, Astryx Console, CLI & Single-Node Ops (`Usable Self-Hosted MVP`)
- **Goal:** Bring Tanpopo to **production-ready small/mid-scale self-hosted usability (`TTFGR < 3 minutes`)** with full unified + native passthrough protocols, 5 Official Reference Adapters, capability-aware + `Fail-Closed` data-residency routing, two-phase `micro-USD` budget ledger, OTel + hash-chained audit logs, 4-level emergency kill-switch, **Astryx `neutral`** Web Console achieving `0 fail` on `astryx doctor --built`, `tanpopo` CLI, and multi-OS distribution packages.
- **Included Implementation Units:** `TP-IMP-0016` .. `TP-IMP-0033` (`18` units).
- **Requirement Coverage (`SRS`):** **Achieves `100%` of all `36/36` `Must` FRs and `18/18` `Must` NFRs (single-node `v0.1` tier)**, plus `TP-FR-0105` (`Native Passthrough`) and core soft-budget alerting of `TP-FR-0803`.
- **Measurable Deliverables:**
  1. Complete `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`, RAM-bounded multimodal `blob_ref` streaming, `/native/{dialect}/*`, and 5 Official Adapter packages (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible/vLLM`, `Ollama`).
  2. Monotonic `Org ∩ Project ∩ Env` policy engine, `RPM/TPM/Concurrency` rate limiters, 6-step capability router + `SM-04` circuit breaker, `Two-Phase Hold` ledger (`EXACT/ESTIMATED/UNMETERED_FLAGGED`), `Drop-Oldest` OTel exporter, `audit_events` hash-chain, and `Kill-Switch < 1s`.
  3. Full `/admin/v1/*` (`ETag`/`409`), static Astryx `neutral` SPA embedded inside the `tanpopo` binary (14 `v0.1` screens, bundled local `Figtree` font), and Linux/macOS/Windows binaries + OCI images with `CycloneDX SBOM` + `Cosign` signatures.
- **Primary Risk:** Frontend scope creep delaying MVP launch (`RSK-002`) or Astryx raw-HTML layout violations.
- **Integration Gate `GATE-M2` (`MVP v0.1 Release Candidate Gate`):**
  - Automated: `100%` green across `Stage 0..4 CI` for `v0.1` scope **including all 10/10 steps of `EXT-ACCEPT-01` with invariant `PID` and `sha256sum(tanpopo)`**; `npx @astryxdesign/cli doctor --built web/console/dist` reports `0 fail`; `@axe-core/playwright` reports `0` `critical/serious` violations; `UJ-01` completes in `< 180s`; encrypted backup/restore (`.tpop.enc`) and `Crypto-Shredding < 60s` verified.
  - Human: **Product Owner** directly signs off on `GATE-REL-01`..`07` before authorizing the `v0.1.0` tag.

---

### Milestone `M3`: Ecosystem `v0.2` — `Authorized OAuth 2.1`, Marketplace/OCI, `MCP Governance Hub` & DX Tooling (`Agentic Tooling & Registry`)
- **Goal:** Expand Tanpopo into a unified governance gateway for both **Model Inference + MCP Tool Execution**, adding single-flight `Authorized OAuth 2.1` refresh brokering, signed static Marketplace index & private OCI registry pulls, `Dry-Run Route Explain`, `Playground`, and HMAC-signed `Webhooks`.
- **Included Implementation Units:** `TP-IMP-0034` .. `TP-IMP-0038` (`5` units).
- **Requirement Coverage (`SRS`):** All `Should` requirements (`TP-FR-0205`, `0307`, `0401`..`0404`, `0604`, full `0803`, `0904`, `TP-NFR-0602`) + `Bedrock`/`Azure`/`/v1/rerank` (`CR-01`).
- **Measurable Deliverables:**
  1. `OAuth 2.1` Broker (`PKCE S256`, `Client Credentials`, `Device Flow`) passing a 100-goroutine stampede test (`1` upstream refresh call) and `RFC 8707` audience/resource isolation.
  2. `/mcp` endpoint with paginated `tools/list` permission filtering (`> 200` tools), automatic quarantine on JCS `schema_hash` drift, zero-retry firewall for `mutating`/`destructive` calls, and human-in-the-loop approval compatible with `Claude Code` / `Cursor`.
  3. Screens `SCR-10` (MCP Hub), `SCR-15` (15-minute ephemeral `Playground`), `Route Explain` simulator (`SCR-08`), and HMAC-signed `Webhook` dispatcher.
- **Primary Risk:** `MCP Streamable HTTP` session-behavior divergence across agentic IDEs (`RSK-006`).
- **Integration Gate `GATE-M3` (`v0.2 Release Gate`):**
  - Automated: Passes `CT-19`..`22`, `AUTH-09`, `10`, `12`, `16`, `FZ-04`, and reference MCP client compatibility suites.
  - Human: `Security Lead + Product Owner` sign off on `DEC-004` / `POL-01` compliance across all OAuth connectors and approve the `v0.2.0` release gate.

---

### Milestone `M4`: Enterprise Scale `v0.3` — Stateless `PostgreSQL 16 + Valkey` HA Cluster, Advanced IAM/KMS & `A2A` Plane (`Stateless Scale-Out & Inter-Agent Plane`)
- **Goal:** Scale Tanpopo to a high-throughput multi-replica stateless cluster (`>= 1,000` concurrent SSE streams/node `TARGET`) with `< 5s` cluster-wide key/kill-switch revocation, zero-data-loss migration from SQLite to PostgreSQL, `OIDC SSO`, `Four-Eyes` dual control, `Cloud KMS / Workload Identity`, `Tier-3 Isolated Sidecar`, and the inter-agent `A2A` plane.
- **Included Implementation Units:** `TP-IMP-0039` .. `TP-IMP-0042` (`4` units).
- **Requirement Coverage (`SRS`):** All `Could` requirements (`TP-FR-0206`, `0405`, `0406`, `0504`, `1005`, `TP-NFR-0201`, `0301`, `0302`) + `CR-02` (`Tier-3`), `CR-09` (opt-in `Response Cache`).
- **Measurable Deliverables:**
  1. `PostgreSQL 16+` storage driver (monthly range partitioning + `FORCE RLS`), `Stage 2 Dual-DB CI` green simultaneously on SQLite and PostgreSQL, and `tanpopo db migrate-engine` achieving `100%` checksum parity.
  2. `Valkey` cluster coordinator using `tp:v1:{org_id}:*` hash-tags, 3-layer `< 5s` revocation propagation, `> 10s` blind-node circuit breaking, and role-split `control-plane` / `data-plane` Helm charts.
  3. Admin `OIDC SSO`, `Four-Eyes` approval workflow, `AWS/GCP KMS / Vault` root key wrapping, `mTLS` `Tier-3 Sidecar`, and `/a2a/*` plane enforcing sub-budget caps & `hop_count <= 5`.
- **Primary Risk:** Transactional semantics divergence between SQLite and PostgreSQL (`RSK-013`) or state-sync lag during cluster network partitions (`RSK-011`).
- **Integration Gate `GATE-M4` (`v0.3 HA & Enterprise Gate`):**
  - Automated: Passes `CT-23`, `CT-28`, `CT-32`, `AUTH-01`, `02`, `06`, `08`, `14`, `OPS-AC-03`, `06`, and 3-node chaos fault-injection drills (random Data Plane pod kill / Valkey outage).
  - Human: `SRE Lead + Security Lead + Product Owner` sign off on the `v0.3.0` gate.

---

### Milestone `M5`: `v1.0 GA` Production Hardening — 12-Month Contract Freeze, `Air-Gap Bundle`, Security/Perf Audit & Public Launch (`Production Freeze & Launch`)
- **Goal:** Replace every `TARGET / UNVERIFIED` label with empirical load-benchmark and timed disaster-recovery evidence, complete offline `Air-Gap` bundle tooling, unlock full Multi-Org GA administration, pass independent grey-box penetration testing, verify `100%` documentation-to-code parity, and submit the release dossier for **Product Owner `v1.0.0` launch sign-off**.
- **Included Implementation Units:** `TP-IMP-0043` .. `TP-IMP-0047` (`5` units).
- **Requirement Coverage (`SRS`):** Fulfills long-term commitments of `TP-FR-0307` (`Air-Gap`), `TP-FR-0501` (`Multi-Org GA`), `TP-NFR-0902` (`12-month SLA`), and all `GATE-REL-01`..`07` criteria. *(The two `Later` `Post-v1.0` items — `TP-FR-0106` Realtime WebRTC and `TP-FR-0804` Automated Invoice Reconciliation — remain explicitly out of `v1.0` scope per `release-scope.md`.)*
- **Measurable Deliverables:**
  1. Reproducible `k6` benchmark report empirically verifying `SLO-01`..`08` and timed drill transcripts for `DR-01`..`04` (`RTO < 15m`).
  2. Continuous `>= 4h` fuzzing across `FZ-01`..`06` (`0` crashes), `tanpopo plugin migrate-check` guaranteeing `N-1` compatibility, and `100%` remediation verification for `Critical/High` penetration-test findings.
  3. `tanpopo bundle export/verify/import` passing air-gapped network-isolated tests (`CT-31`, `OPS-AC-11`), user/SDK documentation portal achieving `100%` code parity, and the complete `Release Readiness Dossier`.
- **Integration Gate `GATE-M5` (`v1.0 GA Human Launch Gate`):**
  - Automated: `100%` green across `GATE-REL-01`..`06`, `0` open `Sev-0`/`Sev-1` defects.
  - Human (**MANDATORY**): **Direct Product Owner sign-off on `TP-IMP-0047` (`GATE-REL-07`)** — CI/Agents are **strictly prohibited from automatically publishing or tagging a public release** without this explicit human authorization.
