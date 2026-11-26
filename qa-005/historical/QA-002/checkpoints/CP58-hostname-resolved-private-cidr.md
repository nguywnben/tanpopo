# CP58 — Hostname-resolved private IP and explicit CIDR enforcement

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for mismatched-CIDR denial and matching-CIDR positive control; no SSRF bypass observed. DEF-026 and QA-OBS-004 recurred.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Test and outcome

Used the real MCP Hub registration and Sync tools flow in Chromium against an isolated Streamable HTTP fixture on `127.0.0.1:19183`. The synthetic hostname `127.0.0.1.nip.io` resolved to IPv4 loopback `127.0.0.1` (`getent ahostsv6` also reported the IPv4-mapped form `::ffff:127.0.0.1`). No request content was written to the fixture log; it records only JSON-RPC method, path, and Host header.

For the negative case, registered `http://127.0.0.1.nip.io:19183/mcp` with explicit private-endpoint consent but a nonmatching `127.0.0.2/32` CIDR. Sync returned HTTP 502 `mcp_sync_failed` and the generic UI message “upstream MCP tool synchronization failed.” The fixture request count remained 0 before and after; the persisted server remained ACTIVE with zero tools. This confirms that this resolved loopback peer did not bypass the explicit CIDR boundary. The generic diagnostic repeats QA-OBS-004.

For the positive control, registered the same hostname and fixture with exact `127.0.0.1/32`. Sync reached the fixture for `initialize`, `notifications/initialized`, and `tools/list`; one tool persisted and the server entered `SCHEMA_DRIFT_LOCKED`. The green result banner said “0 schema changes require approval,” while the server remained locked with one newly discovered tool. This independently reproduces DEF-026; no grant or tool execution occurred.

This phase tests one public DNS wildcard name resolving to loopback. It is not a DNS-rebinding or multi-address answer test. The two QA-only server rows remain in the isolated test database; the product exposes no MCP server DELETE operation in its documented API. No credential was registered, no sensitive endpoint was contacted, and product implementation was not changed.

## Evidence

- [`../evidence/CP58-hostname-cidr-enforcement.json`](../evidence/CP58-hostname-cidr-enforcement.json) — observed resolver outputs, CIDRs, persisted server states, failure response, zero-request negative fixture and three-call positive fixture log.
- [`../evidence/CP58-nipio-mismatch-form.png`](../evidence/CP58-nipio-mismatch-form.png) — visually inspected negative registration with explicit private consent and mismatched CIDR.
- [`../evidence/CP58-nipio-mismatch-sync.png`](../evidence/CP58-nipio-mismatch-sync.png) — visually inspected fail-closed UI error.
- [`../evidence/CP58-nipio-allowlisted-control-form.png`](../evidence/CP58-nipio-allowlisted-control-form.png) and [`../evidence/CP58-nipio-allowlisted-control-sync.png`](../evidence/CP58-nipio-allowlisted-control-sync.png) — positive control and DEF-026 first-discovery message/state mismatch, visually inspected.
- [`../evidence/CP58-mcp-registry-cidr-results.png`](../evidence/CP58-mcp-registry-cidr-results.png) — reloaded MCP registry showing both persisted final states.

## Consolidated status and remaining coverage

- **Historical DEF-001–015:** no CP58 retest; keep the current scoped QA-002 status. DEF-005 remains partial: the tested hostname mismatch failed closed, while DNS rebinding, mixed/multiple DNS answers, HTTPS/chained redirects, alternate runtime behavior, and IPv4-mapped notation support remain open.
- **Current new defects:** DEF-016–028 remain open; no new defect was assigned. DEF-026 recurred on first schema discovery. QA-OBS-004 generic sync diagnostics recurred.
- **Coverage completed:** hostname-to-private resolution, nonmatching CIDR rejection before endpoint contact, explicit matching-CIDR positive control, persisted server/tool state, and real UI screenshot review.
- **Blocked:** none in CP58. CP53 webhook delivery remains blocked by unavailable Operator credentials.
- **Remaining:** actual DNS rebinding/mixed-address response testing, redirects over HTTPS and chains, alternate runtime, broader CIDR/address representations, and all other incomplete product/role/platform coverage. See [`../STATUS-SUMMARY.md`](../STATUS-SUMMARY.md) and [`../COVERAGE-MATRIX.md`](../COVERAGE-MATRIX.md).

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
