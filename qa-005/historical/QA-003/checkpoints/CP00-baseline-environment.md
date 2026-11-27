# CP00 — RC5 baseline and environment inventory

Date: 2026-11-25 (Asia/Saigon)

## Baseline integrity

- QA-003 worktree: `/workspace/tanpopo-qa-003`, branch `qa/qa-003-regression`.
- `HEAD` verified as `3423d4f523ef0ac77469a1201b8b753d1d31c902`.
- GitHub `origin/main` and `origin/develop` were previously observed at this same RC5 commit; QA-001 and QA-002 remote refs resolve to their user-specified historical commits.
- Worktree was clean at resume. Product source remains unmodified.
- QA-001 and QA-002 checkouts remain separate and unchanged.

## Environment findings

- Cloud environment reports running/connected with unrestricted egress enforced. Both configured QA provider variable names report ready; their values were not read, emitted, or copied. No provider request was made in this inventory, so spend is USD 0 to date.
- Docker client/server work through the managed Unix socket (server version 29.9.2).
- Chromium 156, Node 24, Playwright and Python Playwright are installed. This establishes automation tooling, not yet product/browser coverage. Actual screenshot inspection remains pending.
- `go.mod` requires Go 1.26.0. `/usr/bin/go` is not a functioning Go toolchain: `go version` returns `Go: Unknown option: version`; `go env` also fails. No approved host package installer is present. Go-in-container/toolchain restoration is pending.
- No RC5 release artifact was returned by the GitHub release listing. Artifact provenance is therefore UNVERIFIED; exact-source build and metadata verification remain pending.
- Browser status from Cloud capability metadata is unknown. Headless Playwright is available locally; Firefox/WebKit availability is not established.

## QA-003 disposition

All 39 historical defects currently remain UNVERIFIED against RC5. Feature/screen/workflow coverage was carried as a scope inventory only; all QA-003 statuses reset to UNVERIFIED. GATE-REL-07 remains BLOCKED. This checkpoint is not a test-completion claim.
