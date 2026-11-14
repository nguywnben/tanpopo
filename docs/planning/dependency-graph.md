# Acyclic Dependency Graph (`DAG`), Critical Path, Environment Workflow & Execution Order — Tanpopo

> **Dependency Orchestration & Build Infrastructure Plan (`HANDOFF_VERDICT: GO` | `TP-IMP-0001..0009: DONE` | `READY: TP-IMP-0010, TP-IMP-0011`)**
> - **Document ID:** `PLAN-DAG-01` | **Version:** `1.10.0-tp-imp-0009-done-unlock-0011` | **Date:** `2026-11-14`
> - **Authors:** Staff Engineer, Technical Program Manager & QA Lead
> - **Live DAG Frontier:** `TP-IMP-0001`..`0009` = **`DONE`** (`TP-IMP-0005`..`0009` accepted via `DEC-011`..`DEC-019`; `TP-IMP-0009` at commit `c3e3dd93` & CI Run `43615717101`); **`TP-IMP-0010` & `TP-IMP-0011` = `READY` (`2 / 47` units)**; `TP-IMP-0012`..`0047` (`36 / 47`) = **`BLOCKED`** behind upstream `Dependencies`, `REQUIRES_STRONG_REVIEW`, and `COND-PO-01`..`05` gates.
> - **Mathematical Acyclicity Proof (`Verified Acyclic`):** Every dependency edge `TP-IMP-u -> TP-IMP-v` across all `47` units satisfies the strict monotonic index invariant `u < v`, proving the graph is a **Directed Acyclic Graph (`DAG` — `0` cycles, `0` orphan dependency IDs)**.

---

## 1. End-to-End Implementation Dependency Graph (`End-to-End Implementation Dependency DAG`)

