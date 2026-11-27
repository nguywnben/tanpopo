# CP69 — A2A input-required cancellation and reservation release

**Result:** PASS for the corrected isolated-fixture run; initial harness attempt FAILED because of a fixture port mismatch. No product defect found.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Initial harness failure and correction

The first synthetic task used the registered agent endpoint `127.0.0.1:19186`, but the temporary fixture script was still bound to `127.0.0.1:19184`. The task therefore persisted as Failed before reaching the endpoint, and no Cancel action appeared in the Console. The fixture log was empty. Read-only database evidence shows the failed task's unused reservation was released. I corrected the fixture listener to 19186 and used a fresh task plus a new idempotency key. This was test infrastructure misconfiguration, not product behavior.

## Corrected cancellation run

With the enabled isolated runtime `18084`, the A2A-only key created in CP69 submitted a task to the CP68 mock agent. The task reached `input-required`, with a 5,000-micro hold cap. After a hard refresh, the same task ID and state remained in the Console; the screenshot was visually inspected.

I selected **Cancel** in the real Console. The UI returned HTTP 200, showed **Canceled A2A task and released held budget**, and rendered the task as Canceled with zero settled cost. The task API remained Canceled. A read-only SQLite check confirmed its reservation row state was `RELEASED_UNUSED`, its 5,000-micro hold was not in `budget_reservation_ledgers`, and the task's settled cost was 0. The UI continues displaying `$0.005000` in its historical **Sub-budget hold** column; because the persisted active reservation was released, this is the task's original hold cap, not an outstanding active hold.

The synthetic endpoint log has exactly one call for this task: the initial input without the approval marker. No resume/second endpoint side effect occurred. The A2A-only key was revoked in the Console; a later A2A discovery replay returned 401 `invalid_virtual_key`.

## Evidence

- `../evidence/CP69-a2a-cancel-lifecycle.json` — task IDs, initial harness issue, UI/API cancel result, key revoke and replay.
- `../evidence/CP69-a2a-fixture-calls.json` — one initial fixture call and no follow-up call for the canceled task.
- `../evidence/CP69-a2a-reservation-persistence.json` — read-only SQLite task/reservation state for both the fixture-mismatch task and corrected canceled task.
- `../evidence/CP69-a2a-input-required.png` and `CP69-a2a-input-required-after-refresh.png` — task before and after hard refresh, visually inspected after correction.
- `../evidence/CP69-a2a-canceled.png` — visually inspected success notice, Canceled state, and zero settled cost; it also retains the failed harness-attempt row.

## Coverage update

The scoped Console cancel path released the unused sub-budget reservation and prevented a resumed fixture side effect. Cancellation on terminal tasks, in-flight cancellation timing, multihop, cross-region, timeout, and provider/runtime failure recovery remain open. DEF-011's full historical acceptance is not closed by this one path. No product code was modified; the loopback mock remains available for further isolated QA.
