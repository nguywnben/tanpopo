# CP109 — DEF-011 input-required resume on PostgreSQL

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Result:** PASS (scoped). Freshly revalidates historical DEF-011 on PostgreSQL; earlier results were not inherited.

## Real user journey

On A2A-enabled PostgreSQL runtime 18133, issued a real Virtual Key restricted to `A2A` and `/a2a/*` in the selected test environment. A local synthetic endpoint returned `input-required` on its first task request and `completed` with a synthetic artifact on the resumed request. Both endpoint requests reported no forwarded caller Authorization header.

The cookie-free task create returned HTTP 201, `state: input-required`, task ID `tsk_01m8axnh12e008rawg2j6demgz`, and the synthetic prompt. The real Console showed **Input required**, the prompt, and Resume/Cancel actions. I opened Resume, entered `Use reporting period 2026-Q4.`, and submitted **Send & resume**. The resumed request returned HTTP 200 with `state: completed` and one artifact; list readback persisted Completed v5, and the real Console displayed the success banner with no remaining task action.

After readback, the real Access & Keys UI revoked the test key. A cookie-free `/a2a/agents` replay returned 401 `invalid_virtual_key`; the metadata row remained REVOKED with only the A2A plane and `/a2a/*` grant. No replay reached the synthetic agent.

Security → Audit Log showed the targeted task lifecycle events; the Console verified its RFC 8785 audit chain intact across 53 events. The task-submitted, state-transition, resume, and cancellation events for the synthetic tasks remained visible in persistent readback.

This confirms the previously scoped PASS for historical DEF-011 on a second backend. It does not cover async task events, retry/timeout, cancellation races, Team Admin access, nonzero settlement, or a distributed runtime. DEF-069 independently remains FAIL (CP108); the two findings cover separate lifecycle stages.

## Evidence

- Visually inspected [Input required task with Resume/Cancel](../evidence/CP109-a2a-input-required-ui.png), [follow-up form](../evidence/CP109-a2a-resume-form.png), and [completed task](../evidence/CP109-a2a-resumed-completed-ui.png).
- [Create/readback and synthetic endpoint evidence](../evidence/CP109-a2a-input-required.json); [resume response, persisted task and fixture records](../evidence/CP109-a2a-resume-results.json); [task audit events and verified chain](../evidence/CP109-a2a-audit.json); [key revocation and denied replay](../evidence/CP109-a2a-key-revocation.json) with visually inspected [revoked-key UI](../evidence/CP109-a2a-key-revoked.png).

No product implementation was changed. QA continues; **GATE-REL-07 remains BLOCKED.**
