# CP65 — Access & Keys cross-project delayed-response race

**Result:** FAIL — DEF-029 expanded across valid project/environment scopes.  
**Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Method and result

In Chromium at 1440×900, signed in as the synthetic organization owner, the browser harness held the HTTP 200 `GET /admin/v1/virtual-keys` response for the secondary project `prj_01m85ry2gme00an7wydh1pd1gc` and environment `env_01m85ry445e009xg6sr14c5q1z`. The response contained ten rows whose project/environment IDs were the old scope. Before releasing the response, the real workspace selector was changed to `QA002 CP39 Tenant Project / qa002-cp39-env`; the page showed zero environment keys. After release, the selector still showed the tenant project/environment, but all ten secondary-project key rows appeared, with `qa002-cp43-env2` prefixes. All ten were revoked in this dataset.

I visually inspected the before/after screenshots. The before image shows the tenant project/environment selected with the empty-state message. The after image shows the same selection and ten prior-project rows. The rows expose metadata only; this session was an Org Owner authorized for both projects, so the result demonstrates scope/data consistency failure and does not establish an authorization bypass or limited-role access.

After a hard refresh, the Console returned to its runtime-default secondary project/environment and the list matched that scope. This repeats QA-OBS-009: a manually selected workspace does not persist across refresh. Refresh therefore restored internal consistency by changing the selected scope, rather than retaining the tenant-project choice.

## Evidence

- `../evidence/CP65-access-project-switch-race.json` — held query/response scope, before/after page state, and refresh result.
- `../evidence/CP65-access-env2-loading.png` — prior-scope Access & Keys request pending.
- `../evidence/CP65-access-project1-before-release.png` — tenant project/environment empty state before release.
- `../evidence/CP65-access-project1-after-release.png` — visually inspected stale secondary-project rows under the tenant project/environment.
- `../evidence/CP65-access-project1-after-refresh.png` — visually inspected runtime-default secondary project/environment after refresh.
- Cross-browser/no-scope reproduction: [CP64](CP64-webkit-mobile-access-scope-race.md).
- Defect record and acceptance criteria: [DEF-029](../reports/DEF-029-access-keys-stale-after-workspace-scope-change.md).

## Coverage update

DEF-029 remains Low (P3); no new defect ID. The stale-response defect now has Chromium evidence for both no-scope and project-to-project switching, plus WebKit mobile evidence for no-scope switching. Limited-role exposure, environment-to-environment switching within one project, and other page loaders remain unverified. Refresh persistence observation QA-OBS-009 recurred; do not classify the configured default reset itself as a new defect. No mutation was submitted, no secrets were exposed, and no product code/configuration was changed.
