# TP-IMP-0031 Execution Checkpoint

**Status:** `DONE` — accepted under `DEC-061` after PR #4 merged and all four post-merge CI jobs passed on `origin/main`.

## Participants

- Implementer: **GPT-6 Luna**.
- Independent reviewers: **Lovelace** (`code-reviewer`), who returned **APPROVE** for the embedded-console checkpoint only; **Noether** (`code-reviewer`), who reviewed the earlier expanded CLI/support-bundle diff and returned **APPROVE** after requested changes; **Sagan** (`code-reviewer`), who returned **APPROVE** for Windows archive hardening and reviewed the expanded Unit 31 diff; **Avicenna** (`code-reviewer`), who reviewed the readiness update and required persisted-key verification; **Franklin** (`code-reviewer`), who returned **APPROVE** after the KEK and owner-scope regressions were addressed; and **Hubble** (`code-reviewer`), who returned **APPROVE** on the final Windows-specific test correction.
- Test strategy contributor: **Dalton** (`test-engineer`), who mapped acceptance coverage for the server composition, local owner recovery, diagnostics bundle, and standalone binary. The implementation and required tests remain owned by **GPT-6 Luna**.
- Acceptor: **GPT-6 Luna**, delegated by `DEC-060`; accepted Unit 31 under `DEC-061` after confirming the Unit 31 DoD, final independent review, PR checks, merge, and post-merge main CI.

## Completed Checkpoint

- Added an embedded production Console bundle under `internal/webui/dist/`, including precompressed gzip siblings required for clean-checkout `go:embed` builds.
- Added an HTTP handler with SPA history fallback, API path exclusion, missing-asset 404 behavior, content security and anti-framing headers, immutable caching for hashed assets, gzip negotiation, and matching `HEAD` representation metadata.
- Added tests for security headers, SPA fallback, API and asset 404s, gzip quality values, cache headers, and `HEAD` behavior.
- Added a `tanpopo support-bundle -o` command that collects bounded config, lock, metrics, and log inputs; applies structured metadata redaction, strips Master Key assignments from malformed/plain-text inputs, scans the final archive, enforces owner-only file modes, and refuses symlinked or replaced log-directory/file inputs and output collisions.
- Added the `tanpopo doctor` command and a bounded, secret-free report in support bundles. It checks local data/config/lock/log/metrics files and probes `/healthz` and `/readyz` only on loopback through the guarded egress client.
- Added the remote CLI groups `config`, `provider`, `model`, `route`, `key`, `plugin`, `budget inspect`, `audit`, and `killswitch`, with JSON/table/YAML output, canonical exit mapping, terminal escape sanitization for headers and values, request/response bounds, HTTPS for non-loopback servers, and a default SSRF-guarded client. Route explain sends its JSON request body; audit verify uses POST.
- Added `serve --role all|data-plane|control-plane` runtime composition for SQLite, audit, vault, snapshot, kill-switch, broker, Admin API, Data Plane, and embedded Console. API paths cannot fall through to the SPA. The process exposes separate liveness/readiness probes and shuts down gracefully on interrupt/TERM.
- Added `admin reset-local-owner`: it requires a restricted local Master KEK file, proves that key unwraps the selected owner's protected Org DEK from this database, atomically resets the password and appends a SYSTEM audit event, then increments human-principal versions so all browser sessions are invalidated. The generated password is emitted only after commit and output format is validated before any mutation.
- Added acceptance tests for the standalone binary in an otherwise empty directory, service-role routing, health probes, local owner recovery, wrong-key rejection, all-session invalidation, audit-chain verification, and machine-readable CLI output.
- Moved the reusable canary scanner into `internal/security/canary` so production code follows the repository rule against importing `internal/testkit`.
- Reviewer findings on gzip quality parsing, `Vary`, `HEAD` lengths, and bundle inclusion were addressed before approval.
- Windows CI follow-up: the POSIX mode-bit assertion is conditional on non-Windows systems; Windows creates support archives with a protected owner-only DACL supplied at file creation, verifies that ACL before writing, and has ACL tests for both creation and final bundle output. The rooted-directory regression accepts Windows' refusal to rename an open directory only after confirming the original directory identity remains at the path. CI run `44316505917` found a Windows-only test assumption in the local Master KEK recovery test: `chmod 0644` does not express Windows ACLs. The test now preserves the POSIX mode assertion and on Windows exercises rejection of a directory used as a key file; this is a test correction, not a weakening of the POSIX key-file policy.

