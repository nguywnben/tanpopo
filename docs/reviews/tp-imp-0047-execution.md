# `TP-IMP-0047` Execution & Verification Report — Release Readiness Dossier (`GATE-REL-01..07`), Fail-Closed `pre_publish_check.go` & `< 60s` Live Canary Rollback (`DR-04`)

- **Unit ID:** `TP-IMP-0047` (`M5` / `EP-09` · **`REQUIRES_STRONG_REVIEW`**)
- **Model:** **Gemini 4 Hydrogen**
- **Decision:** `DEC-096` & `DEC-097` *(delegated under `DEC-092` M5 sequential authorization — stopping strictly at `GATE-REL-07` with zero tags/releases)*
- **Branch / PR:** `develop` → `main` *(Initial M5 PR [#39](https://github.com/nguywnben/tanpopo/pull/39) merged at `main` SHA [`543e40ac`](https://github.com/nguywnben/tanpopo/commit/543e40ac33dbaaa83a75ae822dc507095a25e4f7), CI [`45011516212`](https://github.com/nguywnben/tanpopo/actions/runs/45011516212) & [`45011516326`](https://github.com/nguywnben/tanpopo/actions/runs/45011516326) `SUCCESS`; follow-up closure PR [#40](https://github.com/nguywnben/tanpopo/pull/40) merged at `main` SHA [`e115ae4a`](https://github.com/nguywnben/tanpopo/commit/e115ae4ae2da4199c1fb9e24f087977ce8269a11), CI [`45037833002`](https://github.com/nguywnben/tanpopo/actions/runs/45037833002) & [`45037833008`](https://github.com/nguywnben/tanpopo/actions/runs/45037833008) `SUCCESS`)*
- **Verdict:** **`READY_FOR_GATE_REL_07`** *(All engineering deliverables, follow-up UI/i18n/E2E/SQLite/Docker HA closures, independent strong reviews, and `< 60s` canary drills verified; **`GATE-REL-07` human Product Owner launch sign-off remains `BLOCKED` fail-closed**)*

---

## 1. Delivered Scope

1. **Pre-Publish Release Gate & `NEG-1` Human Approval Guard ([`scripts/release/pre_publish_check.go`](../../scripts/release/pre_publish_check.go), [`pre_publish_check_test.go`](../../scripts/release/pre_publish_check_test.go)):**
   - Evaluates **`GATE-REL-01`..`06`** aligned 1:1 with [`release-readiness.md`](../quality/release-readiness.md) §3 (`SPEC-QA-04`), confines repository file reads via `os.OpenRoot`, verifies Dual-DB migration sets, `Apache-2.0` (`LICENSE` + `web/console/package.json`), `--skip=publish` dry-run workflows, `15` CLI + `15` UI locales, embedded Web Console, committed [`web/console/e2e/console_e2e.py`](../../web/console/e2e/console_e2e.py), `ALT-GATEWAY-BURN`, and writes a deterministic `23`-entry `SHA256SUMS` manifest cleanly creating missing parent directories via `writeManifestFileSafely` (`os.Lstat` non-symlink guarded).
   - Enforces fail-closed **`NEG-1` (`HTTP 403 human_release_approval_required`, exit status `4`)** whenever Ed25519 PO approval is missing/invalid, `--commit-sha` (`40-hex`) is missing or mismatched in `publish` mode, `Conditions` on `GO_WITH_CONDITIONS` are empty/tampered, trailing JSON tokens or control bytes exist, symlinks/`> 64 KiB` approval inputs are passed, or any CI runner env (`CI`, `GITHUB_ACTIONS`, `GITHUB_RUN_ID`, `GITHUB_WORKFLOW`, `RUNNER_OS`, `GITLAB_CI`, `CIRCLECI`, `BUILDKITE`, `TF_BUILD`, `JENKINS_URL`) is active — guaranteeing automated CI can never unlock `GATE-REL-07`.
   - Enforces **`Go >= go1.26.11`** minimum release build toolchain in `publish` mode (ordered strictly after `VerifyHumanApproval` so unapproved publish always yields `exit 4` / `403 human_release_approval_required`).
   - Wired `--mode dry-run` into Stage 0 of [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) (`GATE-REL-07` remains `Status: "BLOCKED"`).

2. **Live Dual-Binary Canary Rollout & `< 60s` `DR-04` Rollback Drill ([`scripts/release/canary_rollback_drill_test.go`](../../scripts/release/canary_rollback_drill_test.go)):**
   - Compiles `./cmd/tanpopo` (`CGO_ENABLED=0`), boots `baseline`, bootstraps owner, provisions a `Virtual Key`, stops `baseline`, applies an **`Expand-Only` N+1 additive column** (`ALTER TABLE organizations ADD COLUMN canary_expand_only_note ...`) directly onto `baseDBFile`, clones via parameterized `VACUUM INTO ?` (`file:///` + `busy_timeout(5000)`), and **restarts both `baseline` (`N-1` binary role) and `canary` against the expanded schema**.
   - Establishes `4` live `baseline`-backed SSE streams across a readiness barrier, steps a loopback-restricted (`net.Dialer.Control`) weighted reverse-proxy across `5%` (`5/100`), `25%` (`25/100`), and `50%` (`50/100`) stages, injects `20` canary `5xx` errors (`ALT-GATEWAY-BURN`), shifts weight atomically to `0%`, drains/stops `canary`, and confirms `50/50` (`100%`) `200 OK` on `baseline` with **`0` premature SSE drops** in **`< 60s` (`-race`)**.

3. **Follow-Up Hardening & Full-Matrix Closure (`DEC-097`):**
   - **Web Console Accessibility, Deep-Linking & Reverse-Proxy Origin:** Resolved SC 2.5.3 `label-content-name-mismatch` in `SetupPage.tsx` & `ConsoleShell.tsx`, Dark destructive button contrast in `tokens.css`, `astryx doctor --built dist` (`0 fail`), SPA history routing for `/mcp/*` & HTML `GET /a2a/agents` (`internal/webui/embed.go`), `TANPOPO_ALLOWED_ORIGINS` env parsing in `cmd/tanpopo/serve.go`, and native `15/15` locale translation across all Unit 45 Air-Gap/Multi-Org keys (`0` English fallback).
   - **Committed 3-Browser HTTPS E2E (`web/console/e2e/console_e2e.py`, `tlsproxy/main.go`):** Verified `SCR-01..SCR-16` across `Chromium` (`61` audits), `Firefox` (`24` audits), and `WebKit` (`24` audits) with HTTPS `__Host-` cookie reload, Multi-Org create/switch (`Bravo Org`), `Ctrl+K` Command Palette, `vi`/`ja` localization, `0` Axe serious/critical violations, `0` horizontal overflow, `0` console/page/external reqs, and Chrome CDP CWV/INP (`TTFB 9.6-14.3ms, FCP 124-192ms, CLS 0, INP 0ms`).
   - **Fuzzing, SQLite Hot-Path & Multi-Container Docker Compose HA:** Eliminated 2 redundant SELECT queries in `ledger.Service.Reserve`, batched async OTel trace writes (`maxPersistBatchSize = 64`), added opt-in `TANPOPO_PRESTOP_WAIT` in `serve.go`, ran `7/7` fuzz targets (`30s` each, `0` crashes, `FuzzArchiveAndManifest` `86,648` / `FuzzLedgerInvariants` `897` execs), re-measured standalone SQLite (`109.48 req/s`, `2,346/2,346 [DONE]`, `530/684` concurrent short reqs, `2,735/2,735` overload checks), verified `docker compose build` (`exit 0`), and ran Docker Compose HA (`2` `tanpopo:local` replicas + LB + non-superuser `PG16 FORCE RLS` + 3-shard `Valkey 7001..7003`: `56.86 req/s` at `100 RPS`, `574/575` [`99.82%`] rolling restart, `4,071 ms` `DR-04` rollback).

4. **`v1.0.0` Release Readiness Dossier ([`docs/releases/v1.0.0-readiness-dossier.md`](../releases/v1.0.0-readiness-dossier.md)):**
   - Full `GATE-REL-01..07` ledger calibrated transparently under `COND-PO-01` (`Go >= 1.26.11` toolchain floor, offline `lighthouse` CLI absent, `>= 4h` fuzz soak `UNVERIFIED`, 2-vCPU SQLite & single-VM Docker HA throughput ceilings, `1/575` Compose rolling-restart race, physical multi-host K8s & macOS/Windows container isolation `UNVERIFIED`) plus explicit `GO_WITH_CONDITIONS` / `NO-GO` Product Owner sign-off sheet.

---

## 2. Independent Strong Review Sign-Off (`REQUIRES_STRONG_REVIEW`)

| Reviewer | Role | Verdict | Findings & Remediation Summary |
|---|---|---|---|
| `efd83b7a08f62c5da177f06093e6ea18` | `code-reviewer` | **`APPROVED`** *(PR #39)* | Initial review flagged 2 critical (`N-1` `Expand-Only` check applied only to `canary` DB, router-terminated SSE without barrier/drop check, unsigned `Conditions` in `CanonicalApprovalPayload`) and 5 required items; all remediated, re-inspected, and **`APPROVED`** (`0` Critical, `0` Required). |
| `2eec40183b6e4fb7cc99a0a34c0a3e03` | `security-auditor` | **`APPROVED`** *(PR #39)* | Initial audit flagged 1 High, 4 Medium, and 1 Low item; all remediated, re-audited, and **`APPROVED`** (`0` Critical/High/Medium/Low). |
| `2eedff75962759f79007c007fba5e018` | `code-reviewer` | **`APPROVED`** *(`DEC-097` closure)* | Fresh review flagged 3 governance/status sync items plus optional error/path/fuzz checks (`tracer.go` drop metric, `fuzz_test.go` `GetReservation` check, `serve.go` `preStopWaitFromEnv` CLI validation + `/a2a/agents` normalization + `TestA2AAndMCPSPADispatch`, `pentest-v1.0-remediation-report.md` fuzz template wording); all remediated, re-inspected, and **`APPROVED`** (`0` Critical/Required). |
| `bc3b206c8294c2d319ffa7b463ccb985` | `security-auditor` | **`APPROVED`** *(`DEC-097` closure)* | Fresh security audit flagged 1 Medium (`bootstrap` directory/owner hardening), 2 Low (`TANPOPO_ALLOWED_ORIGINS` URL/loopback validation + OIDC `TrustedProxyCIDRs` enforcement), and 2 Info items (`shlex.quote` wrapper); all Medium/Low/Info code findings remediated, tested (`TestEnsureTokenRejectsSharedOrWritableDir`, `TestAllowedOriginsEnvAndLoopbackFallback`, `TestAdminOIDCAndEnterpriseRBAC`), re-audited, and **`APPROVED`** (`0` Critical/High/Medium). |

---

## 3. Empirical Verification Summary

- **`GOTOOLCHAIN=go1.26.11 go test -v -race -count=1 ./scripts/release/...`:** **`PASS`** (all `pre_publish_check_test.go` + live dual-binary canary `DR-04` drill `< 60s`).
- **`GOTOOLCHAIN=go1.26.11 CGO_ENABLED=0 go run ./scripts/release/pre_publish_check.go --mode dry-run`:** **`PASS`** (`GATE-REL-01..06` `PASS`, `GATE-REL-07 = BLOCKED_PENDING_HUMAN_SIGN_OFF`).
- **Compiled Unapproved Publish `NEG-1` Check (`pre_publish_check` default mode):** Exits **`4`** (`exit status 4`) with `HTTP 403 human_release_approval_required`.
- **`GOTOOLCHAIN=go1.26.11 CGO_ENABLED=0 go run ./scripts/ci/check.go --all && go vet ./... && go run ./scripts/docs/check_drift.go --skip-quickstart`:** **`PASS`**.
- **Web Console Full Verification:** `npm run typecheck`, `npm run check:i18n` (`15` native locales), `npm test`, `npm run build`, `npm run sync:embed`, `npx astryx doctor --built dist` (`0 fail`), and `npm run test:e2e` (`Chromium` + `Firefox` + `WebKit` `SCR-01..16` HTTPS) **`PASS`**.
- **Git Tags / GitHub Releases (`GATE-REL-07`):** **`0` tags created; `0` releases published.**
