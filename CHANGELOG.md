# Changelog

All notable changes to **Tanpopo** are documented in this file. The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.

> Public versioned git tags and GitHub Releases remain gated behind human Product Owner sign-off (`GATE-REL-07`). Dates below reflect landed trunk milestones on `main`.

---

## [Unreleased — `v1.0.0` GA Hardening (`M5`)]

### Added
- **Documentation Portal, Open-Source Polish & Drift Automation (`TP-IMP-0046`):**
  - Rewrote root [`README.md`](./README.md) in English with clear positioning, quickstart, honest `COND-PO-01` benchmarks/limits, and `DEC-004` compliance matrix.
  - Added 4 technical journeys under [`docs/guides/`](./docs/guides/README.md) (`quickstart.md`, `plugin-authoring-sdk.md`, `production-ha-and-airgap.md`, `security-and-mcp-governance.md`).
  - Added [`SECURITY.md`](./SECURITY.md), [`CONTRIBUTING.md`](./CONTRIBUTING.md), [`NOTICE`](./NOTICE), and `CHANGELOG.md`.
  - Added [`scripts/docs/check_drift.go`](./scripts/docs/check_drift.go) automating CLI `--help` sync, `openapi/*.yaml` validation, `deploy/monitoring/alerts.yaml` `INC-01..07` runbook verification, internal link checks, secret hygiene, and executable quickstart validation.
- **Air-Gap Offline Bundle Toolchain & Multi-Org GA Console (`TP-IMP-0045` — PR [#37](https://github.com/nguywnben/tanpopo/pull/37), `e7189be`):**
  - Implemented `tanpopo bundle export | verify | import` (`.tar.zst` + domain-separated Ed25519 signatures, pinned trust root, `os.OpenRoot` confinement, `LastIndexSerial`/`LastRevocationSerial` anti-rollback, and two-phase LIFO rollback `prepareDirSwap` enforcing `exit 22` on tamper).
  - Added `--offline` and `--multi-org` flags on `tanpopo serve`, multi-org `X-Tanpopo-Org-ID` switching across PostgreSQL 16 `FORCE RLS`, global `EXTENSION` kill-switch isolation, `OrgSwitcher.tsx`, `/ops/organizations` (`OrganizationsAdminPage.tsx`), client state purge, and 15-locale CLI/Console updates verified inside `docker run --network=none` + Chromium Playwright.
- **Security Audit, Fuzzing, 5-Surface Pen-Test & Cross-Browser Audit (`TP-IMP-0044` — PR [#36](https://github.com/nguywnben/tanpopo/pull/36), `1270250`):**
  - Remediated `G123` sidecar mTLS session tickets, `G122` `os.OpenRoot` extension reads, `internal/webui/embed.go` `path.Clean` reserved-prefix guard, `/admin/v1/*` `Cache-Control: no-store`, and `doctor` `os.Lstat` lockfile check.
  - Added `tanpopo plugin migrate-check` (`CT-24`, 15 CLI locales), 5-surface grey-box adversarial suite [`tests/security/pentest_5surfaces_test.go`](./tests/security/pentest_5surfaces_test.go), 7-target bounded fuzzing (`0` crashes), and multi-browser Playwright + Chrome CDP CWV audit recorded in [`docs/security/pentest-v1.0-remediation-report.md`](./docs/security/pentest-v1.0-remediation-report.md).
- **Load Testing, Protective Shedding & Automated Chaos/DR Drills (`TP-IMP-0043` — PR [#35](https://github.com/nguywnben/tanpopo/pull/35), `44689bb`):**
  - Added `MaxNodeConcurrentStreams` + `85%` heap `503 gateway_overloaded` shedding, `FakeLLM` programmable `chunk_delay_ms`, automated `tests/chaos` (`FMEA-01..08`, `DR-01..04`, `OPS-AC-06`), `tests/perf/harness` & `k6` scripts, and [`docs/quality/benchmark-evidence.md`](./docs/quality/benchmark-evidence.md).

---

## Internal Milestones Landed on `main` (`M0` – `M4`)

### `M4` — Enterprise Scale-Out, Official Adapters, Native/Responses, MCP Governance & A2A (`TP-IMP-0031`..`0042`)
- **`TP-IMP-0042` (`DEC-091`, PR [#32](https://github.com/nguywnben/tanpopo/pull/32), `25e362f`):** Tier-3 Isolated Sidecars (`ConnectRPC` TLS 1.3 SPIFFE mTLS / `0600` UDS), A2A plane (`/.well-known/agent.json`, `/a2a/*`, `SM-09`, `hop_count <= 5`, parent-child sub-budget carving), and Console A2A governance tab.
- **`TP-IMP-0041` (`DEC-090`, PR [#31](https://github.com/nguywnben/tanpopo/pull/31), `e55f246`):** Enterprise OIDC SSO (`PKCE S256`/`JWKS`), Four-Eyes dual control, AWS/GCP/HashiCorp Vault Transit KMS envelope keys, workload identity (`IRSA/STS`), and opt-in `AES-256-GCM` response cache.
- **`TP-IMP-0040` (`DEC-089`, PR [#30](https://github.com/nguywnben/tanpopo/pull/30), `b590447`):** 3-shard Valkey Cluster (`{org_id}` hash tags), `< 5s` revocation sync + kill-switch barriers, graceful `/readyz` `503` drain, and HA Helm chart (`deploy/helm/tanpopo`).
- **`TP-IMP-0039` (`DEC-088`, PR [#29](https://github.com/nguywnben/tanpopo/pull/29), `61c310b`):** PostgreSQL 16+ `FORCE RLS`, monthly `RANGE` partitions (`PART-*`), and `tanpopo db migrate-engine` (`SQLite -> PostgreSQL 16+`).
- **`TP-IMP-0031`..`0038` (`DEC-060`..`085`):** 8 official adapters (`OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible`, `Ollama`, `AWS Bedrock`, `Azure OpenAI`, `Cohere Rerank`), declarative `tanpopo.yaml` reconciler, `/native/*` passthrough, `/v1/responses` + `/v1/rerank`, `MCP 2025-03-26` Governance Hub (`SM-06`, JCS pinning, HITL), encrypted `.tpop.enc` backups, `upgrade preflight`, and `support-bundle`.

### `M3` — Web Console v2 Astryx Neutral & 15-Language i18n (`TP-IMP-0026`..`0030`, `PR #28` & `PR #34`)
- Built offline-bundled React 19 + TypeScript + `@astryxdesign/core` & `@astryxdesign/theme-neutral` Web Console with local `Figtree` fonts, WCAG 2.2 AA (`axe-core`) verification, `15`-language catalogs across Console/CLI/Admin API, and responsive button/toolbar refinement (`PR #34`, `7224740`).

### `M0`–`M2` — Core Gateway Engine & Foundational Contracts (`TP-IMP-0001`..`0025`)
- Locked Protobuf v1, OpenAPI 3.1, SQLite/PostgreSQL DDL specs, Pure-Go CI Floor Guard (`check.go`), SQLite WAL storage, 3-tier Envelope Vault, IAM/RBAC/Virtual Keys, `SSRFGuard` & `BlindHostCredentialBroker`, `.tpkg` installer + `tanpopo.lock`, Tier-1 Declarative & Tier-2 `wazero` engines, `tanpopo plugin` SDK CLI, 6-step Route Resolver & `SM-04` circuit breaker, Two-Phase integer `micro-USD` ledger, OTel & `INC-01..07` alerts, JCS audit chain, and full `/admin/v1/*` Control Plane API.
