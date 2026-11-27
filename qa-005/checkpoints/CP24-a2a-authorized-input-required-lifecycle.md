# CP24 — A2A-only key, authorized discovery and input-required resume lifecycle

**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Binary:** local RC7-source build SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`  
**Runtime:** isolated SQLite online-backup clone at `127.0.0.1:18147`; `TANPOPO_A2A_ENABLED=true` only on this runtime. Main SQLite runtime stays at `127.0.0.1:18145` with the default A2A-disabled configuration.  
**Browser/fixtures:** Chromium/Playwright for Console setup and UI readback; local authorized synthetic A2A server on `127.0.0.1:19092`; no paid or real provider.

## Result: PASS (scoped A2A discovery and input-required resumption)

1. Confirmed the default runtime at `18145` returns HTTP 404 `resource_not_found` for `/a2a/agents` because the A2A plane is disabled by default. No source change was made. Created a transactional SQLite online-backup clone and started a second process using the documented runtime flag `TANPOPO_A2A_ENABLED=true`.
2. Through the real MCP Hub → A2A Agents UI, registered `qa005-a2a-input-required` with endpoint `http://127.0.0.1:19092/a2a`, explicit private/loopback consent, and `127.0.0.1/32`. UI and API showed an active project-scoped agent.
3. Through Access & Keys, issued an A2A-only Virtual Key with only `/a2a/*` routes and all models permitted. The one-time key is not included in screenshots or evidence. Its local secret was stored mode 0600 under `/tmp` for testing.
4. Cookie-free data-plane `GET /a2a/agents` with `Authorization: Bearer` discovered the authorized synthetic AgentCard. The key's attempt to call `/v1/chat/completions` returned HTTP 403 `scope_or_plane_denied` for the PROVIDER plane.
5. Cookie-free `POST /a2a/tasks` dispatched to the local mock and returned HTTP 201 with task state `input-required`, prompt “Please provide the invoice approval code.” Task readback showed `input-required`, version 3.
6. Cookie-free `POST /a2a/tasks/{id}/resume` with the requested approval input returned HTTP 200 `completed`, version 5, with the fixture artifact. Follow-up GET returned `completed`. Actual MCP Hub screenshot showed the active agent and completed task, 1/5 hops, US region, zero sub-budget hold/cost; screenshot visually inspected. The independent `tanpopo admin verify-state` run on the clone verified the full 188-event SQLite audit chain as valid with zero orphaned holds.

Revoked the one-time A2A key in both the original and clone QA databases through Access & Keys; the clone then rejected its A2A request with HTTP 401. The original runtime returned 404 because its A2A plane is disabled. Revocation screenshots visually confirm the key is masked and Revoked.

This confirms the implemented A2A discovery/authorization/task-resume path for one synthetic agent and key. The upstream mock accepted the input and returned a result but did not perform real business work. Cancellation, nonzero hold reservation/release, parent/child budget bounds, task failure/retry, region/model denial, hop/loop attacks, multi-tenant isolation, and real partner interoperability remain unverified. MCP protocol flows are still unverified. The A2A key is revoked in the test databases and the clone/mock are shut down at cleanup; ephemeral keys/DBs stay in `/tmp`.

Sanitized lifecycle readback: `evidence/CP24-a2a-input-required-lifecycle.json` and `evidence/CP24-a2a-key-revocation.json`. Visually inspected screenshots: `CP24-a2a-agent-registered.png`, `CP24-a2a-task-completed.png`. The default-disabled response was observed directly during setup; the alternate runtime configuration is separately recorded in `ENVIRONMENT-AND-ARTIFACT-IDENTITY.md`.
