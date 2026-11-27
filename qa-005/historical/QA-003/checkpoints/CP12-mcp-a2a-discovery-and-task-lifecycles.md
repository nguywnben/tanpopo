# CP12 — MCP discovery, schema controls, and A2A task lifecycle

## Scope and environment

Continued on the exact RC5 source baseline `3423d4f523ef0ac77469a1201b8b753d1d31c902`. For end-to-end data-plane coverage, a transactionally backed-up copy of the local SQLite QA database was started as a fourth isolated runtime on `127.0.0.1:18107` with `TANPOPO_A2A_ENABLED=true`; the three pre-existing runtimes remained unchanged and continued to report A2A disabled. Loopback MCP and A2A endpoint fixtures ran on ports 19191 and 19190. No production endpoints or provider credentials were used.

## A2A Virtual Key, discovery, and lifecycle

The real Access & Keys Console issued a Virtual Key with A2A selected and route `/a2a/*`. The persisted key state contains `A2A` and the restricted route; the UI's default `PROVIDER` selection also remained selected. A browser fetch with credentials explicitly omitted reached `/a2a/agents` and returned HTTP 200 with the registered synthetic ON_PREM agent. The key's one-time secret was kept only in a mode-0600 file under `/tmp`; it is absent from evidence.

With a separate synthetic A2A-plane key, discovery returned the same registered authorized agent. A request with a modified invalid bearer returned 401. An MCP-only Virtual Key calling `/a2a/agents` returned 403 `scope_or_plane_denied`, preserving the plane boundary. A same-origin browser request that included the logged-in Admin session cookie was rejected with `invalid_credential_plane`; retry with `credentials: "omit"` succeeded. This is the explicit security contract (Admin cookies are prohibited on A2A endpoints), not a defect.

The data-plane task POST reached the isolated agent and returned `input-required`, a synthetic prompt, and a 10,000-micro reservation. The Console listed the prompt and exposed Resume and Cancel controls. A real Resume dialog accepted follow-up text; the endpoint returned Completed with one synthetic artifact and 1,234 micro cost. The response and Console row showed Completed and `$0.001234`; a separate GET readback matched state, version, and settlement. Repeating resume after completion returned HTTP 409 `task_not_resumable`. Two other pending synthetic tasks were canceled through the Console; both returned 200, became Canceled, and the UI reported the budget hold released.

The first UI resume submission returned to `input-required` because the initial fixture recognized only a `decision` field while the Console sends a text part. The fixture was adjusted to recognize the actual UI text; the subsequent real UI resume completed. This first attempt was a harness contract mismatch, not a Tanpopo defect. The intermediate evidence is retained for traceability.

Visually inspected screens: [A2A input-required task](../evidence/CP12-a2a-input-required-ui.png), [Resume dialog](../evidence/CP12-a2a-resume-dialog.png), [successful resumed task](../evidence/CP12-a2a-ui-resumed.png), and [cancelled task recovery](../evidence/CP12-a2a-cancelled.png). Structured evidence: [API lifecycle](../evidence/CP12-a2a-lifecycle.json), [UI resume](../evidence/CP12-a2a-ui-resume-result.json), and [synthetic upstream calls](../evidence/CP12-a2a-upstream-calls.json).

## MCP discovery, schema approval, grants, and invocation

Registered a synthetic Streamable HTTP server with exact loopback CIDR `/32`. The backing SQLite record confirms the CIDR persisted. The server's Streamable HTTP initialize/notification/tools-list flow returned one synthetic read-only tool. Initial sync returned HTTP 200, `synced_count=1`, `drifted_count=0`, and `drift_locked=true`; the Console explicitly said approval was pending. The initial RFC 8785 JCS schema hash was pinned as `read_only`; SQLite readback showed the approved and observed hashes equal and the lock cleared. A project/environment-bound MCP Virtual Key was granted to the tool (HTTP 201).

MCP handshake succeeded (initialize 200, initialized notification 202), `tools/list` exposed the granted tool, and `tools/call` returned the synthetic invoice result. A call to an ungranted tool returned JSON-RPC `tool_access_denied`. An A2A-key attempt on MCP returned 401 `invalid_virtual_key`.

The fixture schema was then changed by adding an optional field. A subsequent sync reported one drifted tool and re-locked it. A post-drift call returned JSON-RPC `schema_hash_drift_blocked`. The Console displayed “Schema Drift Locked” and “Drift locked.” This confirms the tested initial pin, grant, read-only invocation, permission denial, and fail-closed drift behavior. Write-risk tools, HITL approval completion, secret broker injection, and concurrent/session-expiry behavior remain open.

Visually inspected screens: [server before sync](../evidence/CP12-mcp-server-before-sync.png), [synced and locked tool](../evidence/CP12-mcp-sync-result.png), [schema approval review](../evidence/CP12-mcp-approval-dialog.png), [pinned tool](../evidence/CP12-mcp-schema-approved.png), [granted key](../evidence/CP12-mcp-granted.png), and [drift re-lock](../evidence/CP12-mcp-schema-drift.png). Structured evidence: [sync result](../evidence/CP12-mcp-sync-result.json), [approval](../evidence/CP12-mcp-approval-result.json), [grant](../evidence/CP12-mcp-grant-result.json), [invocation and denials](../evidence/CP12-mcp-invocation.json), [drift call](../evidence/CP12-mcp-schema-drift.json), and [persistent state](../evidence/CP12-persistent-state.json).

## Historical defect updates

- **DEF-010 — VERIFIED FIXED for tested Console provisioning and A2A discovery.** The Console can issue a key including A2A and the persisted scope includes A2A; a bearer key with `/a2a/*` successfully discovers the authorized agent. Other role provisioning, key rotation/revocation, and broader cross-project boundaries are not covered.
- **DEF-011 — VERIFIED FIXED for tested input-required resume lifecycle.** Real upstream input-required response displayed in Console, user follow-up resumed it through the UI, settlement persisted, and post-completion duplicate resume returned 409. Other upstream failures and multi-hop lifecycles remain open.
- **DEF-026 — VERIFIED FIXED for initial sync.** The first newly discovered tool is reported as pending approval and remains drift locked until the schema hash is reviewed and pinned. The subsequent schema drift also re-locks execution.
- **DEF-005 — PARTIAL.** Exact loopback `/32` CIDR persistence is now confirmed for both MCP and A2A in the isolated runtime. Broader connection enforcement, dual-stack/private-public mixed answers, DNS rebinding and connection races, redirect transitions, and mobile errors remain unverified.

## Coverage changes

FT-07, FT-08, FT-11, SCR-10, WF-05, and WF-09 are **PARTIAL** with the tested scope above. Chromium desktop remains **PARTIAL**. This checkpoint did not exercise cross-project roles, multi-hop delegation, A2A SSE, the kill switch, rate/budget exhaustion, HITL, or other database backends.

## Sanitization and gate

Evidence is limited to synthetic payloads, safe IDs, hashes, result states, and endpoint call summaries. Virtual Key secrets, admin credentials, SQLite files/WAL, and service logs are not included. QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