## Verification Evidence

- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go run ./scripts/ci/check.go --all` — passed.
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -count=1 ./...` — passed on rerun after one timing-sensitive failure in `TestTP0021_StreamingLeaseRenewalPreventsPrematureReclaim`.
- `go test -race ./cmd/tanpopo ./internal/cli/... ./internal/iam/auth ./internal/security/vault ./internal/security/broker ./internal/config/snapshot ./internal/admin/http ./internal/ingress/http` — passed.
- Windows amd64 and macOS arm64 command-package cross-compiles, `go vet` for the changed packages, and focused Windows test-package cross-compilation — passed.
- GitHub PR run [`44318018781`](https://github.com/nguywnben/tanpopo/actions/runs/44318018781) passed Stage 0 and Stage 1 on Ubuntu, macOS, and Windows for implementation SHA `c8ee35d720525b3d32a48e5d96495d59c5be6a27`.
- Post-merge GitHub run [`44319629695`](https://github.com/nguywnben/tanpopo/actions/runs/44319629695) passed all four required jobs on main SHA `6eb603aea20926f326d5c8acdc129a029ded0d39`.
- Independent review: **Lovelace** (`code-reviewer`) — **APPROVE**, limited to this checkpoint.
- Expanded diff review: **Noether** (`code-reviewer`) — **APPROVE**, with all requested changes addressed.
- Windows ACL review: **Sagan** (`code-reviewer`) — **APPROVE**.
- Expanded current-diff review: **Sagan** (`code-reviewer`) — **REQUEST_CHANGES**. Findings covered binding Master KEK proof to DB key material, using POST for audit verify, sanitizing table headers, adding health probes and `tanpopo doctor`, validating output format before password reset, and checking vault plus plugin-engine readiness. These changes are implemented.
- Readiness review: **Avicenna** (`code-reviewer`) — **REQUEST_CHANGES** because readiness initially checked only for an active key ID. Readiness now verifies an existing persisted Org DEK envelope can be unwrapped, and tests cover a wrong but syntactically valid key.
- Final review: **Franklin** (`code-reviewer`) — **APPROVE**. The review confirmed the owner recovery role binding is constrained to the principal's organization and the regression test prevents issuing a recovery credential for a mismatched binding. The reviewer noted as optional that readiness verifies the first persisted Org key envelope rather than scanning all organizations.
- Final Windows test review: **Hubble** (`code-reviewer`) — **APPROVE**. Confirmed that POSIX mode-bit enforcement remains covered on POSIX, Windows exercises non-regular key-file rejection, and owner recovery remains constrained to matching organization scope.

## Acceptance Record

Implementation commit `c8ee35d720525b3d32a48e5d96495d59c5be6a27` was merged via PR #4 at main SHA `6eb603aea20926f326d5c8acdc129a029ded0d39`. All four PR checks passed in run `44318018781`; all four post-merge checks passed in run `44319629695`. **Franklin** approved the complete implementation including the owner-scope regression; **Hubble** approved the final Windows test correction. Unit 31 is accepted as `DONE` under `DEC-061` by **GPT-6 Luna**.

## Delivery Status

GitHub API access is restored. PR [#4](https://github.com/nguywnben/tanpopo/pull/4) merged successfully. Final implementation SHA: `c8ee35d720525b3d32a48e5d96495d59c5be6a27`; main acceptance SHA: `6eb603aea20926f326d5c8acdc129a029ded0d39`; PR CI: [run `44318018781`](https://github.com/nguywnben/tanpopo/actions/runs/44318018781); post-merge CI: [run `44319629695`](https://github.com/nguywnben/tanpopo/actions/runs/44319629695). `develop` was fast-forwarded to the merged main SHA. Unit 32 DoR was confirmed, and its status is proposed as `READY` under `DEC-061`; implementation must wait until this governance acceptance is merged and verified on main.
