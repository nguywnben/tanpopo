# DEF-011 — A2A `input-required` tasks have no resume path

## Impact

A valid delegated task can enter the documented `input-required` SM-09 state but the caller has no exposed route to submit its answer. It remains nonterminal and cannot finish. Multi-turn agent workflows are therefore unusable after the first clarification request. This blocks conformance with the published state machine and TP-IMP-0042 AC-2.

## Reproduction

1. Start an isolated server with `TANPOPO_A2A_ENABLED=true` and register an authorized loopback synthetic endpoint.
2. Have the endpoint accept `POST /tasks` and reply `{"state":"input-required"}`.
3. With an authorized A2A Virtual Key, call `POST /a2a/tasks` with the target agent ID, `sub_budget_hold_micros: 0`, and one text input part.
4. Observe HTTP 201 and state `input-required`; GET `/a2a/tasks/{taskId}` confirms the persisted state.
5. Attempt to resume with `POST /a2a/tasks/{taskId}/transition` and body `{ "state": "working", "input_parts": [{"type":"text","text":"answer"}] }`.
6. Observe HTTP 404 `resource_not_found`, message `A2A endpoint not found.` The Console task list has no resume action or prompt.

## Expected behavior

Provide a documented, authenticated resume operation that supplies caller input, performs `input-required -> working`, dispatches the task again to the registered endpoint, and persists subsequent state/artifacts/cost. Preserve task ownership, tenant/project boundaries, hop and budget limits, trace continuity, cancellation, and auditability. The Console should communicate the requested input and let an authorized caller resume or cancel.

## Actual behavior

The mounted HTTP handler accepts GET for a task and POST to its `/cancel` suffix; all other task subroutes return 404. `TaskTransitionRequest` and internal transition logic exist, but cannot be reached externally. No input prompt is present in the upstream response type/visible row. A normal user cannot resume through the API or Console.

## Evidence and classification

- Reproduction: `../evidence/phase41-a2a-input-required.json`
- Visual: `../evidence/phase41-a2a-input-required-console.png`
- Successful neighboring paths: `../evidence/phase39-a2a-lifecycle.json` (completion, GET, SSE, async submit/cancel, hop rejection, bearer stripping)
- Contract: `docs/specifications/mcp-a2a-boundaries.md` §3.1; TP-FR-0405; TP-IMP-0042 AC-2.
- Severity: Major, because the advertised multi-turn state machine gets stuck after a legitimate agent response.
- Test setup used only an isolated SQLite QA database and a mock bound to 127.0.0.1. No production code or external endpoint was changed.

## Suggested engineering checks

Add an explicit OpenAPI operation and handler for task resumption. Verify same-key task ownership and cross-tenant non-disclosure, accepted state transitions, repeated input, resume dispatch failure, cancellation race, exact budget settlement/release, trace propagation, SSE event order, audit events, and Console accessibility. Retain the existing default-deny behavior for Virtual Keys lacking the A2A plane (DEF-010).
