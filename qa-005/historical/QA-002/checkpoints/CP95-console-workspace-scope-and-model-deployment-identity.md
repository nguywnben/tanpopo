# CP95 — Console workspace scope and model deployment identity

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4)
**Runtime:** QA server `18084`; Org Owner
**Browser:** real Chromium UI, 1440 × 1000; screenshots visually inspected
**Product implementation:** unchanged

## Phase A — organization/project/environment coverage

Used the actual workspace selector and SPA navigation to compare organization-only (`No project / No env`), project-only (`QA002 CP78 Fresh Budget / No env`), and project/environment (`QA002 Audit mwto47wa / stage`) states. Covered Extensions installed/catalog/lockfile, Models, and Routes & Policies. Screenshots and sanitized rendered text/API response evidence are in `evidence/CP95-*`.

- Extensions showed the same two installed packages and eight-item catalog across all three scopes. The evidence is consistent with an organization-level catalog/install inventory; no defect assigned. Other roles and extension installation mutation in each scope were not exercised.
- Routes & Policies explained the missing project/environment context in org-only and project-only states. In the selected `stage` environment, it displayed two routes and effective policy with organization-floor/environment-policy provenance. This is a narrow successful state check, not full route or policy coverage.
- Models rendered the same three deployments in org-only, empty project, and stage-environment contexts. API ownership checks established that all three belong to the `stage` environment in the sibling project. See DEF-035.

## Phase B — DEF-035 reproduction

Authenticated API exploration mapped the only environment in the selected organization’s displayed test data to project `QA002 Audit mwto47wa / stage`; another project `QA002 CP78 Fresh Budget` had no environments. The unfiltered deployment endpoint returned three rows, all belonging to that stage environment. Real Models UI in the empty CP78 project still displayed all three. Its request was `/admin/v1/deployments?limit=100`, without `env_id`. Opening the first row menu showed **Disable deployment** and **Delete deployment**. No action was selected and no resource was mutated.

The valid environment-scoped API read returned the same three stage rows. Direct API request and page evidence are linked in [DEF-035](../reports/DEF-035-model-deployments-ignore-active-workspace-scope.md). A prior exploratory request using an intentionally incorrect environment ID returned 404; that harness probe is not used as defect evidence.

## Results

| Area | Result | Evidence and limits |
| --- | --- | --- |
| Extension inventory across scopes | PASS (narrow, observed) | Same installed count/catalog in three scopes; no install/mutation. Organization-level contract inferred from presentation, not independently documented. |
| Routes & Policies no-scope handling | PASS (narrow) | Clear scope-selection prompt under org-only and project-only; active environment shows two routes/effective policy. Team Admin and empty environment controls remain open. |
| Models deployment scope | FAIL — DEF-035 Moderate | Three `stage` deployments visible in org-only and a different empty project, without owner-scope columns; Disable/Delete accessible. No mutation or auth bypass. |
| DEF-023 | Not retested | CP95 does not change prior confirmation; member-scope defect remains open. |

## Consolidated defect/status summary

- **Historical DEF-001–015:** CP95 did not retest them. Preserve previous QA-002 scoped results only: DEF-001–004, DEF-006, and DEF-008–015 have limited passing acceptance paths; DEF-005 remains partial; DEF-007 successful-path audit evidence does not cover failure atomicity/populated erasure. Independent complete acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical open DEF-016–034:** remain open; CP95 changes no dispositions. See the preserved retest register and earlier checkpoints.
- **New defect:** DEF-035 Moderate, confirmed as above. No separate new observation.
- **Coverage completed:** real scope selection and SPA navigation across three workspace contexts; Extensions, Models, Routes & Policies; API mapping of deployment IDs to environment/project; visually inspected Models screens and row action menu.
- **Coverage blocked:** no CP95 blocker. Existing Cloud egress/webhook, A2A runtime 18445 configuration, OTel collector access, and separate DevBox RC4 artifact limitations remain as recorded.
- **Coverage remaining:** all previously recorded unfinished feature/role/platform work; full DEF-001–015 acceptance; DEF-005 SSRF cases without weakening protections; DEF-007 error/fault paths; all open DEF-016–035 regression; other Console pages and role-specific scope boundaries; mutation/recovery, delayed responses, and secondary browsers/platforms.
- **Recommended handoff:** add DEF-035 to the active scope/state consistency engineering handoff with DEF-023/024/029/030; retain existing priorities DEF-034, DEF-033, DEF-005, and DEF-007.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
