# TP-IMP-0031 Execution Checkpoint

**Status:** `READY` — implementation is in progress. This checkpoint does not satisfy Unit 31 DoD and does not accept the unit.

## Participants

- Implementer: **GPT-6 Luna**.
- Independent reviewers: **Lovelace** (`code-reviewer`), who returned **APPROVE** for the embedded-console checkpoint only; **Noether** (`code-reviewer`), who reviewed the expanded CLI/support-bundle diff, requested changes that were addressed, and returned **APPROVE**; and **Sagan** (`code-reviewer`), who returned **APPROVE** for the Windows archive hardening.
- Acceptor: **GPT-6 Luna**, delegated by `DEC-060`; acceptance is pending all Unit 31 requirements, DoD, required CI, and merge verification.

## Completed Checkpoint

- Added an embedded production Console bundle under `internal/webui/dist/`, including precompressed gzip siblings required for clean-checkout `go:embed` builds.
- Added an HTTP handler with SPA history fallback, API path exclusion, missing-asset 404 behavior, content security and anti-framing headers, immutable caching for hashed assets, gzip negotiation, and matching `HEAD` representation metadata.
- Added tests for security headers, SPA fallback, API and asset 404s, gzip quality values, cache headers, and `HEAD` behavior.
- Added a `tanpopo support-bundle -o` command that collects bounded config, lock, metrics, and log inputs; applies structured metadata redaction, strips Master Key assignments from malformed/plain-text inputs, scans the final archive, enforces owner-only file modes, and refuses symlinked or replaced log-directory/file inputs and output collisions.
- Moved the reusable canary scanner into `internal/security/canary` so production code follows the repository rule against importing `internal/testkit`.
- Reviewer findings on gzip quality parsing, `Vary`, `HEAD` lengths, and bundle inclusion were addressed before approval.
- Windows CI follow-up: the POSIX mode-bit assertion is now conditional on non-Windows systems; Windows creates support archives with a protected owner-only DACL supplied at file creation, verifies that ACL before writing, and has ACL tests for both creation and final bundle output. The rooted-directory regression accepts Windows' refusal to rename an open directory only after confirming the original directory identity remains at the path.

## Verification Evidence

- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -count=1 ./...` — passed after the final code changes.
- `go run ./scripts/ci/check.go --all` — passed.
- `go test -race ./internal/webui/... ./internal/cli/support/... ./internal/security/canary/...` — passed.
- `CGO_ENABLED=0 go build ./cmd/tanpopo` — passed.
- Windows amd64 and macOS arm64 `go test -c ./internal/cli/support` cross-compiles, `go vet` on changed packages, and `git diff --check` — passed.
- Independent review: **Lovelace** (`code-reviewer`) — **APPROVE**, limited to this checkpoint.
- Expanded diff review: **Noether** (`code-reviewer`) — **APPROVE**, with all requested changes addressed.
- Windows ACL review: **Sagan** (`code-reviewer`) — **APPROVE**.

## Remaining Unit 31 Scope

The full CLI command tree and output contract, fully wired single-binary Data Plane/Admin API/Console server, local-only owner recovery, support-bundle doctor/probe diagnostics, and their required acceptance tests remain outstanding. Do not mark TP-IMP-0031 `DONE` until those items and all required GitHub checks are verified.

## Delivery Blocker

GitHub API access is restored in the current task. PR #4 is open as a draft on the checkpoint SHA and CI is visible. Run `44301886133` passed Stage 0, Ubuntu, and macOS but failed two Windows tests: Unix mode-bit reporting was incorrectly asserted on Windows, and the directory-replacement test assumed Windows permits renaming an open directory. These failures are being fixed with a protected Windows DACL and platform-equivalent rooted-directory assertions. No unit acceptance is claimed until the full diff, review, four checks, merge, and post-merge CI are complete.
