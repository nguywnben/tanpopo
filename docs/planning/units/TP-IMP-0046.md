# `TP-IMP-0046` — 4-Journey Documentation Portal, Automated `OpenAPI/CLI` Drift Gate, `CHANGELOG.md` & `Apache-2.0` Open-Source Repository Polish

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0046` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0043`, `TP-IMP-0044`, `TP-IMP-0045` — `GAP-01`)* |
| **Size** | `M` (`README.md`, `SECURITY.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `docs/guides/*`, `scripts/docs/check-drift.sh`) |
| **Review Tier** | `STANDARD_REVIEW` *(Ensuring `100%` of documented example commands execute cleanly & enforcing `DEC-004` legal compliance)* |
| **Technical Owner** | `Developer Relations & Documentation Lead` |

---

## 1. Intent
Complete the end-user, extension-author, and SRE operator documentation suite for the `Apache-2.0` open-source release (`TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`, `GATE-REL-04`, `GATE-REL-05`), paired with an **automated documentation-to-code drift gate (`Zero Doc-to-Code Drift`)** ensuring every CLI invocation, YAML configuration snippet, and OpenAPI schema example in the documentation is continuously verified in CI.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - Root `README.md` (concise Tanpopo overview, `< 3-minute` quickstart, 4-plane architecture summary, and explicit `Official Commercial API / Self-Hosted Infra Only — DEC-004` compliance table), `SECURITY.md` (coordinated vulnerability disclosure policy), `CONTRIBUTING.md`, `CHANGELOG.md`, and `NOTICE`.
  - `docs/guides/quickstart.md` (Journey 1: Application Developer & `< 3m` Quickstart), `docs/guides/plugin-authoring-sdk.md` (Journey 2: Authoring & cassette-testing a Provider Adapter in `< 30 minutes`), `docs/guides/production-ha-and-airgap.md` (Journey 3: SRE Operations, Backups, `DR-01..04` recovery drills, all 7 incident runbooks `INC-01..07`, Clustered HA & Air-Gap), and `docs/guides/security-and-mcp-governance.md` (Journey 4: Credential Vault, OAuth 2.1, MCP & A2A Governance).
  - `scripts/docs/check_drift.go` (cross-platform verifier comparing recursive `tanpopo --help` output against CLI reference docs, validating `openapi/*.yaml`, verifying `deploy/monitoring/alerts.yaml` runbook links across all `INC-01`..`07`, asserting `0` broken internal Markdown links, and executing `Quickstart` snippets against a mock server).
- **Out-of-Scope:** Executing a public release publish (restricted exclusively to `TP-IMP-0047` following explicit Product Owner sign-off).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`, `DEC-001`..`005`, `INC-01`..`07`, `GATE-REL-04`, `GATE-REL-05`, `GATE-REL-06`.
- **Dependencies:** `TP-IMP-0043`, `TP-IMP-0044`, `TP-IMP-0045`.
- **Design Refs:** [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`release-readiness.md`](../../quality/release-readiness.md) (`GATE-REL-04`, `GATE-REL-05`, `GATE-REL-06`).

## 4. API, Wire & Data Contracts
- **Benchmark Honesty & Legal Compliance Invariants:**
  1. Every latency or throughput figure cited in `README.md` and `docs/guides/*` MUST link directly to empirical measurements in `docs/quality/benchmark-evidence.md` (`TP-IMP-0043`) alongside exact reference hardware specifications — **unsubstantiated marketing claims are strictly prohibited**.
  2. Zero guides, examples, or hints may repurpose personal consumer subscriptions (`Plus/Pro/Max/Codex/Antigravity...`) into shared API endpoints (`DEC-004`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`GATE-REL-05 & 06` Executable Docs, Runbook Links & Zero Link Rot):** Running `go run ./scripts/docs/check_drift.go` confirms `0` broken links repository-wide, `100%` alert-to-runbook mapping across `INC-01`..`07`, `0` CLI flag discrepancies between `tanpopo --help` and documentation, and automated `Quickstart` execution completing in `< 60 seconds`.
- **`AC-2` (`GATE-REL-04` Clean Apache-2.0 Licensing):** Consistency checks across `LICENSE`, `NOTICE`, `go.mod`, `web/console/package.json`, and full Go/npm dependency license scans confirm `100%` `Apache-2.0` compatibility.
- **`NEG-1`:** Renaming a CLI flag in code without updating its documentation reference causes `check_drift.go` to fail CI immediately.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 go run ./scripts/docs/check_drift.go` *(PowerShell: `$env:CGO_ENABLED='0'; go run ./scripts/docs/check_drift.go`)*
- Race detector (`-race`): `go test -v -race ./scripts/docs/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Automated secret scanning across all example files in `docs/guides/*` ensures only canonical placeholder tokens (`vk_tp_test_example...`, `sk-example-placeholder`) appear, never live credentials.
- **Rollback:** Documentation files and static verification scripts support atomic single-commit updates.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0043`, `TP-IMP-0044`, and `TP-IMP-0045` are complete.
- **DoD:** `GATE-REL-04` and `GATE-REL-05` fully satisfied, ready to assemble the final release readiness dossier in `TP-IMP-0047`.
