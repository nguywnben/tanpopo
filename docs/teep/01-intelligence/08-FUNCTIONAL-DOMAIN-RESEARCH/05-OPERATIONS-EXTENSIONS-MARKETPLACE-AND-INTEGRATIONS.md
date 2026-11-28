# 08.05 — Functional Domain Research: Infrastructure Operations, Extensions, Marketplace, Integrations & Chaos/Simulation (`DOM-16` .. `DOM-19`, `DOM-24`)

- **Document ID:** `TEEP-01-DOC-08-05`
- **Domains Covered:** `DOM-16` (Infrastructure Operations), `DOM-17` (Extension Platform & SDK), `DOM-18` (Marketplace & Community Ecosystem), `DOM-19` (Notifications & Integrations), `DOM-24` (Resilience, Chaos Engineering & Pre-Flight Policy Simulation)
- **Candidate Capabilities:** `CAP-104`..`CAP-129`, `CAP-152`..`CAP-156`

---

## 1. `DOM-16` — Infrastructure Operations (`CAP-104` .. `CAP-110`)

- **RC9 Audit Gaps (`[VERIFIED_FACT]`):**
  - `DEF-072` & `PG-007`: `serve.go:547` deletes `cluster_nodes` on shutdown, `ops_lifecycle.go:145` fabricates a healthy `"primary"` node if empty, `0` live Data-Plane nodes still reports `Ready / OK`, `loaded_kek_ids_json` is dropped in `SELECT`, and Valkey outage/fallback is hidden in UI.
  - `5` CLI ops (`vault rotate-kek`, `backup create`, `upgrade preflight`, `tenant shred`, `admin reset-local-owner`) and `bundle import --db` hardcode `sqlite.Open` (failing on PostgreSQL HA).
  - `ARCH-OBS-01`: `7` missing PostgreSQL indexes + `18` `O(N_orgs)` RLS loops.
  - `POST /admin/v1/diagnostics/{smoke-test,support-bundle}` and `PUT /admin/v1/settings/retention` return `404` in `server.go`.
- **Target Operational Capabilities (`CAP-104` .. `CAP-110`):**
  1. **True Cluster Topology, Valkey/OTLP Health & DP Quorum Monitoring (`CAP-104`, `DEF-072`, `PG-007`):** Retain `STOPPED`/`UNREACHABLE` node tombstones, surface `loaded_kek_ids_json`, `valkey_status` (`connected` / `degraded_fallback`), and OTLP ring-buffer drop counters, and degrade cluster status when live Data-Plane nodes fall below configured quorum.
  2. **PostgreSQL HA Parity Across All Break-Glass CLI Commands & Verified PITR Runbooks (`CAP-105`, `RR-015`):** Add `--pg-dsn` support to `vault rotate-kek`, `upgrade preflight`, `tenant shred`, `admin reset-local-owner`, and `bundle import`, plus `tanpopo backup verify-pg` validating `pg_dump`/PITR + Vault envelope integrity.
  3. **`ARCH-OBS-01` PostgreSQL Index & `O(k_affected)` Scalability Pack (`CAP-106`):** Land all 7 missing secondary/partial indexes + narrow `SECURITY DEFINER` affected-org lookup functions while keeping `100%` tenant `FORCE RLS`.
  4. **Production Helm Chart, Kubernetes Operator & Distroless FIPS-Optional Packaging (`CAP-107`):** Hardened multi-arch OCI images, Helm chart with split CP/DP deployments, PDBs, `preStop` SSE drain hooks, and NetworkPolicies.
  5. **Online Expand-Only Schema Migrations & Zero-Downtime Skew Preflight (`CAP-108`):** Automated SQLite ↔ PostgreSQL column-width/type parity gate in `db/spec-check` (preventing `DEF-086` regressions) and N-1/N rolling compatibility checks.
  6. **Wired HTTP Diagnostics (`smoke-test`, `support-bundle`, `openapi.json`) (`CAP-109`):** Implement the 3 missing diagnostic/OpenAPI handlers in `admin/http/server.go` and expose them in Console DevTools (`SCR-15`).
  7. **Valkey Shard Failover & `DR-02` Checkpoint Rebuild Automation (`CAP-110`, `RR-010`, `COND-PO-03`):** Continuous automated verification of `valkey_hold_checkpoints` reconciliation during Valkey primary failover.

