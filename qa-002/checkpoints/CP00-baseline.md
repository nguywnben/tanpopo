# CP00 — Baseline and environment

**Date:** 2026-11-24 (Asia/Saigon)
**Requested baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Checked-out revision:** exactly `40bba5b55c2da6fc00b107b315c9b3167fb20169`, merge of PR #43; branch `qa/qa-002-regression`.

## Baseline integrity and artifact inventory

- Remote `main`, remote `develop`, and remote `HEAD` each resolved to the requested commit at inspection. The historical QA-001 branch remained at `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f`.
- Repository has no Git tag at this revision and no checked-in RC4 archive/binary. The checked-out source defaults in `cmd/tanpopo/main.go` to version `0.1.0-dev`; an RC4 version must therefore be established by building with the documented CI/release version flags or identifying a separately published RC4 artifact. Do not infer an RC4 release version from the user’s label.
- The merged engineering commits are `15ade22` (backend/CLI/storage/audit/A2A fixes DEF-002..011) and `e8af25b` (Console/accessibility/WebKit/FinOps fixes DEF-001..015), followed by `e05ed3f` Postgres audit coverage. Build commit and embedded Console assets will be verified after a successful build.
- Prebuilt embedded Console bundle exists at `internal/webui/dist/` (~4.5 MB). No local RC4 binary or release archive was found in the repository scan. Remote release/artifact inventory remains to be queried.

## Environment facts and recovery

- Cloud environment status: connected/running; outbound networking unrestricted and enforced. Runtime bindings `QA_META_MODEL_API_KEY` and `QA_GOOGLE_AI_STUDIO_API_KEY` report ready. The two shell variable names are present and nonempty; values were not displayed.
- Node `v24.22.3`, npm `11.19.1`; Chromium `156.0.8078.79`; Docker client/server `29.9.2`; global Playwright `1.62.1` resolved. Firefox/WebKit availability remains unverified.
- `/usr/bin/go` is the unrelated two-player board game, not the Go compiler (`go version` responds “Unknown option”). Repository declares Go `1.26.0`; release dossier requires Go `>=1.26.11`. Downloaded official Go `1.26.11 linux/amd64` to `/tmp/tanpopo-qa002-go` and verified the archive SHA-256 against `go.dev/dl` metadata before extraction. Keep using that explicit toolchain path.
- `npm ci --ignore-scripts` initially failed because the default cache path `/home/agent/.npm` does not exist. Retried successfully with `npm_config_cache=/tmp/tanpopo-qa-002-npm-cache`; 365 packages installed from the committed lockfile with integrity/TLS checks enabled. No tracked repository files were intentionally changed.
- Docker CLI responds, but runtime behavior/readiness is not yet tested. No product server has been started yet.

## Remaining CP00 validation

- Build source with Go 1.26.11, report actual binary version/commit/build metadata, and compare to any published RC4 artifact if available.
- Run the documented Go CI/unit gates and Console typecheck, tests, and build; inspect all failures.
- Start disposable isolated Tanpopo runtime(s), perform functional readiness and first-run Setup using real browser interactions, and visually inspect screenshots.
- Verify access to Google and the synthetic/alternate-provider fixture independently without exposing credentials; classify proxy/provider/runtime failures separately.
- Record successful Docker/Postgres/Valkey availability or exact blockers before claiming platform coverage.

## Current outcome

**PARTIAL / IN PROGRESS.** Revision and initial toolchain facts verified. No RC4 binary was verified, no application startup or end-to-end user journey has passed yet, and no historical defect is yet verified fixed on this revision. GATE-REL-07 remains BLOCKED.
