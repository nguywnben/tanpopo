# QA observation — A2A task outcome during graceful Virtual Key revocation

**Checkpoint:** CP15  
**Status:** Observed; contract classification UNVERIFIED. This is not a confirmed defect and is not assigned a DEF ID.

A real A2A-only Virtual Key submitted a request to an isolated synthetic endpoint that paused after receiving the request. While the Console showed the task as Working, an Org Owner chose “Graceful drain (allow active streams to finish)” and revoked the key. The revoke returned 200, the key became REVOKED, and the Console reported 0 active streams severed. When the synthetic endpoint was released, the already-admitted A2A submission returned HTTP 400 `invalid_request` / `context canceled`; the A2A task appeared as Failed, its reservation was RELEASED_UNUSED, and settled cost was zero. The fixture recorded that it received the task request without caller authorization forwarded.

The documented WF-10 behavior and UI wording address in-flight streams, and the acceptance criteria discuss long-lived SSE. They do not say whether a synchronous A2A dispatch is expected to finish, cancel, or become recoverable when the caller key is gracefully revoked. The evidence therefore establishes behavior but not a contract violation.

**Why this matters:** the external agent received an operation, but the caller received a canceled error and Tanpopo records a terminal failure with the hold released. For agents that perform side effects, this can leave an ambiguous outcome and a caller may retry. This is an impact hypothesis; the synthetic fixture had no external side effect.

**Recommended engineering decision:** define graceful and emergency revocation semantics for already-admitted A2A tasks, including whether to drain/cancel the upstream request, how to represent ambiguous side effects, reservation settlement/release, audit evidence, and what the UI count measures. Add acceptance tests using a paused endpoint and concurrent task cancellation/revocation.

Reproduction and sanitized evidence are in `../checkpoints/CP15-a2a-graceful-revocation.md` and `../evidence/CP15-a2a-graceful-revocation.json`. UI screenshots were visually inspected. GATE-REL-07 remains BLOCKED.