---

## 2. `DOM-17` — Extension Platform & SDK (`CAP-111` .. `CAP-117`) and `DOM-18` — Marketplace & Community Ecosystem (`CAP-118` .. `CAP-124`)

*(Cross-referenced in full detail in [`10-EXTENSION-AND-MARKETPLACE-RESEARCH.md`](../10-EXTENSION-AND-MARKETPLACE-RESEARCH.md) and [`12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md`](../12-ARCHITECTURAL-DEBT-AND-RESIDUAL-RISKS.md) §3.4.)*

### 2.1 `DOM-17` — Extension Platform & SDK (`CAP-111` .. `CAP-117`)
- **`CAP-111` (`KEEP`):** Zero-ambient-authority Tier-2 `wazero` sandbox (`16 MiB`, `50ms`, DRR fair queuing, `FaultGuard` auto-quarantine).
- **`CAP-112` (`IMPROVE`):** Additive `runtime.proto` `v1.1` host wiring across all **11 `.tpkg` extension categories** (`PROVIDER_ADAPTER`, `AUTH_PROVIDER`, `ROUTING_STRATEGY`, `GUARDRAIL_PROCESSOR`, `CONTEXT_TRANSFORMER`, `CACHE_STRATEGY`, `TELEMETRY_EXPORTER`, `NOTIFICATION_SINK`, `CATALOG_FEED`, `TOOL_CONNECTOR`/`AGENT_MIDDLEWARE`, `SOLUTION_PACK`).
- **`CAP-113` (`IMPROVE`):** Managed Tier-3 Sidecar Supervisor (optional lifecycle spawning, health probes, Linux cgroup/`CLONE_NEWNET` + macOS/Windows firewall confinement disclosure, `0600` UDS / `SPIFFE mTLS`).
- **`CAP-114` (`ADD`):** Official Polyglot Guest SDKs (`tanpopo-sdk-go`, `tanpopo-sdk-rust`, `tanpopo-sdk-ts`) + Tier-3 template + optional build-time WIT lowering.
- **`CAP-115` (`ADD`):** Declarative Localized Extension Config UI Schemas (JSON Schema + `uiSchema` + 15-locale metadata maps; `0` third-party Console JS).
- **`CAP-116` (`IMPROVE`):** Offline Contract, Fuzz & Resource-Cap Harness (`tanpopo plugin test --fuzz`).
- **`CAP-117` (`REPLACE` — Resolves `RR-001` & `DEF-043`):** Unified `internal/extension/tpkg` deterministic cross-OS packer (`linux`/`macos`/`windows` SHA-256 parity, CRLF guard, Merkle tree digest, containerized `build-wasm.sh` including `aws-bedrock`).

### 2.2 `DOM-18` — Marketplace & Community Ecosystem (`CAP-118` .. `CAP-124`)
- **`CAP-118` (`ADD`):** Keyless OIDC Sigstore (`Fulcio` + `Rekor`) Publisher Onboarding & DNS Namespace Verification (`0` static publishing PATs).
- **`CAP-119` (`IMPROVE` — Resolves `DEF-036`, `DEF-044`, `DEF-060`):** Staged $\Delta P$ Consent, Environment Canary Rings (`dev`/`staging`/`5% prod`), Graceful Non-Incident Disable (`INSTALLED_DISABLED`), Browser `.tpkg` Upload / `tanpopo.lock` Download, and `< 5s` (`HYPOTHESIS / UNVERIFIED`) Atomic Rollback.
- **`CAP-120` (`IMPROVE` — Resolves RC9 Setup Friction):** 5-Tier Trust Governance (`OFFICIAL`, `VERIFIED_PARTNER`, `COMMUNITY_REVIEWED`, `PRIVATE_ENTERPRISE`, `UNVERIFIED`) + Console `SCR-07` & CLI `org_extension_policies` controls.
- **`CAP-121` (`ADD`):** 7-Stage Automated Marketplace Moderation & `DEC-004` ToS Firewall CI.
- **`CAP-122` (`ADD`):** TUF (`go-tuf/v2`) Anti-Freeze / Anti-Downgrade Metadata & Monotonic `revocations.json` Kill-Switch.
- **`CAP-123` (`IMPROVE`):** OCI `1.1` Artifact Distribution (`oras-go/v2` + SBOM/Sigstore Referrers) for Native Harbor/Artifactory/ECR Mirroring.
- **`CAP-124` (`IMPROVE`):** PostgreSQL-Compatible Air-Gap `.tpbundle` Sync (`tanpopo bundle export|verify|import --pg-dsn`).

