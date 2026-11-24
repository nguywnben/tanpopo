# DEF-029 — Access & Keys shows stale environment keys after switching to organization-only scope

**Severity:** Low (P3)
**Status:** Confirmed on frozen QA-002 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Summary

A delayed Virtual Key list request for a selected project/environment can complete after the user switches to organization-only scope. The Access & Keys page then renders the old environment’s keys under “No project / No env,” while its own scope banner says a project and environment are required. The stale rows include key names, prefixes/last-four characters, planes/routes, and status. No key secret was exposed and mutation actions were absent in the observed no-environment state. This is a confirmed state-consistency defect; no authorization bypass was observed.

## Reproduction

1. Sign into the isolated RC4 Console as the synthetic ORG_OWNER `qa002-cp39-org-owner@example.invalid`; choose project `QA002 CP43 Secondary Project`, environment `qa002-cp43-env2`.
2. Install a browser-only Playwright route that pauses the first `GET /admin/v1/virtual-keys` request without changing the server or product binary.
3. Open **Access & Keys → Virtual Keys**. The held request is for that exact project and environment and will return HTTP 200 with 10 environment-bound key records.
4. While it remains pending, use the workspace selector to choose **No project** (which also selects **No env**).
5. Before release, the Console shows the informational banner, key count 0, and “No Virtual Keys in this environment.” Capture `CP62-access-keys-org-only-before-response.png`.
6. Release the original response. The selected workspace remains **No project / No env**, but the page now shows count 10 and all 10 keys from the prior environment. It still displays the banner that a project and environment are required. Capture `CP62-access-keys-org-only-after-response.png`.
7. Navigate to Overview and back to Access & Keys while retaining the no-project scope. The list clears and remains empty; this is the observed recovery path. See `CP62-access-after-navigation-recovery.png`.

The JSON evidence records the exact query IDs, HTTP 200 response, returned key count and sanitized key metadata. The delayed payload contained no secret material; response data is not committed beyond names, environment/project IDs, and operational state.

## Expected versus actual

- **Expected:** When project/environment scope changes, only results for the currently selected scope may be committed to page state. In a no-project/no-environment context, no environment key rows should be displayed. A response started for an earlier scope must be ignored after that scope is no longer current.
- **Actual:** The early-return branch initially clears keys, but the older in-flight request later repopulates the list. The no-scope banner remains visible alongside stale environment-scoped records.

## Impact

Operators can mistake revoked or active keys from a different environment for keys in the current workspace, leading to incorrect support, audit, or access-management decisions. The visible information is metadata, not the key secret. This run used an Org Owner already authorized to enumerate both scopes; it does **not** prove cross-tenant access or an authorization bypass. Limited-role cross-project exposure has not been established. Navigation/unmount clears the stale list in the tested path.

## Facts and root-cause hypothesis

**Facts:** the held request was `GET /admin/v1/virtual-keys?project_id=prj_01m85ry2gme00an7wydh1pd1gc&env_id=env_01m85ry445e009xg6sr14c5q1z&limit=100`, returned HTTP 200 with 10 records, and every record carried those same project/environment IDs. The workspace selector showed `No project / No env` both before and after the held response. Before release the UI count was 0; afterward it was 10 and listed those keys. A later navigation/re-entry cleared the rows.

**Source observation / hypothesis:** `web/console/src/pages/access/AccessPage.tsx` has no scope-generation check or cancellation guard around the async `Promise.allSettled` result writes. A scope change enters the no-scope early return and clears state, but the earlier async call can still run its `setKeys` after resolving. This is strongly supported by the controlled ordering, but is recorded as the likely implementation cause rather than a separate runtime fact.

## Acceptance criteria

- Associate each asynchronous Access & Keys load with its captured project/environment scope or request generation; ignore results after the selected scope has changed or the component has unmounted.
- Switching from an environment to no-project/no-environment keeps environment Virtual Keys empty even if the old response returns later.
- Switching between environments under delayed/out-of-order responses never displays records from the previous environment in the newly selected one.
- Clearing scope continues to hide/disable environment-only actions, and the loading/empty state matches the selected scope.
- Add a deterministic regression that holds the old environment response, switches scope, then releases it and verifies no stale rows appear. Preserve existing authorization checks and do not widen key visibility.
- Retest the related DEF-023 member-list scope behavior independently; one fix must not hide organization-wide membership in valid organization contexts.

## Evidence

- `../evidence/CP62-access-scope-race.json` — held request query/status, sanitized returned key metadata, before/after scope and page text.
- `../evidence/CP62-access-keys-loading-before-scope-switch.png` — real page with the environment list request held.
- `../evidence/CP62-access-keys-org-only-before-response.png` — no-scope empty state before delayed result.
- `../evidence/CP62-access-keys-org-only-after-response.png` — visually inspected stale ten-key list under No project / No env.
- `../evidence/CP62-access-scope-recovered-empty.png` and `../evidence/CP62-access-after-navigation-recovery.png` — no-scope state and navigation recovery.
- Related finding: [`DEF-023`](DEF-023-org-members-hidden-until-project-selected.md) and [`CP60`](../checkpoints/CP60-workspace-scope-and-private-hostname-boundaries.md).

No product code, database, or server configuration was modified for this test. Browser response delay was injected only in the test client.
