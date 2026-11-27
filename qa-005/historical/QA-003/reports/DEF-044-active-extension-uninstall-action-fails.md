# DEF-044 — Active extension offers an uninstall action that fails

- **Severity:** Low
- **Status:** Confirmed lifecycle UX defect; backend safely refused direct removal
- **Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- **Affected workflows:** Extensions → Installed → quarantine, uninstall, and recovery
- **Checkpoint:** [CP23](../checkpoints/CP23-extension-marketplace-provenance-and-lifecycle.md)

## Reproduction

1. On the isolated SQLite runtime, install and activate the synthetic built-in Anthropic adapter using its permission review sheet. Readback shows `state=ENABLED_ACTIVE`.
2. In Extensions → Installed, open the active package row's Actions menu. It offers **Uninstall package** alongside Review permissions, Upgrade package, and Quarantine package. See [active row](../evidence/CP23-anthropic-installed-row.png) and [menu](../evidence/CP23-anthropic-action-menu.png).
3. Select **Uninstall package**, provide the required audit reason, and confirm. `DELETE /admin/v1/extensions/{id}` returns HTTP 409 `invalid_state_transition`; readback remains `ENABLED_ACTIVE`. The UI error says “This extension package is already installed. Open Installed extensions to inspect or upgrade it” while the user is already in Installed. Evidence: [confirmation](../evidence/CP23-anthropic-uninstall-confirm.png), [rejected state](../evidence/CP23-anthropic-uninstall-rejected.png), and [API result](../evidence/CP23-anthropic-uninstall-result.json).
4. Recovery required a separate **Quarantine package** action with an audit reason and exact package-ID confirmation. Quarantine returned 200 and persisted `QUARANTINED`; subsequent uninstall returned 204 and removed the installation. Evidence: [quarantine confirmation](../evidence/CP23-anthropic-quarantine-confirm.png), [quarantined row](../evidence/CP23-anthropic-quarantined.png), and [cleanup result](../evidence/CP23-anthropic-cleanup-result.json).
5. Quarantine left an active Extension Installation kill switch after uninstall. The Operator cleared it through Security → Kill Switches; readback returned zero active switches. See [orphan switch row](../evidence/CP23-post-uninstall-killswitch.png), [clear confirmation](../evidence/CP23-clear-orphan-switch-confirm.png), and [cleared state](../evidence/CP23-extension-switch-cleared.png).

## Expected vs actual

- **Expected:** Either permit safe uninstall by applying the supported quarantine/deactivation sequence, or hide/disable the action for active packages and explain the required quarantine step before confirmation.
- **Actual:** The action is offered for an active package, but the server rejects it with 409 only after the user confirms. Its error message points the user back to the screen they are already using. The safe workaround is not mentioned.

## Business impact and authorization

This creates a dead-end lifecycle action and delays routine removal or incident cleanup. The backend safety guard held: the active extension remained intact until it was quarantined, and no data-plane use occurred. After quarantine, the kill switch remained active after uninstall and required separate manual clearing; the user can clear it through the Security screen.

## Facts vs root-cause hypothesis

**Facts:** the menu exposed uninstall in `ENABLED_ACTIVE`; the confirmed delete returned 409 and left state unchanged; quarantine then uninstall succeeded; the kill switch remained active until cleared.

**Hypothesis:** the action menu is not filtered by installation lifecycle state and the error maps a backend conflict to a generic already-installed message rather than the allowed transition.

## Acceptance criteria

1. Make the active-package path explicit: safe quarantine-then-uninstall with impact confirmation, or an unavailable action that explains the required state transition.
2. Return an actionable conflict message naming the required quarantine/deactivation step.
3. Define whether uninstall should clear or preserve an Extension Installation kill switch; display the remaining switch and provide a direct link to clear it if preserved.
4. Verify behavior for staged, active, quarantined, failed, and already-uninstalled packages, with persistent API/audit readback.

## Remaining limits

One built-in Anthropic extension, one isolated SQLite clone, Operator/Org Owner, and Chromium desktop were tested. No external package, live data-plane traffic, active stream, extension update, rollback, alternate adapter, or PostgreSQL/Valkey behavior was exercised. The finding does not indicate an authorization bypass or unintended uninstall.