---

## 3. `DOM-19` — Notifications & Integrations (`CAP-125` .. `CAP-129`)

1. **Async Outbox Hardening & Tenant Isolation (`CAP-125` — `IMPROVE`):** Decouple global extension quarantine webhook fan-out from synchronous containment TXs (`ARCH-OBS-01` Q4; fix duplicate `TX1+TX2` outbox inserts in `serve.go:1046`), add missing `(org_id) WHERE dispatched_at_ms IS NULL` PG index, and separate `/settings/webhooks` from `/settings/residency-retention` so `ORG_OWNER`s can manage their own org webhooks ([`04-CURRENT-TANPOPO-PRODUCT-AUDIT.md`](../04-CURRENT-TANPOPO-PRODUCT-AUDIT.md) §3.5)!
2. **Full Webhook Lifecycle, Delivery Inspector, Replay & Test-Ping (`CAP-126`):** Edit `target_url`/`events`, inspect `webhook_deliveries` & Dead-Letter Queue, replay failed events, and expand event taxonomy (`model.deprecation_warning`, `budget.burn_anomaly`, `mcp.schema_drift`, `scim.deprovisioned`, `policy.what_if_completed`).
3. **Native ChatOps & Incident Connectors via `NOTIFICATION_SINK` (`CAP-127`):** Slack Block Kit, Microsoft Teams Adaptive Cards, PagerDuty Events v2, Opsgenie, Jira, and ServiceNow — with authenticated OIDC + 5-tuple JCS callback verification for Slack/Teams `SM-06` HITL approvals.
4. **SIEM / SOAR & FinOps Ecosystem Connectors (`CAP-128`):** Splunk HEC, Microsoft Sentinel, CrowdStrike Falcon Next-Gen SIEM, Datadog, Vantage, and Snowflake (`FOCUS 1.1`).
5. **Developer IDE & Coding-Agent Profiles (`CAP-129`):** Verified 1-click connection profiles & OAuth device-code Virtual Key binding for **Claude Code**, **Cursor**, **Windsurf**, **Cline**, **Zed**, and **GitHub Actions** (strictly backed by governed enterprise keys/deployments in compliance with `DEC-004`).

---

## 4. `DOM-24` — Resilience, Chaos Engineering & Pre-Flight Policy Simulation *(Discovered Domain — `CAP-152` .. `CAP-156`)*

1. **Metadata-Only Historical Traffic Replay ("What-If Engine") (`CAP-152`, `INN-01`):** Replays `7–30d` of `request_traces_meta` + `usage_settlements` against candidate `ConfigSnapshot` diffs (`0` prompt bytes stored), outputting exact blocked-request counts and `micro-USD` deltas in Console & GitOps PR comments.
2. **8-Layer Live Topology & Pre-Mutation SPOF Cut-Set Analyzer (`CAP-153`, `INN-03`):** Interactive topology graph + pre-action `AlertDialog` impact simulation before disabling connections, rotating secrets, or quarantining plugins.
3. **Built-In Hermetic Loopback Chaos & DORA Drill Studio (`CAP-154`, `INN-05`):** `OPERATOR`-gated loopback fault runner (`429`, mid-SSE `RST`, budget race, MCP rug-pull, Wasm OOM) emitting JCS-signed `dora-drill-receipt.json` attestations (`0` paid API calls).
4. **Continuous Multi-Hour Fuzzing & Cross-Engine Differential CI Gate (`CAP-155`, `COND-PO-01`):** Scheduled `>= 4h` Go fuzzing across SSE/EventStream/JSON-RPC/manifest/Decimal parsers + mandatory dual-backend (`SQLite` + `PG16 FORCE RLS`) E2E parity including unsigned-binary onboarding.
5. **Automated Incident Forensic Timeline & Blast-Radius Exporter (`CAP-156`):** Synthesizes affected keys, connections, traces, and JCS audit proofs for any security quarantine or credential compromise window.
