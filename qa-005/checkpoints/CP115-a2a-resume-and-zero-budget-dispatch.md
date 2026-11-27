# CP115 — A2A input-required resumption and zero-hold dispatch

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
**Environment:** isolated SQLite clone, `127.0.0.1:18166`, `TANPOPO_A2A_ENABLED=true`; local synthetic A2A fixture `127.0.0.1:19188`; Org Owner; real Chromium 156 Console. No external/paid provider use.
**Disposition:** DEF-011 PASS (scoped, state-sequence acceptance now observed); candidate new DEF-085 FAIL (budget-control / dispatch order). GATE-REL-07 remains BLOCKED.

## DEF-011 — actual delayed input-required lifecycle

A QA-only A2A endpoint was registered through the Console after explicit loopback consent and `/32` restriction. An A2A-only Virtual Key was issued. The synthetic endpoint first returned `input-required`, then held the resume request for 12 seconds before returning a result.

The real UI showed the task’s requested input, a required follow-up field, and Resume/Cancel. During the delay, the open modal showed a pending spinner; after the request, a success banner and the same task ID appeared as Completed with one artifact. These screenshots were visually inspected: `CP115-a2a-input-required-ui.png`, `CP115-a2a-resume-dialog.png`, `CP115-a2a-resume-filled.png`, `CP115-a2a-resume-inflight.png`, `CP115-a2a-task-completed.png`.

During the live resume, authorized GET polling observed the same task at `input-required` v3 (+18ms), then `working` v4 (+327ms); resume returned HTTP 200 after 12,327ms. Final GET returned `completed` v5, the same trace root, a persisted `qa115-result` artifact, a $0.002000 settlement against a $0.010000 hold. SQLite readback retained the same completed task. Audit rows include `submitted` → `working` → `input-required` → resumed `working` → `completed`; JCS chain verification passed for both runtime organizations (5 and 691 events). This satisfies the previously missing intermediate-state evidence and changes DEF-011 from PARTIAL to PASS (scoped). It does not establish cancellation, error/retry, multi-hop, alternate roles, PostgreSQL/Valkey in this run, or partner interoperability; prior QA-001/QA-004 findings remain historical.

## DEF-085 — zero-hold task dispatch reaches downstream before rejection

**Severity:** Major. **Status:** FAIL (fresh exact-RC7 reproduction).
**Contract:** `docs/specifications/mcp-a2a-boundaries.md` §3.2 says the sub-budget hold is held atomically prior to dispatch and breach is rejected. `docs/planning/units/TP-IMP-0042.md` AC-2 requires exact consumption settled against allocated sub-budget. Frozen unit test `internal/ingress/a2a/a2a_test.go` explicitly expects a positive spend on a zero-hold task to be rejected at transition time; this compatible implementation test does not assert that downstream dispatch is suppressed.

**Reproduction:** Through the real Console, register/authorize the synthetic local agent and provision an A2A-only key. Send two synchronous A2A task requests with `sub_budget_hold_micros=0`. The fixture records each task ID and returns a deterministic `input-required` response reporting 1,000 synthetic micro-USD spent. Only after dispatch does Tanpopo return HTTP 403 `budget_or_privilege_exceeded`; clone DB readback shows both task rows `FAILED`, cap 0, settlement 0, no reservation. The fixture recorded two calls and no caller Authorization header was forwarded. Audit rows show each task was submitted and moved to working, then failed; there was no budget reservation.

**Expected:** establish a usable budget allocation before an external action is dispatched, or reject a request that cannot authorize downstream spending without contacting the agent. Any supported zero-cost mode must enforce an explicit no-spend contract before completion and must not turn a post-dispatch budget rejection into an apparently safe no-op.

**Actual:** downstream synthetic work occurred before spend validation rejected the response. The test did not incur provider charges and cannot prove an actual third-party charge; it demonstrates a dispatch-order condition under which an external agent could perform billable or mutating work while Tanpopo records a failed task and zero internal settlement.

**Business impact:** potential unbudgeted side effects/cost and misleading failed-task state. The severity reflects the dispatch-before-budget-check invariant, not observed paid loss. No authorization boundary was weakened.

**Root-cause hypothesis (unconfirmed):** synchronous dispatch accepts a zero sub-budget, then the terminal transition rejects the endpoint-reported positive spend after the endpoint has already run. This observed control flow is consistent with frozen source structure and runtime behavior; an engineering diagnosis remains necessary.

**Acceptance criteria:** (1) reject no-budget work before any outbound call when positive spend cannot be authorized; (2) persist an explicit safe rejection and audit reason; (3) deterministic fixture call count stays zero for the rejected case; (4) nonzero held tasks can complete and settle exact usage no greater than the hold; (5) cover synchronous, asynchronous, resumed, parent/child and cancellation paths without weakening A2A plane/RBAC/residency/hop controls.

## Cleanup and integrity

The temporary A2A key was revoked through the authenticated Admin UI/API, readback was `REVOKED` v2, and subsequent replay returned HTTP 401. Audit verification reported intact chains for both organizations. Test service and fixture were stopped. Sanitized results and task audit rows are in [CP115 evidence](../evidence/CP115-a2a-lifecycle-zero-hold.json). No credential secret is included.

## Limits

SQLite/Owner/Chromium and one local synthetic endpoint only. No PostgreSQL/Valkey cross-backend confirmation of DEF-085, no provider interoperability or actual external A2A agent, no paid usage, and no Windows/macOS or physical-mobile coverage. QA-005 remains ongoing; TEEP-01 readiness is NOT DETERMINED.
