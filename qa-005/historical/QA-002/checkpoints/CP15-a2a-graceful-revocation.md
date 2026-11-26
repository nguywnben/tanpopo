# CP15 — In-flight A2A task during graceful key revocation

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite server on `127.0.0.1:18084`; authorized synthetic endpoint on `127.0.0.1:19108`, allowlisted only as `127.0.0.1/32`. No Tanpopo implementation changed.

## Reproduction

1. Through the real Console, registered a synthetic US A2A agent at `http://127.0.0.1:19108`; creation returned 201. The endpoint pauses a request on a test-controlled gate and records whether caller authorization or trace context is forwarded.
2. Through Access & Keys, issued `qa002-drain-observe` with only the `A2A` plane and `/a2a/*` route. Creation returned 201; screenshot `../evidence/CP15-a2a-drain-key-persisted.png` was visually inspected and showed only the A2A grant.
3. Using that key, submitted an A2A task with a $0.005 test hold. Once the fixture confirmed the request had arrived and was paused, the real Console showed the task as Working (`../evidence/CP15-a2a-drain-in-flight.png`, visually inspected).
4. In Access & Keys, opened the key's revoke dialog. The displayed mode said “Graceful drain (allow active streams to finish)” and “blocks new requests while letting in-flight streams finish.” The dialog also said “Recent trace-derived traffic (last 1h): 0 requests.” Screenshot `../evidence/CP15-a2a-drain-revoke-dialog.png` was visually inspected.
5. Confirmed Revoke (graceful drain). Admin mutation returned 200 and the key state became REVOKED. The Console showed “Virtual key revoked. 0 active streams severed.” (`../evidence/CP15-a2a-drain-revoked.png`, visually inspected).
6. Released the loopback fixture. The already-admitted task request returned HTTP 400 `invalid_request`, message `context canceled`. The admin task list and actual A2A screen showed the persisted task as Failed (`../evidence/CP15-a2a-drain-final-task-state.png`, visually inspected).
7. Read-only SQLite state confirmed task version 3, zero settled cost, and the associated $0.005 reservation in `RELEASED_UNUSED`. The fixture recorded that it received the request without the caller Authorization header and with trace context. Audit rows included `virtual_key.revoked` and `a2a.task.transitioned` working→failed. The authenticated Security → Audit Log screen displayed the latest revoke/task events and was visually inspected (`../evidence/CP15-a2a-audit-log.png`). The real Console Verify hash chain action returned HTTP 200, `intact: true`, checked 122 events and reported no broken sequence.

Machine-readable sanitized evidence: `../evidence/CP15-a2a-graceful-revocation.json`. Key plaintext and the paused fixture's raw request are excluded.

## Result and classification

- **Authorization:** A2A-only key was accepted for authorized task submission; the caller credential was not forwarded to the endpoint. Revocation succeeded and transitioned the key to REVOKED.
- **Task lifecycle:** the task did not drain to completion after graceful key revocation. It became Failed and its unused hold was released. A second controlled attempt also persisted Failed, but its response body was not retained. An earlier harness attempt ended before reliable response capture and is excluded from the asserted reproduction.
- **Contract classification:** **UNRESOLVED / UNVERIFIED.** The UI promises that active *streams* finish, and the documented WF-10/acceptance language explicitly covers long-lived SSE streams. The tested synchronous A2A task request is not an SSE stream. Evidence establishes the behavior, but it does not establish whether this behavior violates the documented contract. Keep as a cross-feature contract/recovery question, not a confirmed defect. Engineering should define whether admitted A2A dispatches drain, fail, or transition to a recoverable state when their caller key is revoked, and how ambiguous upstream side effects are reported.

## Coverage updates and remaining work

- FT-08/SCR-10: A2A discovery and input-required/resume/cancel/completion remain partial-pass from CP08. New graceful-revocation interaction is observed but contract result is UNVERIFIED.
- WF-10: key revoke itself is verified; concurrent active SSE and this non-stream A2A dispatch behavior remain UNVERIFIED against a matching acceptance contract.
- New defect ID not assigned because requirements do not define this A2A interaction. This is retained as an explicit cross-feature observation.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
