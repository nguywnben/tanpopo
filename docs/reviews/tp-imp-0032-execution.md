# TP-IMP-0032 Execution Record

## Current checkpoint

- Unit: `TP-IMP-0032`
- Governance state: `READY` under `DEC-061`
- Implementation branch: persistent `develop`
- Draft PR: [#6](https://github.com/nguywnben/tanpopo/pull/6)
- Latest pushed checkpoint: `7bbcf85` (operator quarantine API with atomic ETag checking and verified stream-abort barrier)
- Acceptance state: **IN PROGRESS**. This record does not claim `DONE`.

## Work performed

- **GPT-6 Luna** is implementing the Unit 32 CLI and lifecycle integration, coordinating verification, and will record acceptance only after review and CI evidence.
- **McClintock** implemented the ordered eight-check doctor contract and declarative GitOps reconciliation package.
- **Parfit** implemented authenticated encrypted archive verification and restore, including platform-specific private staging and atomic publication.
- **Zeno** added the initial live-process EXT-ACCEPT-01 process contract test. It checks scaffolding, process identity and API surface availability; its comments explicitly leave authenticated lifecycle and routing outcomes unproven.
- **Carver** added a live binary test covering bootstrap, owner login/session, CSRF enforcement, a four-scope blast-radius response, and fail-closed behavior when the live runtime has no extension archive resolver.
- **Pasteur** supplied the acceptance-gap checklist used to plan concurrent backup, shredding, drain, upgrade-preflight, and insecure-KEK tests.
- **Curie** added focused tests for readiness and graceful shutdown while an SSE response is active, plus incompatible-upgrade preflight output and no-side-effect checks.
- **Mencius** implemented the opt-in local marketplace archive resolver, including path confinement, symlink/type rejection, and a digest-verified private snapshot. **GPT-6 Luna** is integrating the resolver into the live bootstrap/install acceptance flow.

## Verification evidence

- The Cloud `go` and `gofmt` launchers are unusable in this task (`go version` returns `Go: Unknown option: version`). Go compilation, tests, race checks, and formatting gates must therefore be established through repository CI or another configured runner.
- `git diff --check` passed before the `888187f` checkpoint.
- CI run [44334760617](https://github.com/nguywnben/tanpopo/actions/runs/44334760617) failed Stage 0 because two new tests contained unapproved `t.Skip` calls and an existing integration test used an unguarded HTTP client. These findings were fixed in `888187f`.
- CI run [44335158522](https://github.com/nguywnben/tanpopo/actions/runs/44335158522) passed Stage 0 but failed Stage 1 because of compile errors in the new CLI. Those compile errors were fixed in `626ed18`.
- CI run [44335944731](https://github.com/nguywnben/tanpopo/actions/runs/44335944731) passed Stage 0. Ubuntu Stage 1 exposed a stale support-bundle assertion for the old doctor report and two live tests building `.` instead of `./cmd/tanpopo`; these are being corrected in the next checkpoint. macOS and Windows results were still running at the time of this record.
- CI run [44336743635](https://github.com/nguywnben/tanpopo/actions/runs/44336743635) passed Stage 0, then failed Stage 1 on all three operating systems because the live EXT-ACCEPT-01 contract found that `plugin init` could not scaffold the required `@acme-cloud` namespace. It also confirms the earlier implementation and quality gates pass before reaching that acceptance assertion. The CLI now accepts a validated `--namespace`, and an online-backup acceptance test is being added; this diff has not yet been run by CI.
- CI run [44338227474](https://github.com/nguywnben/tanpopo/actions/runs/44338227474) passed Stage 0 and Ubuntu/macOS Stage 1. Windows Stage 1 failed when `t.TempDir` cleanup could not remove `tanpopo.db` after `TestDataPlaneRoleDoesNotMountControlPlaneOrMintBootstrapToken`; the Go test itself emitted no assertion failure. This is under investigation as a Windows handle cleanup issue. The run's online backup acceptance test passed on Ubuntu/macOS.
- CI run [44339674223](https://github.com/nguywnben/tanpopo/actions/runs/44339674223) passed Windows Stage 1, while macOS Stage 1 exposed that the online snapshot could start before any concurrent writer had committed its first ledger/audit pair. The test now waits for all 20 workers to commit before starting the backup, then keeps all writers active during snapshot and requires at least 20 of each record in the restore. Ubuntu results were still running when recorded.
- CI run [44340464974](https://github.com/nguywnben/tanpopo/actions/runs/44340464974) caught a compile error in the new live install test: its approval request type was defined only in another Go package. A test-local request type now fixes the reference. macOS and Windows jobs were still running at the time of the compile failure.
- CI run [44341017802](https://github.com/nguywnben/tanpopo/actions/runs/44341017802) passed all four required checks on `a2b2a6a` (Stage 0 and Stage 1 on Ubuntu, macOS, and Windows). It covers local archive staging and initial permission approval; it does not cover the later live upgrade/re-consent/rollback assertions or OpenAPI/operator-doc edits now in the local diff.
- CI run [44346144897](https://github.com/nguywnben/tanpopo/actions/runs/44346144897) ran on `c2f00a3`: Stage 0, Ubuntu, and macOS passed; Windows failed during `t.TempDir` cleanup because `tanpopo.db` was still held open after `TestDataPlaneRoleDoesNotMountControlPlaneOrMintBootstrapToken`. No assertion failed in that test. The same Windows cleanup failure was observed in run `44338227474` and the Windows job passed in `44339674223`; treat it as a flaky teardown symptom pending a clean repeat, not as a waived gate.

## Current local changes (not yet pushed)

- Added an authenticated `POST /admin/v1/extensions/{installationId}/quarantine` operation. It requires CSRF, `If-Match`, an instance `OPERATOR`, and a reason; establishes the extension kill-switch barrier before changing installation state, then refreshes snapshots and returns the updated ETag.
- Added OpenAPI and RBAC coverage, plus live acceptance assertions for quarantine and all four blast-radius response scopes.
- `git diff --check` passed. The Cloud Go launcher remains unusable, so these changes still require the repository's Actions checks.
- CI run [44347742188](https://github.com/nguywnben/tanpopo/actions/runs/44347742188) on `bb2d896` exposed a stale OpenAPI conformance assertion (`73` routes expected; `74` registered after quarantine was added) on Ubuntu, macOS, and Windows. This assertion is updated to `74` in the current local diff. The Windows job did not reproduce the earlier database temp-directory cleanup failure.
- **Turing** (**code-reviewer**) found that the first quarantine version checked `If-Match` before, but not inside, the lifecycle transaction and could skip/assume an aborting kill switch. **Linnaeus** (**security-auditor**) independently flagged the stream-barrier issue. Commit `7bbcf85` adds an atomic expected-version lifecycle method and re-verifies/executes the active abort barrier on idempotent requests; focused regression tests cover stale ETags, late registered streams, and fail-closed handling of a non-aborting switch. Full independent reviews must be repeated on the final diff.
- Current local tests add a `<1s` assertion around the live quarantine API, verify `tanpopo doctor` and production server startup reject a POSIX Master KEK file with `0644` permissions, and retry the Windows test's data-directory cleanup for up to one second after runtime shutdown. They have not yet run in CI.
- CI run [44349279446](https://github.com/nguywnben/tanpopo/actions/runs/44349279446) on `7bbcf85`: Stage 0 and Ubuntu passed; Windows failed again during `t.TempDir` cleanup in `TestDataPlaneRoleDoesNotMountControlPlaneOrMintBootstrapToken`; macOS was still running when recorded. The cleanup retry is in the next local checkpoint so a persistent handle leak still fails the test.
- Next: checkpoint the route-count correction and review fixes, then finish EXT-ACCEPT-01 routing/settlement/quarantine-timing evidence and the remaining shredding/KEK startup and doctor acceptance gaps. Obtain fresh independent code and security reviews on the final diff before any merge.

## Acceptance gaps still open

- Complete the ten-step EXT-ACCEPT-01 workflow with actual extension install/approval, update/re-consent/rollback, EU routing and exact/estimated settlements, quarantine timing, and populated blast-radius evidence. Route probes and fail-closed install checks do not satisfy these steps.
- Complete the ten-step EXT-ACCEPT-01 workflow with EU routing and exact/estimated ledger verification, quarantine timing, and populated blast-radius results. The live process test now exercises archive staging, permission approval, permission escalation/re-consent, rollback, and CSRF/ETag checks.
- Complete project shred plus KEK rotation and insecure KEK startup/doctor acceptance tests. The concurrent online-backup/restore and graceful SSE drain / incompatible-upgrade preflight tests are in the current implementation diff; they still require CI confirmation.
- Validate the full implementation against the Unit 32 DoD and obtain final independent code and security reviews on the exact final diff.
- Merge only after all four required Stage 0/Stage 1 checks pass, then verify acceptance CI and the DONE record on the resulting `origin/main` SHA.
