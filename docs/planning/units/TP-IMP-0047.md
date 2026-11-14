# `TP-IMP-0047` — Release Readiness Dossier (`GATE-REL-01`..`07`), Staged Canary Playbook & **Mandatory Product Owner Public-Release Sign-Off Gate**

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0047` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release` — **Final Gate `GATE-M5` / `GATE-REL-07`**) · `EP-09` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0046` + **Mandatory Human Product Owner Sign-Off**)* |
| **Size** | `S` (`docs/releases/v1.0.0-readiness-dossier.md`, `scripts/release/pre_publish_check.go`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** + **`MANDATORY_HUMAN_OWNER_SIGN_OFF`** *(Autonomous public release publishing by AI Agents is strictly prohibited!)* |
| **Technical Owner** | `Technical Program Manager, Principal Security Engineer & Product Owner` |

---

## 1. Intent
Assemble and cross-verify all quantitative evidence across the 6 technical release gates (`GATE-REL-01`..`06` per their authoritative definitions in [`release-readiness.md`](../../quality/release-readiness.md) Section 3), establish the staged rollout and rollback playbook (`RC -> Canary 5% -> 25% -> 50% -> 100%` paired with **`< 60-second`** instant rollback per `DR-04` and `roadmap.md`), and present the **Release Readiness Dossier** to the **Product Owner** at **`GATE-REL-07`**: **tagging an official release (`git tag v1.0.0`), pushing public container images, or publishing a GitHub Release requires explicit human authorization from the Product Owner and MUST NEVER be executed autonomously by an AI Agent!**

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `scripts/release/pre_publish_check.go` (cross-platform composite gate verifier checking all 6 technical gates `GATE-REL-01`..`06`).
  - `docs/releases/v1.0.0-readiness-dossier.md` (artifact evidence links mapped to each criterion in `GATE-REL-01`..`07`, transparent `Known Limitations` register, staged `Canary 5% -> 25% -> 50% -> 100%` rollout runbook, automated canary-abort triggers when internal error rate hits **`>= 0.05%`** / error-budget burn rate hits `>= 14.4x` / `ALT-AUDIT-TAMPER > 0` per `SLO-04` & `roadmap.md`, and the dedicated Product Owner sign-off block).
- **Out-of-Scope:** **Executing agents ARE STRICTLY FORBIDDEN from running `git push --tags`, `gh release create`, public `docker push`, or `npm publish` without direct, explicit instruction from the Product Owner after reviewing this dossier.**

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** All `54/54` `Must` (`P0`) requirements + `19` `Should/Could` requirements in the `v1.0` baseline, `GATE-REL-01`..`07`, `DR-04`, `DEC-001`..`008`.
- **Dependencies:** `TP-IMP-0046`.
- **Design Refs:** [`release-readiness.md`](../../quality/release-readiness.md) (`SPEC-QA-04`), [`roadmap.md`](../roadmap.md), [`milestone-map.md`](../milestone-map.md).

## 4. API, Wire & Data Contracts
- **Canonical 7-Gate Release Contract Aligned `100%` With `release-readiness.md` (`GATE-REL-01` .. `GATE-REL-07`):**
  1. **`GATE-REL-01` (`Functional, Contract & Floor Guard`):** `100%` of `P0` requirements passing, full `CT-01..32`, `AUTH-01..16`, and `EXT-ACCEPT-01` green, `0` `Sev-0/Sev-1` defects, and `0` `CONSTRAINTS.md` quality-bar regressions.
  2. **`GATE-REL-02` (`Compatibility Matrix`):** Green across all 5 OS/Arch targets (`Linux/macOS/Windows`), `SQLite <-> PostgreSQL 16` dual-DB behavioral parity, `Chromium/Firefox/WebKit` multi-browser E2E, and `N-1` backward compatibility (`tanpopo plugin migrate-check`).
  3. **`GATE-REL-03` (`Security, Fuzzing, Grey-Box Pen-Test & SLO Performance`):** `0` `Critical/High` CVEs, `FZ-01..06 >= 4 hours` fuzzing (`0` crashes), **completed 5-Surface Grey-Box Penetration Test (`v1.0` Grey-Box Pen-Test) with `0` open `Critical/High` findings**, and empirical `SLO-01..08` + `FMEA-01..08` verification on the `2 vCPU / 2 GiB RAM` reference floor.
  4. **`GATE-REL-04` (`License, Provenance & DEC-004 Legal Compliance`):** Uniform `Apache-2.0` licensing, `CycloneDX SBOM` + `SLSA` provenance + `Cosign` signatures, and strict `DEC-004` compliance (zero connectors or documentation repurposing personal consumer subscriptions).
  5. **`GATE-REL-05` (`Documentation, OpenAPI/CLI Sync, Astryx Doctor & Accessibility`):** `check_drift.go` `100%` green, `npx @astryxdesign/cli doctor --built dist` at `0 fail`, Light/Dark `axe-core` `WCAG 2.2 AA` audits at `0` critical/serious violations, and `UX-AC-01..12` satisfied.
  6. **`GATE-REL-06` (`Operational SRE, DR Drills, Air-Gap & Canary Rollback < 60s`):** `tanpopo doctor`, zero-leak `support-bundle`, timed `DR-01..04` recovery drills (`RTO < 15m`), `Air-Gap Bundle` (`CT-31`), verified `INC-01..07` alert links, and timed `< 60-second` `Canary` rollback drill (`DR-04`).
  7. **`GATE-REL-07` (`Explicit Human Product Owner Sign-Off`):** Explicit written authorization by the human Product Owner.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Running `go run ./scripts/release/pre_publish_check.go` against the `Release Candidate (RC)` exits with code `0`, reporting `GATE-REL-01..06 = PASS` alongside a complete JSON/Markdown manifest of artifact SHA-256 digests.
- **`AC-2` (`DR-04` Canary Rollback Drill `< 60s`):** A timed rollback drill from `v1.0.0-rc1` back to the preceding stable build (verifying *Expand-Only* database schema backward compatibility and uninterrupted SSE streaming) completes in **`< 60 seconds`**.
- **`NEG-1` (`Hard Block on Unapproved Public Publish`):** Whenever explicit human Product Owner authorization is absent, every public-publish script aborts immediately on line 1 (`403 human_release_approval_required`).

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 go run ./scripts/release/pre_publish_check.go` *(PowerShell: `$env:CGO_ENABLED='0'; go run ./scripts/release/pre_publish_check.go`)*
- Race detector (`-race`): `go test -v -race ./scripts/release/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Production release signing keys (`Production Release Signing Key` / GitHub OIDC Environment gated by `Required Reviewers = Product Owner`) are strictly isolated from standard CI test keys — automated coding agents have zero access to production release credentials.
- **Rollback:** The `< 60-second` rollback runbook (`DR-04`) is embedded directly in Section 4 of `v1.0.0-readiness-dossier.md`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** All 46 preceding units (`TP-IMP-0001` .. `TP-IMP-0046`) are `100%` complete and have passed independent audit.
- **DoD:** `docs/releases/v1.0.0-readiness-dossier.md` is complete, `scripts/release/pre_publish_check.go` reports green across `GATE-REL-01..06`, and execution **halts awaiting explicit Product Owner public-release sign-off at `GATE-REL-07`**.
