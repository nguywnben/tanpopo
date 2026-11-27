# CP59 — DEF-011 A2A input-required continuation evidence reconciliation

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
**Disposition:** **PARTIAL** for historical DEF-011. No product code or specification changed. This checkpoint reconciles independently gathered QA-005 evidence; it does not claim a new runtime execution.

## Acceptance criteria and historical evidence

Historical DEF-011 reports that a task entered `input-required`, but `POST /a2a/tasks/{id}/transition` returned 404 and the Console had no resume control. Its stated product outcome is that the caller supplies additional input and the same task proceeds back to `working` and then a terminal state. The accepted SM-09 / TP-IMP-0042 AC-2 contract specifies the state lifecycle; current documentation does not require the literal `/transition` URL.

QA-005 CP24 used the exact source build on an isolated A2A-enabled SQLite clone and the actual Console. The same task was read in `input-required` (version 3), resumed through the implemented `/resume` workflow with follow-up input, returned `completed` (version 5) with a fixture artifact, and persisted `completed` on a follow-up GET. The local synthetic agent recorded both initial and follow-up input; the key was revoked. See [CP24](CP24-a2a-authorized-input-required-lifecycle.md) and its unchanged sanitized evidence.

CP48 independently exercised the actual Console workflow on exact RC7 with PostgreSQL 16 and a three-node Valkey Cluster. The task appeared as Input required with a `$0.010000` hold and Resume action. The Owner submitted follow-up text; the task row persisted Completed, rendered a synthetic artifact, and settled `$0.003000`. The temporary key was revoked. Its scope had both PROVIDER and A2A planes, not A2A-only; CP49 separately tests that provider-only keys are denied A2A discovery and task creation. See [CP48–49](CP48-49-a2a-input-required-and-plane-boundary.md) and the retained evidence/screenshots.

Screenshots for the CP48 input-required row, resume modal and completed result were opened and visually inspected during this reconciliation. They show the same task ID and transition from the Resume control to Completed, but do not expose an intermediate persisted `working` observation.

## Classification

The core product-level ability to continue an input-required task is demonstrated on SQLite and PostgreSQL/Valkey, so the historical total inability to resume did not reproduce on those scoped paths. However, the strict accepted sequence explicitly includes `input-required → working → completed`; current evidence records the initial and terminal states but does not capture an independent read/event proving the intermediate `working` state or its ordering. The earlier literal `/transition` 404 also remains historical evidence. Therefore DEF-011 is **PARTIAL**, not PASS or closed.

Remaining: capture task GET/event state during resume, verify working-state ordering under delayed and asynchronous agents, replay/duplicate resume and invalid-input behavior, roles and A2A-only key on PostgreSQL/Valkey, cancellation/retry/timeout/error transitions, nonzero hold edge boundaries and interoperable partner behavior. No production signing or external provider is implicated.
