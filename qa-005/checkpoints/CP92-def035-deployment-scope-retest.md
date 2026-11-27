# CP92 — DEF-035 deployment scope and ownership retest

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite at `127.0.0.1:18162`; Chromium 156; synthetic Org Owner; real authenticated Console.

## Result

**PASS (scoped QA-005 retest of DEF-035's unrelated-project row display).** At Organization / No project / No env, the Deployments UI displayed an explicit zero-row empty state. At `QA005 No Budget Project / No env`, the Console displayed five deployments; the list explicitly said “All environment deployments,” and each row identified `QA005 Budgetless Dev`. Its request explicitly included the selected project ID and returned exactly those five rows. At `Initial Project / dev`, the UI displayed five different deployments and the request explicitly included the Initial Project and `dev` scope. All five API rows belonged to the selected environment in each populated scope. No sibling-project row appeared at organization-only or project scope; no deployment action was submitted.

## Reproduction

1. Authenticate as the synthetic Org Owner and open Models & Deployments → Deployments.
2. Select organization `QA005 Org`, then `No project / No env`. Confirm the screen shows `All environment deployments 0` and `No deployments found`.
3. Select `QA005 No Budget Project / No env`. Confirm five rows appear, the page identifies the list as “All environment deployments,” each displayed environment is `QA005 Budgetless Dev`, and the underlying collection request includes the selected `project_id` and returns five rows with that project's environment ID.
4. In a separate real browser session, select `Initial Project / dev`. Confirm five rows, each tied by the request's `project_id`/`env_id` to that active scope.

Visually inspected: [organization-only empty state](../evidence/screenshots/CP92-def035-owner-org-only.png), [selected project without environment](../evidence/screenshots/CP92-def035-owner-project-noenv.png), and [Initial Project/dev control](../evidence/screenshots/CP92-def035-owner-initial-project.png). Sanitized row ownership, request query parameters/counts and limitations are in [CP92 JSON](../evidence/CP92-def035-deployment-scope-retest.json).

## Reconciliation and limits

This exact-RC7 Owner/SQLite/Chromium retest does not reproduce sibling-project deployments under an unrelated active workspace scope. Project-only / no-environment intentionally shows all deployments for that selected project and visibly identifies each environment. Historical QA-002 FAIL and QA-004 PARTIAL remain preserved; CP92 only passes the named current-scope isolation slice. It does not test Team Admin destructive actions, cross-organization resources, all roles, filter changes during in-flight requests, other backends, pagination or other browsers. At organization-only, the real UI made no general deployments-list request and showed an empty state; whether an organization-wide inventory should be shown is not generalized from this run. No writes, provider calls or paid usage. `GATE-REL-07` remains BLOCKED.
