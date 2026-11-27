# CP96 — DEF-026 MCP first-review sync status

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite runtime `127.0.0.1:18162`; Chromium 156; Owner `qa005-owner@example.invalid`; loopback-only MCP fixture `127.0.0.1:19098`.

## Result

**PASS (scoped fresh QA-005 retest of DEF-026's sync message/state consistency).** A new synthetic Streamable HTTP MCP server returned one read-only `qa_ready` tool. On first discovery, the real Console displayed “Synced 1 tools …; 1 tool(s) locked pending schema approval,” while the server and tool both showed `Schema Drift Locked` / `Drift locked`; API state returned HTTP 200, `synced_count=1`, `drifted_count=0`, version 1 and `drift_locked=true`. An unchanged resync preserved the same state and explanatory banner. After the loopback fixture's description changed, sync returned `drifted_count=1`, version 2 and `drift_locked=true`; the banner and rows remained consistent. The real first-review dialog exposed the observed JCS hash and risk-classification control. Pinning as `read_only` returned HTTP 200, `schema_drift_locked=false`; subsequent API readback showed the server `ACTIVE`, and the real UI showed `Pinned`.

No key was granted to the tool and no `tools/call` was sent. A first automation attempt clicked the older second server, whose historical loopback fixture was stopped; its expected upstream failure was a harness target-selection error and is excluded from the product result. The corrected run explicitly used the new fixture's first server row and verified successful `initialize`/`tools/list` responses.

## Reproduction

1. Register `QA005 DEF026 First Review Fixture` through the real MCP Hub Console with Streamable HTTP URL `http://127.0.0.1:19098/mcp`, explicit private/loopback consent, and CIDR `127.0.0.1/32`.
2. Select that server's **Sync tools** action. Confirm the first-discovery banner states one tool is locked pending schema approval, and compare the tool row with the HTTP 200 sync payload (`synced_count=1`, `drifted_count=0`, `drift_locked=true`, version 1).
3. Sync again with unchanged fixture schema. Confirm `drifted_count=0`, same hash/version/lock and consistent notice.
4. Change only the fixture's tool description and sync again. Confirm `drifted_count=1`, a changed JCS hash, version 2, locked state and matching explanatory banner.
5. Open **Approve schema**, verify the observed hash and `read_only` classification, then pin. Confirm HTTP 200, `schema_drift_locked=false`, version 3, `Pinned` in the UI and `ACTIVE` in the follow-up server list.

Visually inspected screenshots: [registered loopback fixture](../evidence/screenshots/CP96-def026-register-loopback.png), [first discovery](../evidence/screenshots/CP96-def026-initial-sync.png), [unchanged resync](../evidence/screenshots/CP96-def026-unchanged-resync.png), [schema drift](../evidence/screenshots/CP96-def026-schema-drift-sync.png), [first-review dialog](../evidence/screenshots/CP96-def026-approval-dialog.png), and [post-pin state](../evidence/screenshots/CP96-def026-first-pin-complete.png). Sanitized API/UI observations and limits: [CP96 evidence JSON](../evidence/CP96-def026-mcp-first-review-state.json).

## Limits and reconciliation

The original mismatch—zero changes presented as if no approval were needed while a new tool is locked—did not reproduce on exact RC7. This passes only the Owner/SQLite/Chromium first-discovery, unchanged-resync, drift and first-pin UX/API slice. A pre-pin key-grant attempt and MCP invocation denial were not performed, nor were Team Admin/other role, other backends, browser engines, pagination, multiple newly discovered tools or concurrent sync cases. Earlier QA-003 scoped fixed evidence is preserved but not used as proof for this fresh result. The synthetic MCP server and fixture remain in the isolated QA database/runtime; the test had no key grant and sent no tool invocation. No production source or specification changed; no paid/external provider was called. `GATE-REL-07` remains BLOCKED.
