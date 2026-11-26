# CP01–02 — Exact RC7 runtime, fresh install, Console surface traversal

**QA campaign:** QA-005 / TEEP-00 baseline stabilization  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime artifact:** `/tmp/qa005-bin/tanpopo`, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`  
**Build:** Go `1.26.11 linux/amd64`, `-trimpath`, `CGO_ENABLED=1`; Go build metadata reports the exact source revision and `vcs.modified=false`. CLI reports `0.1.0-dev (commit=none built=unknown)` because this QA build did not inject release linker flags. This is a local source build, not the RC7 release artifact.

## Environment

- Isolated instance: SQLite WAL; data and ephemeral 256-bit Master KEK under `/tmp/qa005-runtime`; HTTP on `127.0.0.1:18145`; metrics disabled.
- Chromium `156.0.8078.79`; Node `24.22.3`; real browser automation via Playwright package already installed in the Cloud Node runtime. Browser was headless because no desktop JS tool was exposed in this turn; screenshots are browser-rendered and were opened for visual inspection.
- No provider API was called. Credentials made available by the environment were not used. No external paid usage.
- First response `/healthz`: HTTP 200. UI shell and API server started from the same compiled process.

## Fresh install — PASS (scoped)

### Reproduction

1. Start the exact binary with a new `TANPOPO_DATA_DIR`, `TANPOPO_DB_PATH`, `TANPOPO_MASTER_KEY_FILE`, loopback listen address and disabled metrics listener.
2. Open `/setup` in Chromium at 1440×1000. Blank bootstrap form rendered with owner email/password, confirmation, organization and project fields.
3. Enter the one-time token from this runtime's private `data/bootstrap.token`, synthetic owner `qa005-owner@example.invalid`, a disposable QA-only password, organization `QA005 Org`, and project `Initial Project`.
4. Follow the First connection step, where the interface states no adapters are active and recommends completing setup before Marketplace activation; continue without an initial connection.
5. Confirm the master key custody checkbox, review owner/org/project and “Initial connection: Skipped”, then select “Complete setup”.
6. Verify the browser transitions to `/overview`; capture API response and request the bootstrap status endpoint.

### Observed result

- `POST /admin/v1/bootstrap/complete` returned 201; session check and initialization calls returned 200.
- `/admin/v1/bootstrap/status` returned `initialized: true`, `marketplace_catalog_count: 8`, no available adapters. Overview displayed the signed-in owner, `QA005 Org / Initial Project / dev`, zero requests/spend and a healthy nominal empty state.
- Owner session remained authenticated through the Overview bootstrap/API reads. No JS page errors observed.
- Visual inspection: setup stages are readable, progress is prominent and the Review page accurately shows connection skipped. Overview sidebar, workspace selector and empty-state cards render without overlap at this desktop viewport.
- Screenshot evidence: `evidence/screenshots/CP01-fresh-setup-initial.png`, `CP01-owner-form-redacted.png`, `CP01-first-connection.png`, `CP01-key-custody.png`, `CP01-review.png`, `CP01-after-setup.png`.

### Scope limits

This is one fresh SQLite/`all`-role installation with no adapter or model configured. It does not establish adapter-enabled setup, other service roles, persistence after restart, bootstrap recovery on reload, invitation flows, or any production signing status. Closing the browser before setup completion discards the client-side progress and returns a new context to step 1; this was observed while diagnosing the wizard, but no accepted persistence contract was identified, so it is not assigned as a defect. Retain as a beginner/recovery UX observation for evaluation.

## Console primary navigation — PARTIAL

### Reproduction

1. Sign in in Chromium using the owner account from the preceding fresh setup.
2. Visit each primary sidebar navigation item by real link click, wait for rendering, capture a full-page PNG and collect Admin API status/method/path plus page errors.

### Observed result

Visited: Overview, Traces, FinOps, Providers, Models, Routes & Policies, Extensions, MCP Hub, Playground, Access & Keys, Security, Operations. Each screen rendered its declared empty or default state. All observed Admin API reads returned HTTP 200; no browser page errors occurred. Screenshots: `evidence/screenshots/CP02-*.png`.

The current all-owner, empty-tenant screen inventory is only a route/load smoke test. It does not prove forms, mutations, accounting, protocol traffic, RBAC, persisted-state consistency or other browser/platform coverage. In particular, Overview's “All systems nominal” copy is only an empty-state summary and not a broad health assertion.

## Checkpoint classification

| Scope | Result |
|---|---|
| Exact source and built runtime identity | PASS (local source build only) |
| Fresh setup and initial owner session | PASS (scoped) |
| SQLite runtime readiness | PASS (startup only) |
| Console primary route rendering | PARTIAL |
| Stateful business functionality | UNVERIFIED |
| Chromium accessibility, responsive, localization and cross-browser | UNVERIFIED |
| PostgreSQL/Valkey/cluster, restart and restore | UNVERIFIED |
| Provider connectivity/real provider interoperability | BLOCKED (not attempted; paid use not authorized) |
| Official RC7 release artifact identity | UNVERIFIED |
| Production signature integration | BLOCKED by owner-reported canonical archive digest disagreement; production key/signing scope is explicitly excluded |

QA-005 continues. No historical defect has been marked passed from this smoke phase. `GATE-REL-07` remains BLOCKED.
