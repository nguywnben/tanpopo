# `TP-IMP-0005` — Bootstrap Monorepo Layout, Align `Apache-2.0` License (`ISS-002`), `.gitignore`, `CONSTRAINTS.md` & Cross-Platform `Stage 0/1` CI

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0005` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Status** | **`DONE`** *(Accepted `2026-11-14` by Product Owner via `DEC-011` at commit `1f4f5a19a9ab8b53b82342e048970ea565500161`; GitHub Actions Run `43584413913` `SUCCESS` across Stage 0 and Stage 1 `ubuntu-24.04`, `macos-15`, `windows-2022`; `REQUIRES_STRONG_REVIEW` approved; closes `ISS-002..004, 008`)* |
| **Size** | `M` (`go.mod`, `.gitignore`, `web/console/`, `CONSTRAINTS.md`, `scripts/ci/check.go` & GitHub Actions) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Establishes `Floor Guard` quality gates, root Go module path & repository layout)* |
| **Technical Owner** | `Staff Platform Engineer + CI/Security Engineer` |

---

## 1. Intent
Establish a clean physical repository foundation that executes identically across **Linux, macOS, and Windows PowerShell** per [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md) and [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md):
1. Initialize root `go.mod` with the single canonical module path **`module github.com/nguywnben/tanpopo`** (`go 1.24.0`).
2. Relocate initial root frontend files (`package.json`, `package-lock.json`, `astryx-themes.js`, `src/*`) into `web/console/` while preserving the `neutral` theme intact (`ISS-002`..`004`: set `"license": "Apache-2.0"`, explicitly declare `react@19.3.1` & `react-dom@19.3.1`, move `@astryxdesign/cli` to `devDependencies`, and set `"type": "module"`).
3. Harden `.gitignore` (excluding `/tanpopo`, `*.exe`, SQLite `*.db*`, `*.key`/`*.pem` secrets, `*.tpop.enc` backups, and `.worktrees/`) and build the pure-Go cross-platform quality gate **`scripts/ci/check.go`** enforcing `CONSTRAINTS.md` (`Floor Guard`, `Apache-2.0` license compliance, and one-way package DAG enforcement) with zero `bash` dependency.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod` (`module github.com/nguywnben/tanpopo`), `cmd/tanpopo/main.go` (minimal bootstrap stub supporting `--version`).
  - Canonical package directory skeleton with `doc.go` boundary declarations: `internal/{pkg,testkit,storage,config,security,iam,extension,provider,policy,ratelimit,routing,finops,telemetry,webhook,ingress,admin,webui,cli,ops}`.
  - `.gitignore` (complete exclusion rules for Go binaries, SQLite WAL files, secret/backup artifacts, and `git worktree` directories) and `.gitattributes` (`eol=lf` across source/generated files so Windows clean checkouts preserve exact LF line endings for `astryx doctor` `theme-module`).
  - Relocate `package.json`, `package-lock.json`, `astryx-themes.js`, `src/*` into `web/console/`, standardize all placeholder demo UI copy in `web/console/src/App.tsx` to English (`ISS-008` — preserving Astryx layout/theme and zero raw `div`/`span`), and update path notes in `AGENTS.md` (preserving `100%` of the `<!-- ASTRYX:START -->..<!-- ASTRYX:END -->` and `<!-- TANPOPO-HANDOFF:START -->..<!-- TANPOPO-HANDOFF:END -->` blocks).
  - `CONSTRAINTS.md`, `scripts/ci/check.go` (runnable via `go run ./scripts/ci/check.go` across Windows/macOS/Linux; recognizes `tools/spikes/go.mod` as an isolated `M0` spike module while forbidding any `cmd/` or `internal/` import from `tools/spikes/`), and `.github/workflows/ci.yml` (`Stage 0` & `Stage 1`, pre-seeded with `OPENAI_API_KEY=CANARY_FORBIDDEN_CI_KEY`).
- **Out-of-Scope:** Vite/TypeScript configuration (`tsconfig.json`, `tsc --noEmit`) and production UI screen scaffolding belong to `TP-IMP-0026` (`Stage 1` conditionally activates `npm run typecheck` once `web/console/tsconfig.json` lands in `TP-IMP-0026`); zero storage or networking logic (owned by `TP-IMP-0006`+).

## 3. Requirements, Dependencies & Design References
- **Requirements & Issues Refs:** `TP-NFR-0901`, `TP-NFR-1001`, `ISS-002`..`004`, `ISS-008`, `RSK-012`, `GATE-REL-01`, `GATE-REL-04`.
- **Dependencies:** `TP-IMP-0003`.
- **Design Refs:** [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md), [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md), [`release-readiness.md`](../../quality/release-readiness.md) Section 2, [`test-strategy.md`](../../quality/test-strategy.md) Section 5, [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Section 1.1.

## 4. API, Wire & Data Contracts
- **One-Way Package Dependency DAG (enforced by `scripts/ci/check.go`):**
  `pkg` ← `storage/port` ← `storage/{sqlite,postgres,migrate}` ← `security/{vault,egress,broker,oauth,audit,killswitch,foureyes}` ← `config/snapshot` ← `{iam,extension,provider,policy,ratelimit,routing,finops,telemetry,webhook}` ← `{ingress,admin,webui,ops}` ← `cli` ← `cmd/tanpopo` (`0` import cycles; strictly ban importing `tools/spikes` or using `http.DefaultClient`).
- **`CONSTRAINTS.md` & `Floor Guard`:** Automatically fail any PR containing: (a) unticketed `.skip` / `t.Skip()`, (b) `//nolint:gosec`, `@ts-ignore`, `@ts-expect-error`, `eslint-disable`, or `# gitleaks:allow`, (c) raw `div`/`span` elements inside `web/console/src`, (d) dependencies carrying `GPL`/`AGPL`/`SSPL` licenses (with `.agents/plugin.json` tooling metadata explicitly allowlisted under its upstream `MIT` license compatible with `Apache-2.0`), or (e) competing secondary lockfiles (`yarn.lock` / `pnpm-lock.yaml`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given the bootstrapped repository layout, When running `go run ./scripts/ci/check.go --pure-go-build` and `npx @astryxdesign/cli doctor` inside `web/console/`, Then the `tanpopo` binary builds cleanly (`CGO_ENABLED=0`) and `astryx doctor` reports `0 fail` with `defaultThemeSlug = "neutral"` preserved `100%`.
- **`AC-2`:** Given `LICENSE` and `web/console/package.json`, When running `go run ./scripts/ci/check.go --license`, Then all project source metadata aligns on `Apache-2.0` (`ISS-002` closed) with `0` forbidden copyleft dependencies.
- **`AC-3`:** Given `.github/workflows/ci.yml` running on Linux runners (`npx @astryxdesign/cli doctor`) and local verification on Windows PowerShell (`cmd /c npx @astryxdesign/cli doctor`), Then `Stage 0` & `Stage 1` complete in `< 3 minutes`.
- **`NEG-1`:** Injecting a synthetic `@ts-ignore`, `//nolint:gosec`, `http.DefaultClient`, or unticketed `t.Skip()` fixture into `check.go` unit tests → exits non-zero.
- **`NEG-2`:** Running `astryx theme eject` or manually editing generated regions of `astryx-themes.js` → strictly forbidden.
- **`NEG-3`:** Never permit `npm ci` to execute unvetted `postinstall` lifecycle scripts (`--ignore-scripts` is mandatory).

## 6. Test Commands & Required Evidence
- Cross-platform verification (identical across Windows PowerShell, macOS, and Linux):
  - `go run ./scripts/ci/check.go --all`
  - `go test -v ./...` *(plus `go test -v -race ./...` on environments with a C toolchain)*
  - Inside `web/console`: `npm ci --ignore-scripts` + `npx @astryxdesign/cli doctor` *(Windows PowerShell: `cmd /c npx @astryxdesign/cli doctor`)*.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Lock internal supply-chain hygiene from the very first code commit (readonly `go.sum` + `npm ci --ignore-scripts` + `gitleaks` + `.gitignore` blocking secret/DB files).
- **Rollback:** Single atomic structural PR containing zero user data; cleanly revertible via `git revert` back to the documentation baseline if needed.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner has approved `ADR-0001`, `ADR-0004`, `SD-G1-05` (`Apache-2.0`), and `TP-IMP-0003` is complete.
- **DoD:** Closes `ISS-002`, `ISS-003`, `ISS-004`, `ISS-008`; `go run ./scripts/ci/check.go --all` and `Stage 0/1 CI` pass `100%`; `REQUIRES_STRONG_REVIEW` approved.
