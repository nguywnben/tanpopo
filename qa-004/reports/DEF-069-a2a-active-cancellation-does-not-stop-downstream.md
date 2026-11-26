# DEF-069 — A2A active cancellation does not stop downstream work

**Severity:** Major  
**Status:** FAIL on frozen RC6  
**Baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Discovered:** QA-004 CP82  
**Affected workflows:** synchronous A2A dispatch, active task cancellation, downstream privacy and cost control, task recovery/retry

## Summary

Canceling a Working A2A task in the Console persists the task as Canceled and displays a success message, but the in-flight downstream request continues. When the downstream agent later returns `completed`, the original `POST /a2a/tasks` receives HTTP 400 for an invalid `canceled -> completed` transition. The task remains canceled, but external work has already continued after cancellation was reported successful.

## Reproduction

1. Start an A2A-enabled exact-RC6 isolated runtime and register the synthetic loopback agent already shown in the evidence. Its endpoint accepts the task, waits ten seconds, then responds HTTP 200 with `state: completed`. It records whether the response connection was interrupted.
2. In real Chromium, sign in as Org Owner and select `QA004 Test Root Org / default / dev`.
3. With the environment-bound A2A-only Virtual Key (`/a2a/*`), submit a synchronous task with one synthetic text input and `sub_budget_hold_micros: 1`. Use a cookie-free request.
4. While the task is visibly **Working**, click **Cancel** in MCP Hub → A2A Agents & Active Tasks.
5. Verify Cancel returned HTTP 200 and the task became `canceled`, version 3. Wait for the fixture response.
6. Observe fixture result `completed_response_sent`. The original create call returns HTTP 400 `invalid_request`, message `a2a: invalid SM-09 state transition: canceled -> completed`; task readback remains canceled.

The sanitized machine-readable results are [CP82 evidence](../evidence/CP82-a2a-active-cancellation-results.json). Visual evidence: [Working with Cancel available](../evidence/CP82-a2a-active-working-before-cancel.png) and [Canceled with success banner](../evidence/CP82-a2a-active-working-after-cancel.png). Both screenshots were visually inspected. The synthetic endpoint call had one text input, no forwarded caller Authorization header, and zero settled cost. No provider credential, external endpoint or real user data was used.

## Independent CP108 PostgreSQL retest

Repeated the active-cancel flow on the separate PostgreSQL 16-backed exact-RC6 runtime with A2A explicitly enabled. A loopback endpoint with a 10-second delay received an authorized, environment-bound A2A-only request without caller Authorization. The real Console displayed Working and Cancel; Cancel returned 200 and API readback persisted Canceled v3. The fixture nevertheless sent HTTP 200 `completed_response_sent`, after which the original create call returned HTTP 400 `canceled -> completed`; final state remained Canceled v3. The before/after screenshots were visually inspected. See [CP108](../checkpoints/CP108-def069-postgres-runtime-retest.md) and its sanitized [result JSON](../evidence/CP108-a2a-postgres-cancel-results.json). This independently confirms DEF-069 on PostgreSQL; no new defect ID was assigned.

## Expected versus actual

- **Expected:** the documented SM-09 workflow says `working → canceled` propagates the cancellation signal downstream. The cancel caller should receive a stable canceled result, downstream execution should stop, and any late completion should not turn the original create into an unrelated invalid-transition error.
- **Actual:** the Console returns success and Tanpopo persists Canceled v3, but the downstream endpoint completes normally. The pending create receives HTTP 400 for `canceled -> completed` after the fact.

## Business impact and severity

Major severity is based on the user-visible cancel guarantee, not on spend measured in this synthetic test. In production, an agent may continue processing sensitive input or consuming its own billable resources after the operator believes work stopped. The original caller may retry after receiving a transition error, producing duplicate downstream work.

## Facts and hypothesis

**Facts:** Cancel HTTP 200; task state Canceled v3; the synthetic endpoint's delayed success response was delivered without a broken connection; original create returned HTTP 400 for `canceled -> completed`; final task state remained canceled with zero settled cost.

**Root-cause hypothesis:** the cancellation path updates persisted task/ledger state but does not propagate a cancellation mechanism to the already active request; the synchronous dispatch later attempts to apply its result against terminal state. This requires engineering confirmation.

## Acceptance criteria

1. Active cancellation terminates or signals the in-flight downstream operation, and a deterministic test endpoint confirms receipt before its delayed completion.
2. Cancel returns a correlatable task representation; the pending create request resolves to a documented cancellation outcome rather than a generic transition error.
3. A late response cannot overwrite or contradict the terminal canceled state.
4. Reservation release, settlement, trace, and audit behavior are correct under repeated cancel and completion/cancel races.
5. Regression coverage includes Console/API actors, `working` and `input-required`, slow/error/disconnected endpoints, nonzero spend, and distributed PostgreSQL/Valkey runtime.

## Root-cause validation request

Trace cancellation through the live dispatch lifecycle and verify how the outbound request or downstream A2A cancellation message is interrupted. The report does not prescribe an implementation mechanism.
