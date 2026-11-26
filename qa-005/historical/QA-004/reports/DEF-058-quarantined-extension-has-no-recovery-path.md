# DEF-058 — Quarantined extension has no supported recovery path

**Severity:** Major
**Status:** FAIL
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Affected workflow:** emergency extension containment, incident recovery, provider availability.

## Summary

After an authorized Operator quarantines an extension, RC6 provides no tested UI or API operation to perform the documented audited transition from `QUARANTINED` to `INSTALLED_DISABLED`. The only displayed removal option is also rejected while dependent provider connections remain. This leaves emergency-contained installations stuck in quarantine and their dependent inference unavailable, without a supported route through forensic review back to a disabled state.

## Reproduction

1. In isolated test-root instance `127.0.0.1:18124`, sign in as `qa004-testroot-owner@example.invalid` with `OPERATOR` and `ORG_OWNER` roles and open **Extensions → Installed**.
2. Using the already-installed synthetic Ollama extension, choose **Quarantine package**. In the real confirmation modal enter the reason `QA-004 adapter quarantine acceptance probe`, enter exact package ID `@tanpopo/ollama`, and confirm.
3. Confirm the success notice and installed row show `QUARANTINED`.
4. Open the row action menu. It offers Review permissions, Upgrade package, and Uninstall package; it has no Unquarantine action.
5. As the authenticated Operator, POST `/admin/v1/extensions/{installationId}/unquarantine`. RC6 returns 404 `resource_not_found`. POST `/approve-permissions` returns 409 `invalid_state_transition` because the installation is quarantined.
6. Attempt the displayed uninstall operation with current `If-Match: "v3"`. It returns 409 `extension_referenced_by_active_connections` and reports two dependent provider-connection records. Read back the installation: it remains `QUARANTINED`, version 3.

## Expected and actual behavior

**Expected:** the SM-01 state machine explicitly defines `QUARANTINED -> INSTALLED_DISABLED` as audited unquarantine by `OPERATOR` / `ORG_OWNER` (`docs/requirements/business-entity-map.md`). An authorized operator should be able to complete forensic review and restore the installation to a non-serving disabled state, then separately review permissions, enable, probe, and restore traffic. Unquarantine must not silently reactivate the package.

**Actual:** no Unquarantine UI action or API route is available. Approval cannot transition from quarantine, and referentially safe uninstall is rejected while dependent connections exist. The installation stays quarantined.

## Business impact

The emergency kill action works, but recovery cannot proceed through the documented lifecycle. An operator must leave the integration unavailable or undertake an unsupported destructive workaround such as dismantling dependent connection records before package removal/reinstallation. This can prolong provider outage after an incident is understood and cleared, while making state and recovery ownership unclear.

## Evidence

- `../checkpoints/CP42-extension-quarantine-recovery.md`
- `../evidence/CP42-quarantine-confirm.png`, `CP42-quarantine-confirm-filled.png`, `CP42-quarantined-state.png`, `CP42-quarantined-actions.png`
- `../evidence/CP42-recovery-probes.json` — missing endpoint 404, approval 409, dependent uninstall 409, final quarantined state/version.
- `../evidence/CP42-system-audit.json` — quarantine event present in SYSTEM partition and intact four-event chain.
- Frozen-source route/action inspection: `web/console/src/pages/extensions/ExtensionsPage.tsx`, `internal/admin/http/server.go`; lifecycle state edge is documented in `docs/requirements/business-entity-map.md`.

## Facts and root-cause hypothesis

**Facts:** the actual UI reaches and confirms quarantine, persists `QUARANTINED`, and removes execution from two tested request paths. Its quarantined-row menu has no Unquarantine action. The attempted unquarantine endpoint is not found; approval from this state is invalid; uninstall is blocked by dependencies. The source route registry exposes no unquarantine route, although the lifecycle model includes the edge.

**Root-cause hypothesis:** the lifecycle transition is represented in the state-machine contract and generic lifecycle layer, but its recovery command was not carried through the Admin API and Console action model. This explains the observed gap but does not establish implementation history or intent.

## Acceptance criteria

1. Provide a documented, authenticated, audited unquarantine operation for authorized instance OPERATOR / ORG_OWNER roles; enforce optimistic concurrency and require an incident/recovery reason.
2. Transition only `QUARANTINED -> INSTALLED_DISABLED`; never automatically restore execution or clear forensic evidence.
3. Keep all dependent provider routes blocked until a separate authorized enable/reprobe workflow succeeds; preserve permission review, trust policy, package digest, and kill-switch safety.
4. Present the recovery action and its consequences in the real Console; refresh, navigation, and API readback must consistently show the disabled state.
5. Deny unauthorized roles and stale ETags without changing installation, barriers, connections, or audit state.
6. Permit uninstall only under existing referential-integrity rules and provide actionable dependency guidance; do not weaken those rules to work around missing recovery.
7. Add end-to-end coverage for quarantine → blocked traffic → audited unquarantine to disabled → explicit enable/probe → restored traffic, including failure/rollback behavior.
