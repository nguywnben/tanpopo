# TP-IMP-0032 Execution Record

## Current checkpoint

- Unit: `TP-IMP-0032`
- Governance state: `READY` under `DEC-061`
- Implementation branch: persistent `develop`
- Draft PR: [#6](https://github.com/nguywnben/tanpopo/pull/6)
- Latest pushed checkpoint: `ff07a2e` (live test compile fix is in the current local diff)
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

## Acceptance gaps still open

- Complete the ten-step EXT-ACCEPT-01 workflow with actual extension install/approval, update/re-consent/rollback, EU routing and exact/estimated settlements, quarantine timing, and populated blast-radius evidence. Route probes and fail-closed install checks do not satisfy these steps.
- Complete the ten-step EXT-ACCEPT-01 workflow with EU routing and exact/estimated ledger verification, update/re-consent/rollback, quarantine timing, and populated blast-radius results. A live archive stage/permission-approval flow is now being added to the process test.
- Complete project shred plus KEK rotation and insecure KEK startup/doctor acceptance tests. The concurrent online-backup/restore and graceful SSE drain / incompatible-upgrade preflight tests are in the current implementation diff; they still require CI confirmation.
- Validate the full implementation against the Unit 32 DoD and obtain final independent code and security reviews on the exact final diff.
- Merge only after all four required Stage 0/Stage 1 checks pass, then verify acceptance CI and the DONE record on the resulting `origin/main` SHA.