```mermaid
flowchart TD
    GateOwner["ROOT GATE: Product Owner Sign-Off on Gates G1-G3 + CR-01..10 & SPIKE Authorization"]

    subgraph M0["M0: A08 Spec Closure & Spikes"]
        U01["TP-IMP-0001: Spec Closure A (SM, SRS, AC)"]
        U02["TP-IMP-0002: Spec Closure B (Proto, OpenAPI, CLI)"]
        U03["TP-IMP-0003: Spec Closure C (DDL, DEK, RLS, Valkey)"]
        U04["TP-IMP-0004: Spikes SPIKE-01..04 & Freeze Wasm ABI"]
    end

    subgraph M1["M1: Foundation & External Adapter Vertical Slice"]
        U05["TP-IMP-0005: Monorepo, Apache-2.0 & Floor Guard CI"]
        U06["TP-IMP-0006: Hermetic Testkit & FakeLLMServer"]
        U07["TP-IMP-0007: Storage Port & SQLite WAL"]
        U08["TP-IMP-0008: Envelope Vault (KEK/Org/Project DEK)"]
        U09["TP-IMP-0009: IAM, RBAC, Virtual Keys & ConfigSnapshot"]
        U10["TP-IMP-0010: SSRFGuard & Blind Credential Broker"]
        U11["TP-IMP-0011: Package Verifier, SM-01 & tanpopo.lock"]
        U12["TP-IMP-0012: Tier-1 Declarative Engine"]
        U13["TP-IMP-0013: Tier-2 Wazero Wasm Sandbox Pool"]
        U14["TP-IMP-0014: Plugin SDK & Cassette Test Harness"]
        U15["TP-IMP-0015: /v1/chat SSE + EXT-ACCEPT-01 Vertical Slice"]
    end

    subgraph M2["M2: Complete MVP v0.1 (Routing, FinOps, Astryx UI, CLI, Ops)"]
        U16["TP-IMP-0016: /v1/responses, embeddings, models & blob_ref"]
        U17["TP-IMP-0017: Lossless /native/{dialect}/*"]
        U18["TP-IMP-0018: 5 Official Reference Adapters v0.1"]
        U19["TP-IMP-0019: Hierarchical Policy & Rate Limiter"]
        U20["TP-IMP-0020: 6-Step Capability Router & SM-04 Breaker"]
        U21["TP-IMP-0021: Two-Phase Micro-USD Budget Ledger"]
        U22["TP-IMP-0022: OTel Exporter & Privacy Redactor"]
        U23["TP-IMP-0023: JCS Audit Hash-Chain & 4-Level Kill-Switch"]
        U24["TP-IMP-0024: Admin API Part 1 (IAM, Keys, Sessions)"]
        U25["TP-IMP-0025: Admin API Part 2 (Plugins, Routes, FinOps, Ops)"]
        U26["TP-IMP-0026: Astryx Console Setup, Figtree Local & AppShell"]
        U27["TP-IMP-0027: UI SCR-01..02 (Setup Wizard <3m & Overview)"]
        U28["TP-IMP-0028: UI SCR-03..07 (Providers, Models & Plugins)"]
        U29["TP-IMP-0029: UI SCR-08, 09, 13 (Routes, Policies & Keys)"]
        U30["TP-IMP-0030: UI SCR-11, 12, 14, 16 (Traces, FinOps, Sec, Ops)"]
        U31["TP-IMP-0031: Unified tanpopo CLI & Embedded SPA Binary"]
        U32["TP-IMP-0032: Doctor, GitOps, Backup, Shredding & Preflight"]
        U33["TP-IMP-0033: Multi-OS Installers, OCI, SBOM & Cosign"]
    end

    subgraph M3["M3: v0.2 OAuth 2.1, Marketplace & MCP Hub"]
        U34["TP-IMP-0034: Authorized OAuth 2.1 Single-Flight Broker"]
        U35["TP-IMP-0035: Marketplace Index, OCI Puller & Bedrock/Azure"]
        U36["TP-IMP-0036: MCP Hub Core & JCS schema_hash Pinning"]
        U37["TP-IMP-0037: MCP HITL Destructive Approval & Webhooks"]
        U38["TP-IMP-0038: Route Explain, Playground & MCP/Approval UI"]
    end

    subgraph M4["M4: v0.3 HA Postgres/Valkey Cluster, SSO/KMS & A2A"]
        U39["TP-IMP-0039: PostgreSQL 16 FORCE RLS & SQLite->PG Migrate"]
        U40["TP-IMP-0040: Valkey {org_id} Lua, <5s Revoke & Helm HA"]
        U41["TP-IMP-0041: OIDC SSO, Four-Eyes, Cloud KMS & Cache"]
        U42["TP-IMP-0042: Tier-3 Sidecar mTLS & A2A Plane (SM-09)"]
    end

    subgraph M5["M5: v1.0 GA Hardening, Air-Gap, Docs & Release"]
        U43["TP-IMP-0043: k6 Load, SLO Verification & Chaos/DR Drills"]
        U44["TP-IMP-0044: 4h Fuzzing, N-1 Shim, Multi-Browser & Pen-Test"]
        U45["TP-IMP-0045: Offline Air-Gap Bundle & Multi-Org Console GA"]
        U46["TP-IMP-0046: Docs Portal, OpenAPI Sync & Repo Polish"]
        U47["TP-IMP-0047: Release Dossier & HUMAN OWNER SIGN-OFF GATE"]
    end

    GateOwner --> U01 --> U02 --> U03
    U02 & U03 --> U04
    U03 --> U05
    U05 --> U06 & U07
    U07 --> U08 --> U09
    U04 & U08 --> U10
    U07 & U09 --> U11
    U04 & U10 & U11 --> U12 & U13
    U06 & U12 & U13 --> U14
    U09 & U12 & U13 & U14 --> U15
    U15 --> U16 & U17 & U19
    U14 & U16 & U17 --> U18
    U16 & U19 --> U20 --> U21 --> U22 --> U23 --> U24
    U12 & U20 & U21 & U24 --> U25
    U05 & U24 --> U26
    U25 & U26 --> U27 & U28 & U29 & U30
    U14 & U25 & U27 & U28 & U29 & U30 --> U31 --> U32 --> U33
    U10 & U24 --> U34
    U11 & U18 & U34 --> U35
    U10 & U19 & U34 --> U36 --> U37 --> U38
    U07 & U21 & U23 --> U39
    U33 & U39 --> U40
    U34 & U39 --> U41
    U13 & U21 & U39 --> U42
    U40 & U42 --> U43
    U38 & U41 & U42 --> U44
    U35 & U40 & U41 --> U45
    U43 & U44 & U45 --> U46 --> U47
```

