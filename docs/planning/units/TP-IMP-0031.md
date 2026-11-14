# `TP-IMP-0031` — Unified `tanpopo` CLI, Embedded Static SPA (`embed.FS`), Break-Glass `reset-local-owner` & Redacted `support-bundle`

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0031` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0014`, `TP-IMP-0025`, `TP-IMP-0027`, `TP-IMP-0028`, `TP-IMP-0029`, `TP-IMP-0030` — `GAP-01, 13`)* |
| **Size** | `M` (`cmd/tanpopo/*`, `internal/cli/*`, `internal/webui/embed.go`) |
| **Review Tier** | `STANDARD_REVIEW` *(Unified single binary `tanpopo`, offline break-glass recovery & zero-leak diagnostic `support-bundle` `OPS-AC-10`)* |
| **Technical Owner** | `Developer Experience & Systems Lead` |

---

## 1. Intent
Unify the entire product into **a single executable binary `tanpopo`** (`ADR-0001`, `CR-10` — eliminating any split `tanpopoctl` naming fragmentation):
1. Embed the compiled `web/console/dist` static bundle directly into the Go binary via `embed.FS` so `tanpopo serve` serves the Data Plane, Admin API, and Web Console out of the box without requiring Node.js or Nginx at runtime (`TP-FR-1001`).
2. Complete the administrative CLI command tree (`tanpopo serve`, `config`, `provider`, `model`, `route`, `key`, `plugin`, `budget inspect` for `INC-01` runbooks, `audit`, `killswitch`) supporting `--output json|table|yaml`, the canonical exit-code table `0 / 2 / 10 / 14 / 22 / 30` defined in `SPEC-OPS-04`, and **strict terminal escape sanitization stripping hazardous `ANSI / OSC-52 clipboard escape` sequences (`\x1b]52;...`) across all human-readable table outputs (`CT-27`)**.
3. Deliver the local-disk break-glass recovery command **`tanpopo admin reset-local-owner`** for single-node deployments without SMTP when an administrator loses their password (`CR-09`), alongside the secret-scrubbed diagnostic bundler **`tanpopo support-bundle`** (`OPS-AC-10`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `cmd/tanpopo/main.go` (single unified entrypoint supporting `--role=all|data-plane|control-plane`).
  - `internal/webui/embed.go` (serving pre-compressed static assets with `Cache-Control: immutable` for hashed bundles, SPA history fallback for UI routes, and security headers `CSP`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` — meeting Lighthouse CI budgets `LCP <= 2.0s`, `INP <= 150ms`, `CLS <= 0.05` per `UX-AC-08`).
  - `internal/cli/root.go`, `internal/cli/sanitize/ansi.go` (`CT-27` ANSI/OSC-52 control-sequence stripper) & remote/local administrative subcommands (including `tanpopo budget inspect`).
  - `internal/cli/admin/reset_owner.go` (`tanpopo admin reset-local-owner`: requires proof of local host ownership by verifying read access to the `Master KEK` file, appends a `LOCAL_BREAK_GLASS_OWNER_RESET` event to the `"SYSTEM"` audit hash chain, and revokes all active browser sessions).
  - `internal/cli/support/bundle.go` (`tanpopo support-bundle`: collects redacted configuration, health probes, `tanpopo.lock` inventory, Prometheus metrics, and recent logs routed through `privacy.Redactor` + automated `CanaryScanner` self-verification prior to writing the `.tar.gz` archive).
- **Out-of-Scope:** `tanpopo bundle export/verify/import` air-gapped supply-chain commands for enterprise fleet distribution (covered in `TP-IMP-0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0903`, `TP-FR-1001`, `TP-NFR-1002`, `ADR-0001`, `ADR-0004`, `CT-27`, `UX-AC-08`, `OPS-AC-10`, `INC-01`, `CR-09`, `CR-10`, `GAP-13`.
- **Dependencies:** `TP-IMP-0014`, `TP-IMP-0025`, `TP-IMP-0027`, `TP-IMP-0028`, `TP-IMP-0029`, `TP-IMP-0030`.
- **Design Refs:** [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-09`, `CR-10`).

## 4. API, Wire & Data Contracts
- **Automation-Safe Output Contract:** When `--output json` is specified, `stdout` emits strictly a single valid JSON object/array (routing all progress/warning messages to `stderr`) so downstream `jq` pipelines never break.
- **Local-Only Guard on `reset-local-owner`:** Executes exclusively via direct local CLI access on the host holding the KEK/database files — **never exposed as a remote HTTP endpoint under `/admin/v1/*`**.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Single Binary Self-Host`):** Copying only the single `tanpopo` binary into a completely empty directory (no `node_modules`, no `web/` directory) and executing `tanpopo serve` brings up `/v1/*`, `/admin/v1/*`, and the embedded Web Console at `100%` functionality.
- **`AC-2` (`CR-09` Break-Glass Password Recovery):** When the `ORG_OWNER` password is lost, running `tanpopo admin reset-local-owner --db ...` mints a one-time recovery password, invalidates all existing sessions, and appends a verifiable `LOCAL_BREAK_GLASS_OWNER_RESET` entry to the `"SYSTEM"` audit hash chain.
- **`AC-3` (`OPS-AC-10` Zero-Leak Support Bundle):** After seeding canary secrets `sk-tp-canary-...`, `TANPOPO_MASTER_KEY`, and `vk_tp_live_...`, running `tanpopo support-bundle -o diag.tar.gz` and scanning every extracted file confirms **`0` leaked secret bytes**.
- **`NEG-1` (`CT-27` CLI Terminal Escape Injection & Unauthorized Reset):** Package names or log entries containing malicious `\x1b]52;c;...` sequences have all `ESC` control bytes stripped before rendering in CLI table mode; invoking `tanpopo admin reset-local-owner` from an OS user lacking read permission on the `Master KEK` file fails with exit code `14`.

## 6. Test Commands & Required Evidence
- Build static SPA: `cd web/console && npm run build`
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./cmd/tanpopo/... ./internal/cli/... ./internal/webui/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./cmd/tanpopo/... ./internal/cli/... ./internal/webui/...`)*
- Race detector (`-race`): `go test -v -race ./internal/cli/... ./internal/webui/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Generated `diag.tar.gz` archives are created with `0600` owner-only permissions and self-scanned against secret-pattern detectors before reporting completion.
- **Rollback:** Atomic single-file binary swap back to the prior `tanpopo` executable.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0014`, `TP-IMP-0025`, and all `M2` Web Console units (`0027`..`0030`) are complete.
- **DoD:** Standalone static binary runs cleanly, and both `OPS-AC-10` and `reset-local-owner` recovery tests pass.
