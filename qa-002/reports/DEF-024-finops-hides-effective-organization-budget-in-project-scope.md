# DEF-024 — FinOps project view hides an applicable organization budget

- **Severity:** Moderate (P2)
- **Status:** Confirmed on frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`
- **First evidence:** CP43
- **Affected workflows:** FinOps budget review, organization/project scope switching, project budget administration, budget incident diagnosis
- **Principal/context:** Synthetic organization owner in the isolated CP43 SQLite runtime; no production data or credentials

## Reproduction

1. In organization-only scope, create an organization monthly hard budget through FinOps. CP43 used a synthetic 1,000,000 micro-USD limit. The Console displayed a success notice and the organization row; screenshot: `../evidence/CP43-org-budget-created.png`.
2. Select a different project in the same organization that has no project or environment budget. CP43 created `QA002 CP43 Secondary Project` and `QA002 CP43 Secondary Environment` through the Console, then opened FinOps. Screenshot: `../evidence/CP43-project2-finops-budget-retest.png`.
3. Observe “Budget ceilings 0,” “No budgets configured,” and “Budget records appear once configured for your organization or project.”
4. In the same authenticated session, `GET /admin/v1/budgets?limit=100` returned the persisted organization budget. `GET /admin/v1/budgets?project_id=<secondary-project>&limit=100` returned 200 with an empty data array. The all-record response also contained two persisted rows for the first project (project and environment scope), plus the organization row.
5. The behavior is repeatable after navigation and reload. The organization-only view again lists the same persisted organization budget; selecting the secondary project again presents the empty state.

## Expected vs. actual

- **Expected:** A project-scoped FinOps view must make applicable ancestor ceilings visible, or clearly present a distinct “local budgets” view alongside inherited/effective limits. The ledger design states that reservations enforce Virtual Key → Environment → Project → Organization scopes simultaneously. The project’s effective view should not imply no ceiling exists when an organization ceiling is configured.
- **Actual:** FinOps passes the selected `project_id` to the list endpoint. The project-filtered result excludes organization-scope rows, and the UI renders the empty state without indicating that inherited organization limits are omitted. The organization row remains persisted and is visible only with no project filter.

## Business impact

An owner or project administrator can inspect a project with active traffic and conclude that it has no configured ceiling, despite a governing organization cap. This obscures the source of budget exhaustion, complicates cap planning, and can prompt redundant project-level configuration. No inference traffic or budget enforcement was attempted during CP43, so this finding is specifically about Console visibility and user-facing state, not a failure of ledger enforcement.

## Evidence and basis

- `../evidence/CP43-org-budget-created.png` — visually inspected, organization budget row and success notice.
- `../evidence/CP43-project2-finops-budget-retest.png` — visually inspected, selected secondary project/environment and misleading empty state.
- `../evidence/CP43-project-budget-created.png` and `../evidence/CP43-environment-budget-created.png` — project/environment rows shown in the other project context while the organization row is omitted.
- CP43 same-session API summaries: unfiltered list contained ORGANIZATION (1,000,000 micro-USD), PROJECT and ENVIRONMENT rows; project-filtered secondary-project list was empty. No credentials, database, or raw logs are included.
- `docs/architecture/deployment-topology.md` describes simultaneous budget enforcement across `VIRTUAL_KEY -> ENV -> PROJECT -> ORG` scopes.
- Source inspection corroborates the observation: Console `listBudgets(projectId)` adds `project_id`; backend filtering compares each row’s resolved project and excludes the organization row. This is a root-cause hypothesis until engineering confirms intended list semantics.

## Root-cause hypothesis (not a verified fact)

The Console API client scopes the budget collection to one project, and the server maps an organization budget to no project when applying this filter. This likely suits a “local configuration only” listing but the UI presents it as all budget ceilings and uses an unqualified empty state. The intended contract may instead require returning effective ancestor scopes or explicitly labelling the view as local-only.

## Acceptance criteria

1. With an organization budget and a project selected, FinOps shows the applicable organization ceiling and identifies it as inherited/read-only (or presents a clearly named local-budget list plus a separate effective-ceiling summary).
2. Direct project/environment ceilings remain separately identifiable; values are not duplicated or represented as organization-owned local settings.
3. An empty state appears only when the chosen list truly has no applicable configured budgets, and its text states which scope(s) it covers.
4. Organization/project switching, refresh, and a fresh session preserve consistent limits and source labels.
5. Non-organization users see only ancestor budgets permitted by their role and scope; modifying an inherited row does not bypass existing RBAC rules.
6. Regression covers simultaneous organization/project/environment/key budget enforcement and its presentation after a real persisted settlement or a deterministic isolated fixture.