---

## 2. Critical Path, Parallel Worktree Lanes & Conditional First 5 Units

### 2.1. Longest Dependency Chain (`Critical Path`)
Any slip along the **22-hop sequential chain for `MVP v0.1`** (plus **4 hops to `v1.0 GA`**) below directly delays release readiness:
- **Critical path to `MVP v0.1`:** `TP-IMP-0001` → `0002` → `0003` → `0005` → `0007` → `0008` → `0009` → `0011` → `0013` → `0014` → `0015` → `0016` → `0020` *(via parallel `0019`)* → `0021` → `0022` → `0023` → `0024` → `0025` → `0027..0030` *(4-way parallel UI fan-out)* → `0031` → `0032` → `0033`.
- **Continuation critical path to `v1.0 GA`:** `0033` + `0039` → `0040` → `0043` *(parallel with `0044`, `0045`)* → `0046` → `0047`.

### 2.2. High-Throughput Parallel Fan-Out Windows via `git worktree`
When executing concurrently across multiple AI Coding Agents (one dedicated `git worktree` directory per Agent), **4 major conflict-free parallel fan-out windows** open up:
1. **Immediately after `TP-IMP-0003`:** Run `TP-IMP-0004` (`tools/spikes/*` with an isolated `tools/spikes/go.mod`) and `TP-IMP-0005` (root `go.mod`, `web/console`, `.github/workflows`, `.gitignore`) in parallel.
2. **Immediately after `TP-IMP-0011` + `0010`:** Run `TP-IMP-0012` (`internal/extension/declarative`) and `TP-IMP-0013` (`internal/extension/wasm`) in parallel.
3. **Immediately after `TP-IMP-0015`:** Run 3 lanes concurrently: Protocol & Official Adapters Lane (`0016`, `0017`, `0018`), Policy/Router/Ledger Lane (`0019` → `0023`), and (once `0024` lands) the Astryx AppShell Lane (`0026`).
4. **Immediately after `TP-IMP-0025` + `0026`:** Run **4 independent frontend screen lanes in parallel** (`0027`, `0028`, `0029`, `0030` — each owning disjoint page directories under `web/console/src/pages/*`), while backend lanes can simultaneously branch early for `0034` (`OAuth 2.1`) and `0039` (`PostgreSQL Driver`).

### 2.3. Conditional Execution Order of the First 5 Units (`Conditional First 5 Units`)

> **Live Queue Frontier Today (`TP-IMP-0010` & `TP-IMP-0011` `READY`):** **`TP-IMP-0001..0009` are `DONE` (`9 / 47`)**, **`TP-IMP-0010` & `TP-IMP-0011` (`2 / 47`) are `READY`** (`DEC-018`, `DEC-019`), and `TP-IMP-0012..0047` (`36 / 47`) remain `BLOCKED` pending upstream completion and review gates.
> **Original Gate G1–G4 Sequence Reference (Preserved for Auditability):**

