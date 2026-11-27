# CP25 — MCP CIDR mismatch negative test with reachable positive control

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite Tanpopo `127.0.0.1:18086`; local synthetic instrumented MCP fixture `127.0.0.1:19110`; Chromium real Console interaction and screenshots visually inspected. No source changes.  
**Release gate:** `GATE-REL-07` **BLOCKED**.

## Question and result

Does the configured non-wildcard CIDR prevent Tanpopo from contacting a private MCP endpoint outside that range? **Scoped PASS** for a literal loopback endpoint and one address mismatch. The negative server's sync attempt returned an error, and its fixture recorded no requests. The same endpoint with a matching loopback CIDR completed sync and produced the expected three MCP negotiation requests. This does not exhaust DNS, redirects, IPv6, address changes, ports, or other egress paths.

## Reproduction and evidence

- Registered `qa002-cp25-denied` at `http://127.0.0.1:19110/mcp`, with private/loopback explicitly enabled but allowed CIDR set to `127.0.0.2/32`. Registration returned 201 and the row persisted with Active state, zero tools. The actual form is `evidence/CP25-mcp-cidr-mismatch-filled.png`; registration response and visible state are `CP25-mcp-cidr-mismatch-register.json` and `CP25-mcp-cidr-mismatch-registered.png`.
- Reset the synthetic fixture request log, then clicked Sync tools for that row through the real UI. The API returned HTTP 502 `mcp_sync_failed`/`upstream_error`, generic message `upstream MCP tool synchronization failed`, `retryable=true`; the page showed the same generic error. Fixture state remained `requests: []`. Screenshot `CP25-mcp-cidr-mismatch-sync.png` was visually inspected; sanitized result is `CP25-mcp-cidr-mismatch-sync.json`.
- As a positive control, registered a second server at the exact same URL with CIDR `127.0.0.1/32`. Sync returned 200, discovered one tool, and the same instrumented fixture recorded `initialize`, `notifications/initialized`, and `tools/list`. The state then showed `SCHEMA_DRIFT_LOCKED` because the new tool had not yet been pinned; this expected state was visible in `CP25-mcp-cidr-control-sync.png` (visually inspected) and recorded in `CP25-mcp-cidr-control-sync.json`.

The differential control corroborates that the fixture was reachable and that the mismatched configuration prevented the app request from arriving. Registration itself permits storing a CIDR that does not match the literal endpoint; sync is the enforcement boundary in this scenario.

## Classification

- **DEF-005:** extends runtime coverage for required CIDR governance. The mismatch was blocked before endpoint contact; DEF-005 remains PARTIAL PASS because only a literal loopback mismatch was tested and other SSRF/address-resolution dimensions remain open.
- **Product defect:** none confirmed by this experiment. The generic retryable message and registry row remaining `Active` are recorded separately as unresolved UX/operations observation [QA-OBS-004](../reports/QA-OBS-004-mcp-allowlist-failure-diagnostic.md). Active-state semantics and safe error disclosure need clarification before defect classification.
- Sync failure in this test was induced by our intentionally mismatched allowlist, not by Cloud egress or a product crash. The matching-CIDR control succeeded on the same app and fixture.

## Residual coverage

DNS rebinding, mixed public/private resolution, redirect targets, IPv6 and IPv4-mapped addresses, port changes, CIDR normalization, actual blocked address details, non-owner roles, server disable/delete recovery, and workspace/project boundaries remain UNVERIFIED. No product code was modified and no release stability conclusion is implied.
