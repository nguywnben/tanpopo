# `TP-IMP-0047` Execution & Verification Report — Release Readiness Dossier (`GATE-REL-01..07`), Fail-Closed `pre_publish_check.go` & `< 60s` Live Canary Rollback (`DR-04`)

- **Unit ID:** `TP-IMP-0047` (`M5` / `EP-09` · **`REQUIRES_STRONG_REVIEW`**)
- **Model:** **Gemini 4 Hydrogen**
- **Decision:** `DEC-096` *(delegated under `DEC-092` M5 sequential authorization — stopping strictly at `GATE-REL-07` with zero tags/releases)*
- **Branch / PR:** `develop` → `main` *(Final `M5` `develop -> main` PR, `main` SHA & post-merge CI pending merge)*
- **Verdict:** **`READY_FOR_GATE_REL_07`** *(All engineering deliverables, independent strong reviews, and `< 60s` canary drill verified; **`GATE-REL-07` human Product Owner launch sign-off remains `BLOCKED` fail-closed**)*

---

## 1. Delivered Scope

1. **Pre-Publish Release Gate & `NEG-1` Human Approval Guard ([`scripts/release/pre_publish_check.go`](../../scripts/release/pre_publish_check.go), [`pre_publish_check_test.go`](../../scripts/release/pre_publish_check_test.go)):**
   - Evaluates **`GATE-REL-01`..`06`** aligned 1:1 with [`release-readiness.md`](../quality/release-readiness.md) §3 (`SPEC-QA-04`), confines repository file reads via `os.OpenRoot`, verifies Dual-DB migration sets, `Apache-2.0` (`LICENSE` + `web/console/package.json`), `--skip=publish` dry-run workflows, `15` CLI + `15` UI locales, embedded Web Console, `ALT-GATEWAY-BURN`, and writes a deterministic `23`-entry `SHA256SUMS` manifest cleanly creating missing parent directories via `writeManifestFileSafely` (`os.Lstat` non-symlink guarded).
   - Enforces fail-closed **`NEG-1` (`HTTP 403 human_release_approval_required`, exit status `4`)** whenever Ed25519 PO approval is missing/invalid, `--commit-sha` (`40-hex`) is missing or mismatched in `publish` mode, `Conditions` on `GO_WITH_CONDITIONS` are empty/tampered, trailing JSON tokens or control bytes exist, symlinks/`> 64 KiB` approval inputs are passed, or any CI runner env (`CI`, `GITHUB_ACTIONS`, `GITHUB_RUN_ID`, `GITHUB_WORKFLOW`, `RUNNER_OS`, `GITLAB_CI`, `CIRCLECI`, `BUILDKITE`, `TF_BUILD`, `JENKINS_URL`) is active — guaranteeing automated CI can never unlock `GATE-REL-07`.
   - Enforces **`Go >= go1.26.11`** minimum release build toolchain in `publish` mode (ordered strictly after `VerifyHumanApproval` so unapproved publish always yields `exit 4` / `403 human_release_approval_required`).
   - Wired `--mode dry-run` into Stage 0 of [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) (`GATE-REL-07` remains `Status: "BLOCKED"`).

2. **Live Dual-Binary Canary Rollout & `< 60s` `DR-04` Rollback Drill ([`scripts/release/canary_rollback_drill_test.go`](../../scripts/release/canary_rollback_drill_test.go)):**
   - Compiles `./cmd/tanpopo` (`CGO_ENABLED=0`), boots `baseline`, bootstraps owner, provisions a `Virtual Key`, stops `baseline`, applies an **`Expand-Only` N+1 additive column** (`ALTER TABLE organizations ADD COLUMN canary_expand_only_note ...`) directly onto `baseDBFile`, clones via parameterized `VACUUM INTO ?` (`file:///` + `busy_timeout(5000)`), and **restarts both `baseline` (`N-1` binary role) and `canary` against the expanded schema**.
   - Establishes `4` live `baseline`-backed SSE streams across a readiness barrier, steps a loopback-restricted (`net.Dialer.Control`) weighted reverse-proxy across `5%` (`5/100`), `25%` (`25/100`), and `50%` (`50/100`) stages, injects `20` canary `5xx` errors (`ALT-GATEWAY-BURN`), shifts weight atomically to `0%`, drains/stops `canary`, and confirms `50/50` (`100%`) `200 OK` on `baseline` with **`0` premature SSE drops** in **`24.76 ms` (`-race`, `< 60s` SLA)**.