1. **[`TP-IMP-0001`](./units/TP-IMP-0001.md)** *(Sequential #1)* — Spec Closure Batch A: Remediate `SM-01`..`09` state-machine dead ends, author missing `AC`s for `TP-FR-0701`, `0702`, `0901`, and align `v0.1` exit gates.
2. **[`TP-IMP-0002`](./units/TP-IMP-0002.md)** *(Sequential #2, immediately after `0001`)* — Spec Closure Batch B: Extract clean-compiling `runtime.proto`, multimodal `blob_ref` streaming envelope, all `51` `OpenAPI 3.1` endpoints, and canonical CLI / metric / error-code registries.
3. **[`TP-IMP-0003`](./units/TP-IMP-0003.md)** *(Sequential #3, immediately after `0002`)* — Spec Closure Batch C: Complete missing DDL tables (`26` canonical tables), `Project DEK` hierarchy, composite `(org_id, id)` keys + `FORCE RLS`, per-`org_id`/JCS `audit_events` hash-chaining, and Valkey `{org_id}` hash-tag key schema.
4. **[`TP-IMP-0004`](./units/TP-IMP-0004.md)** *(Parallel Lane A immediately after `0003`)* — Execute the 4 time-boxed spikes `SPIKE-01`..`04` and record the frozen `Wasm ABI` specification (`POC-01`).
5. **[`TP-IMP-0005`](./units/TP-IMP-0005.md)** *(Parallel Lane B immediately after `0003`)* — Bootstrap the Monorepo layout (`cmd/`, `internal/`, `web/console/`), reconcile `Apache-2.0` licensing (`ISS-002`), wire the `CONSTRAINTS.md` Floor Guard, and establish `Stage 0/1` CI.

---

## 3. Environmental Prerequisites, Hermetic Test Data & Build / Packaging Procedures (`Env, Test Data & Packaging Procedures`)

1. **Engineer Workstation & CI Runner Prerequisites (`Environmental Prerequisites`):**
   - **Go Toolchain:** `Go >= 1.24` (`module github.com/nguywnben/tanpopo`). **Mandatory build-flag rule:** Go's `-race` detector requires `CGO_ENABLED=1` (a C toolchain on the workstation/runner); therefore, **never combine `CGO_ENABLED=0` and `-race` in a single command** — always run two distinct invocations: (a) pure-Go verification without CGo: `CGO_ENABLED=0 go test ./...` (PowerShell: `$env:CGO_ENABLED='0'; go test ./...`), and (b) data-race verification: `go test -race ./...` (or invoke the cross-platform runner `go run ./scripts/ci/check.go`).
   - **Node.js & UI Toolchain:** `Node.js >= 22.13.0`, `npm` (always installed via `npm ci --ignore-scripts`), invoking Astryx CLI via `npx @astryxdesign/cli ...` (on Windows PowerShell use `cmd /c npx @astryxdesign/cli ...` per `ISS-006`).
   - **Contract & Security Tooling:** `buf` / `protoc`, `golangci-lint`, `govulncheck`, `osv-scanner`, `gitleaks`, `cosign`, `syft`/`cyclonedx-gomod`, and local Docker/Compose (required only for `PostgreSQL 16` + `Valkey 8` integration suites in `Stage 2 CI` and `TP-IMP-0039+`).
   - **Pre-Compiled Adversarial Wasm Fixtures (`Zero Host TinyGo Requirement for Unit Tests`):** Small `.wasm` guest fixtures for adversarial sandbox tests (`infinite_loop.wasm`, `oom_alloc.wasm`, `unreachable_trap.wasm`, `unapproved_wasi.wasm`, `echo_transform.wasm`) are deterministically assembled from auditable `.wat` text sources in `internal/testkit/wasmfixtures/` so `go test` runs out-of-the-box on Windows/macOS/Linux without requiring host `TinyGo`; `TinyGo` / `Rust wasm32` is only needed when compiling new SDK sample plugins.
2. **Hermetic Test Data & Zero External AI Cost (`Hermetic Test Data & Zero External AI Cost`):**
   - Every test package must set `TANPOPO_TEST_HERMETIC=1` inside `TestMain`, activating `testkit/netguard` to immediately fail any socket connection outside `127.0.0.0/8` / `::1`.
   - All test credentials use the canonical canary token `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c` so `CanaryScanner` can post-scan all logs, DB rows, OTel spans, and `support-bundle` archives.
   - Each test allocates an isolated `t.TempDir()` (for SQLite) or a deterministic `TANPOPO_TEST_SEED`-seeded `UUIDv7` `org_id` (for PostgreSQL/Valkey) to guarantee safe `t.Parallel()` execution.
3. **Single-Binary Build & Release Packaging Procedure (`Build & Package Procedure`):**
   - Step 1 *(Web Console Static Build)*: `cd web/console && npm ci --ignore-scripts && npm run build && npx @astryxdesign/cli doctor --built dist` (`0 fail`).
   - Step 2 *(Embed & Pure-Go Cross-Compile)*: Copy/stage static assets into `internal/webui/dist`, then run `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -mod=readonly -trimpath -ldflags="-s -w -X main.version=<git-tag>" -o dist/tanpopo ./cmd/tanpopo`.
   - Step 3 *(Provenance & Signing)*: Generate `SHA256SUMS`, emit `sbom.cdx.json`, sign keylessly via `cosign sign-blob`, and run offline binary smoke tests before submitting to the release gate.
