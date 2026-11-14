# `TP-IMP-0044` — Comprehensive Security Audit, `>= 4h` Fuzzing Campaign (`FZ-01..06`), `N-1` Compatibility Shim (`migrate-check`), Multi-Browser/OS E2E & Pen-Test Remediation

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0044` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0038`, `TP-IMP-0041`, `TP-IMP-0042` — `GAP-01`)* |
| **Size** | `L` (`tests/security/*`, `tests/e2e/*`, `internal/extension/compat/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Adversarial security verification `CT-01..36` + `AUTH-01..16`, `>= 4h` continuous fuzzing & `1.x` backward-compatibility commitment)* |
| **Technical Owner** | `Principal Security Auditor & QA Lead` |

---

## 1. Intent
Execute the final quality hardening and security audit campaign ahead of freezing the `v1.0 GA` contract in 100% alignment with [`release-readiness.md`](../../quality/release-readiness.md) (`GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03`, `GATE-REL-05`):
1. **Full `52/52` Adversarial Matrix (`CT-01`..`36` + `AUTH-01`..`16`), Continuous `>= 4-Hour` Fuzzing Campaign (`FZ-01`..`06`) & Mandatory `5-Surface Grey-Box Penetration Test` (`GATE-REL-03`):** Conduct adversarial grey-box penetration testing across all 5 critical attack surfaces (Wasm/Sidecar Sandbox escape, `SSRF / DNS Rebinding` & `BlindBroker` credential exfiltration, multi-tenant `RLS / AEAD AAD` privilege escalation, `MCP Rug-Pull / OAuth RFC 8707` spoofing, and FinOps ledger / audit hash-chain tampering) and remediate `100%` of `Critical / High` findings.
2. **`v1.0` Contract Freeze & `N-1` Backward-Compatibility Checker (`tanpopo plugin migrate-check` — `CT-24`, `GATE-REL-02`):** Ensure `.tpkg` artifacts authored against the final `0.x` contract execute seamlessly on `v1.0` and establish the `>= 12-month` stability guarantee for the `1.x` line.
3. **Cross-Browser (`Chromium`, `Firefox`, `WebKit`), Multi-Viewport (`1280`, `768`, `375`), `WCAG 2.2 AA` (`axe-core` Light + Dark), Lighthouse CI (`UX-AC-08`) & Multi-OS (`Linux`, `macOS`, `Windows`) End-to-End Verification (`GATE-REL-02`, `GATE-REL-05`).**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `tests/security/adversarial_matrix_test.go` & `docs/security/pentest-v1.0-remediation-report.md` (converging verification of all 36 `CT-*` + 16 `AUTH-*` cases alongside the 5-Surface Grey-Box Penetration Test remediation report).
  - `scripts/qa/run-fuzz-campaign.go` & regression seed corpora `testdata/fuzz/*` covering all 6 fuzz targets `FZ-01`..`06`.
  - `internal/extension/compat/v1_shim.go` & `internal/cli/plugin/migrate_check.go` (`tanpopo plugin migrate-check` — `CT-24`).
  - `web/console/e2e/*.spec.ts` (Playwright executing all 8 user journeys `UJ-01`..`08` across 3 browser engines + Light/Dark `axe-core` scans `UX-AC-05`, non-color-only state verification `UX-AC-06` & Lighthouse CI `UX-AC-08` across all 16 screens `SCR-01`..`16`).
- **Out-of-Scope:** End-user documentation portal publication (covered in `TP-IMP-0046`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0401`..`0404`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-0801`, `TP-NFR-0901`, `TP-NFR-0902`, `CT-01`..`36`, `AUTH-01`..`16`, `FZ-01`..`06`, `UX-AC-05`, `UX-AC-06`, `UX-AC-08`, `GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03`, `GATE-REL-05`.
- **Dependencies:** `TP-IMP-0038`, `TP-IMP-0041`, `TP-IMP-0042`.
- **Design Refs:** [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`SPEC-QA-01`), [`test-strategy.md`](../../quality/test-strategy.md) (`SPEC-QA-02`), [`release-readiness.md`](../../quality/release-readiness.md) (`SPEC-QA-04`).

## 4. API, Wire & Data Contracts
- **Strict Zero `Sev-0 / Sev-1` Release Blocker Policy (`Zero Sev-0 / Sev-1 Policy` — `SPEC-QA-04`):** Any defect causing secret exposure, Wasm/SSRF boundary escape, cross-tenant data read, FinOps ledger drift, or `CONSTRAINTS.md` quality-floor regression automatically blocks GA release progression.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`GATE-REL-01 & 03` Adversarial Matrix, Fuzzing `>= 4h` & 5-Surface Grey-Box Pen-Test):** All 52 `CT/AUTH` adversarial cases pass, `gosec` + `govulncheck` + `npm audit --omit=dev` report `0 Critical / 0 High`, `FZ-01..06` completes `>= 4 hours` with `0` crashes, and the 5-Surface Grey-Box Penetration Test dossier closes with `0` open `Critical / High` findings.
- **`AC-2` (`GATE-REL-02 & 05` Multi-Browser, Accessibility & Core Web Vitals):** All `UJ-01`..`08` journeys pass on `Chromium`, `Firefox`, and `WebKit` (`0 flaky retries`), Light/Dark `axe-core` audits report `0` `critical`/`serious` violations across all 16 screens (`UX-AC-05`), Lighthouse CI achieves `LCP <= 2.0s`, `INP <= 150ms`, `CLS <= 0.05` (`UX-AC-08`), and `npx @astryxdesign/cli doctor --built dist` passes with `0 fail`.
- **`AC-3` (`CT-24` & `GATE-REL-02` Contract N-1 Compatibility):** `.tpkg` artifacts built during `M1/M2` pass `tanpopo plugin migrate-check` and execute cleanly on the `v1.0 RC` runtime.
- **`NEG-1`:** Automated `CONSTRAINTS.md` enforcement (`go run ./scripts/ci/check.go --all`) confirms `0` lint/type suppressions or lowered coverage thresholds.

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./tests/security/... ./internal/extension/compat/... ./internal/cli/plugin/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./tests/security/... ./internal/extension/compat/... ./internal/cli/plugin/...`)*
- Race detector (`-race`): `go test -v -race ./tests/security/...`
- `go run ./scripts/qa/run-fuzz-campaign.go --duration=4h`
- `cd web/console && npx playwright test --project=chromium --project=firefox --project=webkit && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Every edge case or defect uncovered by fuzzing or the 5-Surface Grey-Box Penetration Test MUST be committed as a permanent regression seed inside `testdata/` prior to closing this unit.
- **Rollback:** Adds compatibility shims, regression suites, and security hardening fixes only.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0038`, `TP-IMP-0041`, and `TP-IMP-0042` are complete.
- **DoD:** `100%` of `GATE-REL-01`, `GATE-REL-02`, `GATE-REL-03`, and `GATE-REL-05` criteria satisfied, with joint `REQUIRES_STRONG_REVIEW` sign-off from `security-auditor` and `test-engineer`.
