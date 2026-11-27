# CP146 — SQLite duplicate provisioning and audit reconciliation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** exact RC6 SQLite listener `127.0.0.1:18128`  
**Actor:** synthetic Org Owner  
**Product source modified:** no

## Outcome

- **PASS (scoped, DEF-007):** Through the real Console, attempted a new project with existing organization slug `default`. The POST returned HTTP 409 `resource_already_exists`; the form retained the entered name and slug and displayed “project slug already exists in organization.”
- **PASS (scoped, DEF-007):** In the existing `default` project, attempted a new environment with existing slug `dev`. The POST returned HTTP 409 `resource_already_exists`; the form retained the entered name and slug and displayed “environment slug already exists in project.”
- **PASS (scoped):** Authenticated list readback before and after showed exactly one `default` project and one `dev` environment. Filtered audit reads returned the same 2 `project.created` and 3 `environment.created` events before and after each failed attempt; `has_more=false` for each read. Neither duplicate conflict emitted a success-create event.
- **No new defect:** Duplicate-conflict behavior is independently verified on SQLite and complements CP113 on PostgreSQL. This does not establish audit atomicity after storage errors, partial writes, concurrent creation, or all provisioning workflows.

## Reproduction

1. Sign in to exact RC6 as the isolated test-root Org Owner.
2. Open the workspace picker and submit the existing project slug `default` with a distinct synthetic display name.
3. Capture the POST response, error banner, retained fields, project list and `project.created` filtered audit read.
4. Select `default` (the Console automatically selects its `dev` environment), then submit the existing environment slug `dev` with a distinct synthetic name.
5. Capture the POST response, error banner, retained fields, project environment list and `environment.created` filtered audit read.
6. Compare before and after counts and confirm both list endpoints still return exactly one matching resource.

Sanitized before/after API and audit results are in [CP146 evidence](../evidence/CP146-sqlite-provisioning-conflicts.json). Actual Chromium error states were visually inspected: [duplicate project](../evidence/CP146-project-duplicate-error.png) and [duplicate environment](../evidence/CP146-environment-duplicate-error.png).

## Limits and campaign status

An initial automation attempt looked for a separate `dev` option after selecting `default`; visual inspection showed the Console automatically retains/selects `dev`. The workflow was resumed using the observed UI state. No extra resource was created. The verified slices are scoped to duplicate slugs and do not resolve DEF-007's storage-failure and race cases. QA-004 remains ongoing; defect totals are unchanged and **GATE-REL-07 remains BLOCKED.**