3. **`v1.0.0` Release Readiness Dossier ([`docs/releases/v1.0.0-readiness-dossier.md`](../releases/v1.0.0-readiness-dossier.md)):**
   - Full `GATE-REL-01..07` ledger calibrated transparently under `COND-PO-01` (`Go >= 1.26.11` toolchain floor, standalone `103.69 req/s` SQLite ceiling, Docker LB `4/1,410` race without `preStop`, bounded fuzz vs `>= 4h` soak `UNVERIFIED`, offline `lighthouse`/`INP` `UNVERIFIED`, `8/16` ad-hoc Playwright routes & `0` committed `e2e/*.spec.ts`, open WCAG SC 2.5.3 `label-content-name-mismatch` [`GATE-REL-05` `PARTIAL / WAIVER_REQUIRED`], `WebKit` loopback `__Host-` cookie `PARTIAL`, `astryx doctor agent-docs` pre-existing fail, non-`en`/`vi` fallback copy, and macOS/Windows container isolation `UNVERIFIED`) plus explicit `GO_WITH_CONDITIONS` / `NO-GO` Product Owner sign-off sheet.

---

## 2. Independent Strong Review Sign-Off (`REQUIRES_STRONG_REVIEW`)

| Reviewer | Role | Verdict | Findings & Remediation Summary |
|---|---|---|---|
| `efd83b7a08f62c5da177f06093e6ea18` | `code-reviewer` | **`APPROVED`** *(after remediation)* | Initial review flagged 2 critical (`N-1` `Expand-Only` check applied only to `canary` DB, router-terminated SSE without barrier/drop check, unsigned `Conditions` in `CanonicalApprovalPayload`) and 5 required items (`lockedBuffer` race, `TANPOPO_BOOTSTRAP_TOKEN_FILE`, Windows `Kill()` fallback, `modernc` pragma DSN, `manifest` parent `MkdirAll`, `SPEC-QA-04` gate alignment, honest `DR-04` scope). All remediated, tested, re-inspected, and **`APPROVED`** (`0` Critical, `0` Required). |
| `2eec40183b6e4fb7cc99a0a34c0a3e03` | `security-auditor` | **`APPROVED`** *(after remediation)* | Initial audit flagged 1 High (unauthenticated `Conditions` & optional `ExpectedCommitSHA` anti-replay), 4 Medium (trailing JSON / symlinks / `os.OpenRoot`, narrowed CI env + dry-run `PASS` status, loopback `Dialer.Control` / proxy `Transport` / `node.logs` race / `VACUUM INTO ?`, `8/16` UI gap + `Go >= 1.26.11` floor + `SPEC-QA-04` taxonomy), and 1 Low (missing negative tests). All remediated, re-audited, and **`APPROVED`** (`0` Critical/High/Medium/Low). |

---

## 3. Empirical Verification Summary

- **`go test -v -race -count=1 ./scripts/release/...`:** **`PASS`** (`10.066s` — all `pre_publish_check_test.go` + live dual-binary canary `DR-04` drill in **`24.76 ms`**, `0` dropped SSE streams).
- **`CGO_ENABLED=0 go run ./scripts/release/pre_publish_check.go --mode dry-run`:** **`PASS`** (`GATE-REL-01..06` `PASS`, `23` SHA-256 entries, `GATE-REL-07 = BLOCKED_PENDING_HUMAN_SIGN_OFF`).
- **Compiled Unapproved Publish `NEG-1` Check (`pre_publish_check` default mode):** Exits **`4`** (`exit status 4`) with `HTTP 403 human_release_approval_required`.
- **`CGO_ENABLED=0 go run ./scripts/ci/check.go --all && go vet ./... && CGO_ENABLED=0 go run ./scripts/docs/check_drift.go --skip-quickstart`:** **`PASS`**.
- **Git Tags / GitHub Releases (`GATE-REL-07`):** **`0` tags created; `0` releases published.**
