# CP59 — Authorized MCP loopback lifecycle

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped) for Owner registration, sync, schema pin, one-tool grant, Streamable HTTP invocation, denial boundaries, persistence and revocation. Full MCP acceptance remains PARTIAL/UNVERIFIED.  
**Product source changes:** none.

## Real-user workflow

Using the real Chromium Console as the synthetic Org Owner, I registered `QA004 CP59 Loopback Echo` at an isolated synthetic endpoint `http://127.0.0.1:19444/mcp`. The Console private/loopback control was explicitly enabled and the permitted CIDR was narrowed to `127.0.0.1/32`. The endpoint is a QA fixture that implements only initialize, tools/list and a harmless echo tool; no public destination or production credential was used.

The first Sync tools attempt returned 502 before the fixture had been restarted. That attempt was not instrumented enough to attribute it to Tanpopo. With the same endpoint after fixture restart, sync returned HTTP 200, discovered one tool, and reported zero drift. The Console correctly left that tool locked until schema approval. After visually reviewing the read-only echo schema, I pinned hash `d56e138a634486dea1045c412f7e698313612804e5b696af9e61ca3e33761785` as `read_only` using the real approval form.

This project had no environment. I created `QA004 CP59 MCP Test` through the real workspace selector so the key remained environment-bound. The create response preserved the human name, but subsequent list readback and the active scope selector rendered `qa004-cp59-mcp-test` as the name. This independently reproduces existing **DEF-055**, not a new defect.

I issued `QA004 CP59 MCP-only Echo` through Access & Keys. Authenticated readback showed the key active, bound to the test environment, with `allowed_planes=[MCP]` and `allowed_mcp_tools=[qa004-cp59-loopback-echo__echo]`. I then selected the key in MCP Hub and used the UI Grant action; it returned 201. The one-time secret was stored only in a mode-0600 temporary file for test calls and is not included in evidence or screenshots.

## Data-plane results

Using only that Virtual Key (no Admin cookie):

- Unauthenticated `initialize` returned HTTP 401 `invalid_virtual_key`.
- Authenticated `initialize` returned HTTP 200 and a session ID; `notifications/initialized` returned HTTP 202.
- `tools/list` returned HTTP 200 and exactly the granted echo tool.
- `tools/call` returned HTTP 200 with `QA004 echo: QA004 CP59 authorized loopback round trip`.
- `GET /v1/models` returned HTTP 403 `scope_or_plane_denied`, identifying the missing PROVIDER plane.
- A `tools/call` for an ungranted tool returned JSON-RPC error `tool_access_denied`.

Afterward, I revoked the synthetic key using the real graceful-drain UI. The UI showed zero active streams severed, authenticated inventory readback showed `REVOKED` version 2, and replay of the key returned HTTP 401. This left the endpoint without an active test credential.

## Persistence, audit, visual inspection

A new authenticated session read the tool grant from `GET /admin/v1/mcp/tools/{toolId}/grants`; one row persisted with the exact project, environment, and key. Audit readback showed `mcp.tool_discovered` (#127), `mcp.tool_schema_approved` (#135), and `mcp.tool_granted` (#161). The real approval dialog, pinned schema/tool row, environment form and post-create picker, key review/list, grant review/result, refresh state, and revoke confirmation/result were captured. Screenshots were visually inspected during the workflow.

After a full page reload, the server and tool count remained, while the per-tool table did not render until Sync tools was clicked again. The persisted grant remained available through the grant API. This is recorded as a UX observation only; the tested product contract does not establish that the table must auto-hydrate without an explicit sync.

## Evidence

- [Sanitized lifecycle/API evidence](../evidence/CP59-mcp-authorized-lifecycle.json)
- [MCP tools after sync](../evidence/CP59-mcp-tools-synced.png)
- [Schema approval dialog](../evidence/CP59-mcp-schema-approval-dialog.png)
- [Pinned schema](../evidence/CP59-mcp-schema-approved.png)
- [Environment form](../evidence/CP59-mcp-environment-create-form.png)
- [Environment after create](../evidence/CP59-mcp-environment-created.png)
- [MCP-only key review](../evidence/CP59-mcp-vkey-review.png)
- [Persisted key inventory](../evidence/CP59-mcp-vkey-created.png)
- [Grant review and success](../evidence/CP59-mcp-grant-review.png), [grant success](../evidence/CP59-mcp-granted.png)
- [After authenticated-page reload](../evidence/CP59-mcp-after-refresh.png)
- [Revocation confirmation and persisted revoked state](../evidence/CP59-mcp-vkey-revoke-confirm.png), [revoked key](../evidence/CP59-mcp-vkey-revoked.png)

## Coverage limits / next work

This is one Owner/Chromium/local-loopback path. It does not establish other-role authorization, cross-project or cross-tenant isolation, remote HTTPS endpoint behavior, multi-node session handling, schema drift/reapproval, parallel calls, timeout/cancellation, audit-chain verification, or A2A behavior. Broader MCP/A2A, RBAC, SSRF, operational recovery, and remaining product work continue. `GATE-REL-07` remains **BLOCKED**. QA-004 is not complete.
