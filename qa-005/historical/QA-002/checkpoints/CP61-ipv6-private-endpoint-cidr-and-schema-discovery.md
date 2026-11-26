# CP61 — Direct IPv6 MCP endpoint CIDR controls and first discovery

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for fail-closed `::2/128` mismatch and exact `::1/128` positive control; DEF-026 and QA-OBS-004 recurred. No new defect or SSRF bypass.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Execution

Used the real MCP Hub form and Sync action in Chromium. The synthetic Streamable HTTP fixture listened only on IPv6 loopback `::1:19185`, returned one inert tool schema, and stored only JSON-RPC method/path metadata. No provider, external endpoint, credential, or user data was contacted.

For the negative control, registered `http://[::1]:19185/mcp` with private-endpoint consent and a nonmatching `::2/128` CIDR. Registration returned 201 and left an Active row with zero tools. Sync returned 502 `mcp_sync_failed`; the actual Console displayed the generic red “upstream MCP tool synchronization failed” notice. The fixture received zero requests. This preserves the explicit CIDR boundary and repeats QA-OBS-004’s diagnostic gap.

For the positive control, used the same exact IPv6 loopback endpoint and `::1/128`. The actual Sync action showed the green one-tool success banner and contacted only the local fixture for `initialize`, `notifications/initialized`, and `tools/list`. One tool persisted. A second sync response was captured as HTTP 200, `synced_count:1`, `drifted_count:0`, and repeated those three fixture methods. Both rows stayed isolated in the test database; no tool was granted or called.

The positive UI state independently reproduces DEF-026: the new tool is visibly **Drift locked** and the server is **Schema Drift Locked**, while the green sync banner says “0 schema changes require approval.” The same inconsistency persists after a second sync. No approval was made. Screenshots of both registration forms and resulting UI states were visually inspected.

## Evidence

- [`../evidence/CP61-ipv6-mcp-cidr-results.json`](../evidence/CP61-ipv6-mcp-cidr-results.json) — sync response statuses, persisted tool/server state, and all six metadata-only fixture calls.
- [`../evidence/CP61-qa002-cp61-ipv6-deny-r2-form.png`](../evidence/CP61-qa002-cp61-ipv6-deny-r2-form.png) and [`../evidence/CP61-qa002-cp61-ipv6-deny-r2-sync.png`](../evidence/CP61-qa002-cp61-ipv6-deny-r2-sync.png) — explicit mismatch form and fail-closed state.
- [`../evidence/CP61-qa002-cp61-ipv6-allow-r2-form.png`](../evidence/CP61-qa002-cp61-ipv6-allow-r2-form.png), [`../evidence/CP61-qa002-cp61-ipv6-allow-r2-sync.png`](../evidence/CP61-qa002-cp61-ipv6-allow-r2-sync.png), and [`../evidence/CP61-ipv6-allow-recheck.png`](../evidence/CP61-ipv6-allow-recheck.png) — exact allowlist, initial sync, first-discovery mismatch, and repeated sync.

## Consolidated QA status

- **Historical DEF-001–015:** not retested in CP61; keep the existing scoped QA-002 status. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Confirmed open defects:** DEF-016–028 remain open. DEF-026 recurred on a direct IPv6 loopback first-discovery case; CP61 does not change its existing severity or expected/actual report. No additional defect assigned.
- **Observations:** QA-OBS-004 recurred. QA-OBS-011 remains undocumented shorthand-host compatibility. No observation was closed.
- **Coverage completed:** direct IPv6 literal registration, explicit mismatching and exact /128 CIDRs, zero-contact negative control, local positive sync, same-schema resync, persistent server/tool state, and visual confirmation of DEF-026.
- **Remaining:** DNS rebinding/mixed-answer race tests, HTTPS/chained redirects, alternate runtime and browser parity, revocation/grant/authorization paths for this direct IPv6 MCP server, and all other incomplete feature/role/platform areas. The positive tool remains drift-locked and ungranted; no security control was weakened.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
