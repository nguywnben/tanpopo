# DEF-020 — Emergency kill-switch badges stay active after clear

**Severity:** Moderate  
**Status:** Confirmed on QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Affected workflows:** Security → Kill Switches; global Kill Switch status; cross-screen navigation after switch clear.

## Expected behavior

After an authorized operator clears an active kill switch, the Security screen, navigation badge, and global Kill Switch status should reflect that no switch is active without requiring a full browser reload. The empty-state text and counters should agree across the application shell and page content.

## Actual behavior

The clear request succeeded and the Security page displayed “Kill switch cleared and logged,” an active-switch count of 0, and “No active kill switches.” In the same authenticated session, the global Kill Switch pill remained red with count `1`, and the Security navigation item retained badge `1`. Navigating to Playground preserved the stale global badge. The authenticated `GET /admin/v1/kill-switches` returned `{"data":[]}`. A full browser reload then showed the pill without a count, no Security badge, and the same empty state.

The observed discrepancy is transient and clears after reload. The evidence does not show that the switch remained active after the server cleared it.

## Reproduction

1. Sign in as an owner with an isolated project containing test entities.
2. Open **Security → Kill Switches** and trigger a project-level switch through the two confirmation steps.
3. Confirm the active-switch row and `GET /admin/v1/kill-switches` show one active switch.
4. Clear that row through the UI and provide a clear reason.
5. Observe that the page says there are zero active switches while the shell's global Kill Switch pill and Security navigation badge still show `1`.
6. Navigate to another screen; the global count remains stale. Reload the page; both badges reconcile to zero.

## Evidence

- `evidence/CP27-killswitch-active.png` — visually inspected active switch and one affected-project result.
- `evidence/CP27-killswitch-active.json` — trigger returned 200, one persisted active switch, target and actor IDs.
- `evidence/CP27-killswitch-playground.png` — visually inspected expected 503 while the project freeze was active.
- `evidence/CP27-killswitch-cleared.png` — visually inspected zero active rows and success notice alongside stale shell/navigation badges of `1`.
- `evidence/CP27-killswitch-recovery.json` — clear action and audit chain verification responses; active switch list empty. The initial recovery attempt was separately budget-blocked.
- `evidence/CP27-killswitch-recovery-success.png` — visually inspected successful Playground recovery after raising the test project's exhausted cap.
- `evidence/CP27-killswitch-recovery-success.json` — recovery request HTTP 200 and fixture request evidence.
- `evidence/CP27-killswitch-badge-after-reload.png` and `.json` — visually inspected reconciled zero badges and API empty state after reload.

## Business impact

Operators can continue to see an emergency-control indicator after the incident has been cleared, causing unnecessary incident response and reducing confidence in the control plane. The confirmed behavior is stale presentation, not failed enforcement or a switch that remains active.

## Acceptance criteria

- On clear success, the shell's global Kill Switch count and Security navigation badge update to zero in the same session.
- Navigating between screens does not restore a stale active count; the displayed count agrees with the server's active-switch list.
- A trigger/clear regression test verifies both the active and cleared states, including one cross-screen transition without a reload.
- If the refresh cannot complete, the UI shows an explicit loading/error state rather than an old count presented as current.
