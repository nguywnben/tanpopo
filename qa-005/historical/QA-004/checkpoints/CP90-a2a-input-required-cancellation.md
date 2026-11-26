# CP90 — A2A input-required cancellation and credential boundary

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) — authorized loopback A2A task reached `input-required`, was canceled in the real Console, persisted, rejected later resume, and had a truthful audit trail. No new defect assigned.
**Gate:** GATE-REL-07 remains BLOCKED. Product source unchanged.

## Setup and authorization

In the A2A-enabled isolated runtime `127.0.0.1:18128`, an Org Owner registered `qa004-cp90-inputcancel` through the real MCP Hub UI, pointed only at synthetic loopback `127.0.0.1:18135`, and allowed only `127.0.0.1/32`. The local fixture returned an `input-required` prompt and recorded only request path, input-part count and whether an Authorization header arrived.

A new environment-bound Virtual Key was issued through Access & Keys and its final grants were read back as `allowed_planes=[A2A]`, `allowed_routes=[/a2a/*]`. An initial test key retained the default PROVIDER plane when A2A was added; it was revoked through the UI before any data-plane request. The final key was used only with cookie-free requests (`credentials: omit`). A deliberate `/v1/models` call returned 403 `scope_or_plane_denied`. An earlier request that included the Admin session cookie returned 401 `invalid_credential_plane`, confirming the A2A cookie boundary; it was not a product failure.

Evidence: [agent registration](../evidence/CP90-a2a-agent.json), visually inspected [agent row](../evidence/CP90-a2a-agent-registered.png), [final A2A-only key grants](../evidence/CP90-a2a-key.json), and [authorization probes](../evidence/CP90-a2a-input-required-results.json).

## Input-required → canceled journey

The A2A-only key discovered the project agent. A synchronous task create returned HTTP 201 and `input-required` with prompt `Provide the report period to continue.` The persisted data-plane readback was HTTP 200, state `input-required`, version 3. The synthetic agent received exactly one `/tasks` call and no caller Authorization header.

The open Console initially retained its existing task list after the separate data-plane request. A full page reload displayed the newly persisted task at the top with the `Input required` state, prompt, and both Resume and Cancel actions; this initial-staleness/refresh observation was not classified as a defect because the product contract reviewed does not promise live server push. The screenshot was visually inspected: [before refresh](../evidence/CP90-a2a-before-refresh.png) and [input-required after refresh](../evidence/CP90-a2a-input-required-refresh.png).

I clicked the actual Cancel action. The UI showed “Canceled A2A task and released held budget.” Subsequent cookie-free task readback returned state `canceled`, version 4, zero hold and zero settled cost; the refreshed UI retained the Canceled row with no remaining action. A caller-key resume replay returned 409 `task_not_resumable`. The fixture call count remained exactly one, so canceled work did not dispatch again. Visually inspected [canceled task](../evidence/CP90-a2a-input-required-canceled.png).

## Audit, key cleanup and visual evidence

Audit events #486–489 record submitted → working → input-required → canceled for the same task. The real Security → Audit Log filter displayed the cancellation event; Verify hash chain reported the RFC 8785 JCS chain intact across 491 events after the test-key lifecycle entries. The final A2A-only key was gracefully revoked (state `REVOKED` v3) and replay returned 401; the setup-only mixed-plane key was also revoked. The local fixture was stopped and the one-time secret files were deleted.

Evidence: [task and replay readback](../evidence/CP90-a2a-input-required-results.json), [task transitions and final chain](../evidence/CP90-a2a-audit.json), [final key event reconciliation](../evidence/CP90-a2a-audit-final.json), visually inspected [Audit Log chain](../evidence/CP90-a2a-cancel-audit-chain-final.png), [key revocation readback](../evidence/CP90-a2a-key-revocation.json), and [revoked-key screenshot](../evidence/CP90-a2a-key-revoked.png).

## Coverage boundary

This passes one isolated Owner/loopback, zero-hold, synchronous A2A input-required cancellation flow. It does not resolve DEF-067 synchronous `working` responses or DEF-069 active downstream cancellation. Nonzero sub-budget release, async worker dispatch, failure/timeout/retry, cross-project/tenant/role isolation, other transports, distributed cancellation, and broader SSRF remain UNVERIFIED. The initial key form mistake was detected in inventory and corrected before use; it is not a product finding. QA continues; **GATE-REL-07 remains BLOCKED**.
