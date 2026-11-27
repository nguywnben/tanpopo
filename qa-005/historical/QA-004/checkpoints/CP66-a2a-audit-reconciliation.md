# CP66 — A2A task audit and chain reconciliation

**Baseline:** exact RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated A2A-enabled RC6 clone `127.0.0.1:18128`  
**Actor:** synthetic Org Owner; task dispatch used a scoped A2A-only Virtual Key  
**Product changes:** none

## Result

- **PASS (scoped):** Authenticated audit API reads and the real Security → Audit Log screen exposed the A2A transitions for both CP65 tasks. The action filter is exact match; `a2a.task.transitioned` showed 8 matching rows, including the two CP65 transitions.
- **PASS (scoped):** The failed synchronous task's audit sequence is `a2a.task.submitted` (#250), `submitted → working` (#251), then `working → failed` (#252). This matches the actual HTTP 400, persisted Failed row, and the source error-recovery path.
- **PASS (scoped):** The separate task's audit sequence is `a2a.task.submitted` (#254), then `a2a.task.canceled_by_admin` (#256) with `submitted → canceled`, matching the Console action and API state.
- **PASS (scoped):** The real Console **Verify hash chain** action returned HTTP 200 and reported an intact RFC 8785 chain across 262 events. Each relevant event had a stored event hash and previous hash.
- **No new defect:** Audit records truthfully match both the product task states and the observed defect. This does not resolve DEF-067; it confirms the failed state is durably auditable.

## Reproduction

1. Open the Security → Audit Log page as the isolated Org Owner.
2. Enter exact action `a2a.task.transitioned`; inspect the CP65 failed task's submitted-to-working and working-to-failed events.
3. Query exact actions `a2a.task.submitted` and `a2a.task.canceled_by_admin` for the CP65 submitted/canceled task.
4. Click **Verify hash chain** and confirm the green intact result (262 events).

## Evidence

- [Filtered task-transition audit and chain verification screenshot](../evidence/CP66-a2a-audit-transitioned-and-chain-verified.png)
- [Sanitized event sequence and verification response](../evidence/CP66-a2a-audit-events.json)
- [CP65 task lifecycle and DEF-067 details](CP65-a2a-working-response-and-cancel-lifecycle.md)

The actual screenshot was opened and visually inspected. QA-004 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
