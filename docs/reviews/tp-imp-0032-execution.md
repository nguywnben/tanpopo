# TP-IMP-0032 Execution Record

## Current checkpoint

- Unit: `TP-IMP-0032`
- Governance state: `READY` under `DEC-061`
- Implementation branch: persistent `develop`
- Draft PR: [#6](https://github.com/nguywnben/tanpopo/pull/6)
- Latest pushed checkpoint: `888187f1f1d28b61486fee06335700e7b01ad986`
- Acceptance state: **IN PROGRESS**. This record does not claim `DONE`.

## Work performed

- **GPT-6 Luna** is implementing the Unit 32 CLI and lifecycle integration, coordinating verification, and will record acceptance only after review and CI evidence.
- **McClintock** implemented the ordered eight-check doctor contract and declarative GitOps reconciliation package.
- **Parfit** implemented authenticated encrypted archive verification and restore, including platform-specific private staging and atomic publication.
- **Zeno** added the initial live-process EXT-ACCEPT-01 process contract test. It checks scaffolding, process identity and API surface availability; its comments explicitly leave authenticated lifecycle and routing outcomes unproven.
- **Carver** added a live binary test covering bootstrap, owner login/session, CSRF enforcement, a four-scope blast-radius response, and fail-closed behavior when the live runtime has no extension archive resolver.
- **Pasteur** supplied the acceptance-gap checklist used to plan concurrent backup, shredding, drain, upgrade-preflight, and insecure-KEK tests.

## Verification evidence

- The Cloud `go` and `gofmt` launchers are unusable in this task (`go version` returns `Go: Unknown option: version`). Go compilation, tests, race checks, and formatting gates must therefore be established through repository CI or another configured runner.
- `git diff --check` passed before the `888187f` checkpoint.
- CI run [44334760617](https://github.com/nguywnben/tanpopo/actions/runs/44334760617) failed Stage 0 because two new tests contained unapproved `t.Skip` calls and an existing integration test used an unguarded HTTP client. These findings were fixed in `888187f`.
- CI run [44335158522](https://github.com/nguywnben/tanpopo/actions/runs/44335158522) passed Stage 0 but failed Stage 1 because of compile errors in the new CLI. Those compile errors were fixed in `626ed18`.
- CI run [44335944731](https://github.com/nguywnben/tanpopo/actions/runs/44335944731) passed Stage 0. Ubuntu Stage 1 exposed a stale support-bundle assertion for the old doctor report and two live tests building `.` instead of `./cmd/tanpopo`; these are being corrected in the next checkpoint. macOS and Windows results were still running at the time of this record.

## Acceptance gaps still open

- Complete the ten-step EXT-ACCEPT-01 workflow with actual extension install/approval, update/re-consent/rollback, EU routing and exact/estimated settlements, quarantine timing, and populated blast-radius evidence. Route probes and fail-closed install checks do not satisfy these steps.
- Add and run the required concurrent online-backup/restore, project shred plus KEK rotation, graceful SSE drain, incompatible-upgrade no-side-effect, and insecure KEK startup/doctor acceptance tests.
- Validate the full implementation against the Unit 32 DoD and obtain final independent code and security reviews on the exact final diff.
- Merge only after all four required Stage 0/Stage 1 checks pass, then verify acceptance CI and the DONE record on the resulting `origin/main` SHA.
