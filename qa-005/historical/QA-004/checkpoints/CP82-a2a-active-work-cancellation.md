# CP82 — A2A active-work cancellation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** A2A-enabled isolated RC6 clone at `127.0.0.1:18128`  
**Role/browser:** synthetic Org Owner, real Chromium, 1440×1000  
**Scope:** `QA004 Test Root Org / default / dev`  
**Product source modified:** no

## Outcome

- **FAIL — DEF-069 (Major): active A2A task cancellation does not stop downstream work.** A real UI cancellation of a synchronous task in `working` returned HTTP 200 and persisted `canceled` v3 with zero settled cost. The synthetic downstream agent continued processing the request for ten seconds and successfully sent its `completed` response. Tanpopo then returned HTTP 400 to the still-pending create request: `invalid SM-09 state transition: canceled -> completed`. The task remained canceled, but the endpoint had completed work after the user was told it was canceled.
- **PASS (scoped): Console cancellation state transition.** The Console exposed Cancel on the live Working row, showed its success banner, and removed the action once task state became Canceled. This does not resolve downstream cancellation.
- **Harness correction:** the first probe accidentally included the Admin session cookie with the Virtual Key and was correctly rejected before dispatch (`401 invalid_credential_plane`). A later scope-picker check found the UI had returned to the canary after a hard navigation. The final run used `credentials: omit`, explicitly selected `default/dev`, verified the active scope, and navigated within the Console so the task row was fresh. Neither harness issue is a product finding.
- **Fixture cleanup:** restored the original CP10 input-required/completed synthetic endpoint on port 18127 and verified its supported POST response returned HTTP 200 `input-required`. The delayed cancellation fixture remained outside the repository in `/tmp`.

## Reproduction

1. Run the exact RC6 clone with A2A enabled and the established synthetic `qa004-a2a-loopback` agent. Point only that loopback fixture at a controlled ten-second delay followed by HTTP 200 `state: completed`; the fixture accepts no public egress and records whether its response write is interrupted.
2. Sign in as the isolated Org Owner and select `QA004 Test Root Org / default / dev`.
3. Using the existing A2A-only Virtual Key with `/a2a/*`, submit a synchronous `POST /a2a/tasks` to the agent with one synthetic text input and a 1-micro-USD sub-budget cap. Omit Admin cookies from the data-plane request.
4. While the task appears as Working in the real MCP Hub → A2A Agents & Active Tasks Console, click its Cancel action.
5. Observe HTTP 200 for cancellation and the visible “Canceled A2A task and released held budget” message. Read back the task as `canceled`, version 3, settled cost zero.
6. Wait for the endpoint to finish its ten-second request. The fixture records `completed_response_sent`, proving the downstream response was delivered. The original create call returns HTTP 400 `a2a: invalid SM-09 state transition: canceled -> completed`; the task remains canceled.

Sanitized result details are in [CP82-a2a-active-cancellation-results.json](../evidence/CP82-a2a-active-cancellation-results.json). The actual Working task and available Cancel action are shown in [the before screenshot](../evidence/CP82-a2a-active-working-before-cancel.png); [the post-cancel screenshot](../evidence/CP82-a2a-active-working-after-cancel.png) was visually inspected and shows the Canceled state and success banner. Fixture implementation and synthetic inputs remained in `/tmp` and are excluded from the handoff; fixture outcome is included in the sanitized JSON.

## Expected versus actual

- **Expected:** per SM-09, caller cancellation of a Working task propagates a cancellation signal to the downstream agent, releases the remaining budget hold, and prevents a late successful response from changing the caller-visible outcome to an error.
- **Actual:** Tanpopo records the task as Canceled and returns success from the cancel action, but the in-flight downstream request continues to completion. The original task-create request then fails with a transition error even though the task remains Canceled.

## Impact and severity

**Major.** Users and operators can believe an in-flight delegated task has stopped while the external agent continues processing supplied input and consuming its own resources. The create caller also receives an error after a separate operator successfully canceled the task. This can create duplicate retries, unexpected third-party work or cost, and uncertainty about whether sensitive input is still being processed. The test used only synthetic input and zero-cost loopback infrastructure; production spend or disclosure is not claimed.

## Facts and root-cause hypothesis

**Observed facts:** Console cancellation returned 200; task state became Canceled v3; the downstream fixture received the original request, did not observe an interrupted connection, and successfully delivered its delayed completion response; the original create request returned a 400 transition error; durable final task state remained canceled.

**Hypothesis:** cancellation transitions database/ledger state without retaining or invoking a cancellation handle for the in-flight HTTP dispatch. The synchronous create path later applies the now-stale downstream completion against terminal Canceled state. This is consistent with the evidence but should be confirmed by engineering tracing before implementation changes.

## Acceptance criteria

1. Canceling a Working A2A task signals the active downstream operation through the supported protocol or aborts its active transport promptly; a deterministic test endpoint observes cancellation before its completion response.
2. The cancellation API/Console returns a stable canceled task representation and the original create caller receives an unambiguous cancellation outcome containing or otherwise correlating the task ID.
3. A late downstream completion cannot produce a contradictory transition error or overwrite the terminal task outcome.
4. Sub-budget hold release, settled cost, trace linkage and audit events remain correct and idempotent for cancellation races before, during, and after downstream completion.
5. Tests cover both Console and Virtual Key cancellation, `working` and `input-required` states, timeout/disconnect races, retries, and PostgreSQL/Valkey distributed execution.

## Coverage gaps

Only an Org Owner Console cancellation of one synchronous HTTP-backed task in a local SQLite RC6 clone was exercised. Other roles, direct Virtual Key cancel, async worker dispatch, streaming/event-based agents, upstream cancellation protocols, nonzero spend, concurrent complete/cancel races, restart recovery, PostgreSQL/Valkey, and non-loopback endpoints remain unverified. CP10's earlier `input-required → resume → completed` flow remains a scoped QA-004 result; CP82 does not repeat or expand that result.

**Campaign status:** 23 historical findings have scoped PASS, 6 are PARTIAL, DEF-025 remains FAIL, and 20 are UNVERIFIED. DEF-051–069 are 19 confirmed new failures. QA continues; **GATE-REL-07 remains BLOCKED.**
