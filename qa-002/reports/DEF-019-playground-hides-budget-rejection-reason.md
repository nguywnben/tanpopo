# DEF-019 — Playground hides the reason for a project-budget 429

**Severity:** Moderate  
**Status:** Confirmed on exact QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

When a Playground request is rejected because the project budget is exhausted, the UI shows only `Playground inference failed (429).` The persisted request trace identifies the reason as `project_budget_exceeded`. The local provider fixture records no request for the rejected call. The UI therefore hides the action-relevant distinction between a budget cap, rate limit, credential failure, or other HTTP 429.

## Reproduction

1. Configure a project budget for the active synthetic provider project, set its hard limit to 1 micro-USD, and submit one Playground request through route `qa002-cp22-route-repro`. The request returns 200, is observed at the loopback provider fixture, and settles exactly 1 micro-USD into the project ledger.
2. Submit a second Playground request at the exhausted cap. The UI shows a red banner `Playground inference failed (429).` Screenshot `../evidence/CP26-playground-over-cap-result.png` was visually inspected.
3. The synthetic provider fixture request count remains unchanged at one for the first two attempts (`../evidence/CP26-playground-over-cap.json`). Read-only SQLite stores the failed request as HTTP 429, `canonical_error_code=project_budget_exceeded`, no selected deployment and no failover attempt (`../evidence/CP26-budget-enforcement-recovery-state.json`).
4. Raise the cap to 2 micro-USD through FinOps. A new Playground request returns 200, the fixture receives a second request, and the second micro settles. This recovery control shows the route/provider is usable when the budget permits it (`../evidence/CP26-playground-recovery.json`, `CP26-budget-enforcement-recovery-state.json`).

## Expected versus actual

- **Expected:** display the safe canonical rejection reason (for example, `project_budget_exceeded`) and a useful action such as checking the project budget or contacting its owner. Keep credentials, provider secrets and sensitive diagnostics hidden.
- **Actual:** the UI displays only the HTTP status. It does not tell the Playground user why the request was rejected or how to recover, even though the persisted trace records the canonical error.

## Impact

Developers cannot distinguish a deliberate governance cap from transient throttling or provider failure using the Playground result. They may retry a request that cannot pass or route an incident to the wrong owner. In this case the FinOps owner can see the exhausted cap on another screen, but Playground does not connect the failure to that project budget.

## Facts and root-cause hypothesis

**Facts:** UI error text is `Playground inference failed (429).`; the request trace is `project_budget_exceeded`, with `selected_deployment_id=null`; the provider fixture count did not increase; raising the project cap restored success.

**Hypothesis:** Playground handles non-2xx responses by preserving only the numeric status and discarding the canonical error envelope. This source-level explanation is a hypothesis; the live UI behavior and saved trace are the defect evidence.

## Acceptance criteria

- For a budget rejection, show the canonical error code and explain which budget scope blocked dispatch when safely available; provide a clear route to the FinOps page or relevant admin contact.
- Preserve generic safe fallbacks for unknown errors without exposing secrets or raw internal diagnostics.
- Distinguish project/key caps, rate limits, provider 429s, authentication failures and unavailable routes in the Playground result state.
- Verify both keyboard/screen-reader announcement and visible styling; verify no upstream request is sent for a pre-dispatch budget rejection and that service recovers after an authorized cap increase.

## Evidence

- `../evidence/CP26-playground-over-cap-result.png` — visually inspected generic 429 banner.
- `../evidence/CP26-playground-over-cap.json` — HTTP response status and unchanged synthetic fixture call count.
- `../evidence/CP26-budget-enforcement-recovery-state.json` — persistent trace reason, null deployment selection, and subsequent successful exact settlement.
- `../evidence/CP26-playground-recovery-result.png` — visually inspected restored successful response after cap increase.

QA-002 remains in progress; no release approval is implied.
