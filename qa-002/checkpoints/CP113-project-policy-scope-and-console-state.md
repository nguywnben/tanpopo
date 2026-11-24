# CP113 — Project policy access across workspace scopes

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo Console `18084`  
**Browser/role:** real Chromium, 1440 × 1000; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Journey and evidence

Used the real workspace chooser and Console navigation, without direct DOM state mutation. Tested organization-only, project-only, and project/environment contexts for Routes & Policies → Policies; then navigated away/back and hard-refreshed.

| Context | Observed UI/API | Result |
| --- | --- | --- |
| Organization-only | Policies selected; explicit “Select a project and environment…” prerequisite; no editor. | Scoped PASS for clear no-scope guard. No org-only policy editing attempted. |
| Existing project, no environment | `QA002 Audit mwto47wa / No env`; same prerequisite and no policy table/editor. `GET /admin/v1/policies/effective?project_id=prj_01m84ymb7we009px5hb5pfwsns` returned HTTP 200 / ETag `"v1"`. | **FAIL — DEF-039 Low:** project policy is API-readable and documented, but UI blocks its review/edit until an environment is selected. |
| Same project, `stage` environment | Effective table showed eight policy constraints and provenance; Routes tab showed two configured routes. Edit scope options were Organization, Project, Environment. Selecting Project exposed its unchanged form; Save remained disabled. | Scoped PASS for environment policy display, editor-scope switching, and no-write state. |
| SPA navigation away/back | `stage` selection and policy table/editor remained consistent. | Scoped PASS. |
| Hard refresh | Returned to runtime-default `QA002 CP78 Fresh Budget / No env`, where the environment prerequisite returned. | QA-OBS-009 recurrence; preference contract remains unresolved, no new defect. |

Screenshots were visually inspected after interactions: `../evidence/CP113-org-only-policy-prerequisite.png`, `CP113-project-no-env-message-settled.png`, `CP113-environment-policies-closed.png`, `CP113-policy-edit-scope-options.png`, `CP113-project-policy-editor-selected.png`, and `CP113-policy-after-refresh-closed.png`. Sanitized API/UI state is `../evidence/CP113-policy-project-only-ui-api.json`.

No policy or route write was attempted; the unchanged Project editor's Save button was disabled. The test did not request any authorization relaxation or inspect another tenant. The Team Admin credential was unavailable in this runtime, so role-boundary coverage remains UNVERIFIED here; CP71/CP88 remain prior limited evidence.

## DEF-039 disposition

The policy hierarchy contract is Organization → Project → Environment. The selected project already has an environment policy and the same page explicitly exposes a Project editor when an environment is active. Requiring any environment to reach the Project editor creates an undocumented project-onboarding dependency. See [DEF-039](../reports/DEF-039-project-policy-ui-requires-an-environment.md) for expected behavior, impact, evidence, root-cause hypothesis, and acceptance criteria. This does not classify environment-required route behavior as defective.

## Consolidated status summary

- **Historical defects verified fixed for scoped QA-002 retests:** DEF-001–004, DEF-006, and DEF-008–015 retain only the limited QA-002 outcomes in the retest register; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 success paths do not close fault atomicity, storage-failure, or erasure-proof gaps.
- **Historical defects failing/open:** DEF-016–038 remain preserved and open, including confirmed DEF-038 High and the CP110 DEF-021/023 retests. CP113 does not change their disposition.
- **New defect:** DEF-039 Low — project policies cannot be reviewed/edited in project-only scope without an environment. No permission bypass observed.
- **Coverage completed:** read-only Org Owner Chromium interaction across org-only, project-only, and project/environment scopes; policy API/UI comparison; project editor scope change; SPA navigation, hard-refresh behavior, and screenshot inspection.
- **Coverage blocked:** Team Admin project-policy role retest unavailable due to lack of a usable Team Admin session; no inference about that role from the owner run. Firefox/WebKit execution remains blocked by CP111 managed-image limits.
- **Coverage remaining:** full DEF-001–015 acceptance remains incomplete; DEF-005 DNS rebinding/mixed-answer and residual SSRF cases; DEF-007 fault atomicity and backup/erasure proof; all open DEF-016–039 regression; Team Admin project policy and write/conflict tests; complete remaining feature, integration, browser, platform, accessibility, security, and operational scenarios.
- **Recommended handoffs:** add DEF-039 to the existing workspace-scope engineering bundle with DEF-023/024/029/030/035; preserve DEF-038/007 as higher-priority reliability/privacy handoffs. Do not weaken SSRF, role authorization, or monotonic policy inheritance.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
