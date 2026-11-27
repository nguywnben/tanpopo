# DEF-060 — Initial extension install hides disabled rollback guidance

**Severity:** Moderate
**Status:** FAIL
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflow:** extension upgrade operations, rollback planning, and incident recovery discoverability.

## Summary

When an installation has no previous pinned version, the Extensions row action menu omits Rollback entirely. The documented Console empty state requires the Rollback action to remain visible but disabled with an explanatory tooltip. Operators therefore receive no indication whether rollback is unavailable because the installation is at its initial pin, hidden by permissions, or missing due to an interface defect.

## Reproduction

1. In isolated healthy test-root runtime `127.0.0.1:18128`, sign in through Chromium as the synthetic Operator / Org Owner and open **Extensions → Installed**.
2. Confirm the active Ollama installation is version `0.1.0`. Authenticated `/admin/v1/extensions/installations` readback reports `previous_rollback_version: null` and no candidate.
3. Open the row action menu. It contains Review permissions, Upgrade package, and Quarantine package; no Rollback action or explanatory tooltip is present.
4. Compare the empty-state contract in `docs/ux/user-flows.md`: when `previous_pinned_version_id IS NULL`, the Rollback button is disabled with a tooltip explaining no prior pin exists and pointing to Disable/Quarantine for emergency cutoff.

## Expected and actual behavior

**Expected:** show a discoverable Rollback action in disabled state with an accessible explanation that the installation has no prior pin; include the documented safe alternatives. No rollback mutation should be possible.

**Actual:** omit Rollback from the row action menu entirely. The screen gives no reason why it is absent.

## Business impact

Operators inspecting a newly installed extension cannot learn from the installed row that the rollback path becomes available only after a previous version is retained. During release planning or an incident, this creates avoidable uncertainty about whether the package can be rolled back and where to find a safe containment action.

## Evidence

- `../checkpoints/CP45-extension-upgrade-and-rollback-discoverability.md`
- `../evidence/CP45-extension-installed-18128.png`
- `../evidence/CP45-extension-actions-18128.png`
- `../evidence/CP45-extension-lifecycle-preconditions.json`
- Contract: `../../docs/ux/user-flows.md`, SM-01 Extension Upgrade, Empty state.

## Facts and root-cause hypothesis

**Facts:** API readback returns `previous_rollback_version:null`; the real action menu has no Rollback item. The UX specification explicitly requires a disabled action with explanatory tooltip in this state. Frozen-source inspection confirms the action array adds Rollback only when `row.previous_rollback_version` is truthy.

**Root-cause hypothesis:** the implementation treats the absence of a rollback target as a reason to hide the control, while the documented design expects a visible disabled control and help text. This is consistent with the evidence but does not establish design or implementation history.

## Acceptance criteria

1. When there is no previous pin, keep the Rollback action visible but disabled in the real row menu.
2. Provide accessible hover, keyboard-focus, and screen-reader explanation: no previous pin exists.
3. Show the documented safe alternatives (Disable or Quarantine) without implying they perform rollback.
4. Confirm the disabled control cannot submit a rollback mutation and the installation remains unchanged.
5. After a valid upgrade has retained a previous pin, show the enabled rollback action and test the separate rollback lifecycle, audit, and post-rollback inference.
