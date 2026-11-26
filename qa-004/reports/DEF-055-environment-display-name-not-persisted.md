# DEF-055 — Environment display name is lost after creation

**Severity:** Moderate  
**Status:** FAIL (confirmed on frozen RC6)  
**Area:** Project/environment lifecycle, workspace selection, operational UX

## Summary

The real Console asks for separate `Environment name` and `Environment slug`. Creating an environment with distinct values returns the requested name in HTTP 201, but the next environment-list response substitutes the slug as `name`. The workspace picker then displays the slug rather than the configured human-readable name. The value is lost between create and readback, so a user cannot rely on the name they entered to distinguish environments in the main scope selector.

## Reproduction

1. As the synthetic Org Owner, open the workspace selector, select the isolated canary project, and choose **New environment**.
2. Enter `QA-004 Human Label Check` for **Environment name** and `qa004-human-label-check` for **Environment slug**. Submit in the real Console.
3. The actual `POST /admin/v1/projects/{projectId}/environments` body contains both distinct values. The response is HTTP 201 and returns `name: "QA-004 Human Label Check"`, `slug: "qa004-human-label-check"`.
4. Fetch `GET /admin/v1/projects/{projectId}/environments`. It returns HTTP 200 but the matching row has `name: "qa004-human-label-check"`, `slug: "qa004-human-label-check"`.
5. Reopen the actual workspace picker. Its environment options display `qa004-scope-canary-env` and `qa004-human-label-check`; neither uses its entered human-readable name.

Evidence: `evidence/CP19-environment-create-form.png`, `evidence/CP19-env-human-name-reproduction.json`, `evidence/CP19-env-human-name-displayed.png`, and `evidence/CP19-environment-picker-slug-options.png`. The real screenshots were visually inspected. A prior independent environment creation in CP18 also read back and displayed its slug; the second case records the exact distinct request, create response, and subsequent list response.

## Independent QA-004 CP55 retest

The same discrepancy was reproduced again on frozen RC6 artifact `45581179274` through a separate real-browser project/environment journey. The Owner submitted `QA004 CP55 Staging West` / `qa004-cp55-staging-west`; POST returned HTTP 201 with both distinct fields, while the subsequent project-scoped GET returned `name` equal to the slug, and the refreshed picker displayed only the slug. Screenshots and sanitized request/readback evidence are in [CP55](../checkpoints/CP55-project-environment-provisioning-and-audit-retest.md). CP57 then corrected a prior duplicate-slug failure in the same form; the environment create recovered successfully, but the active scope and subsequent readback still used the slug instead of the submitted human label ([recovery evidence](../checkpoints/CP57-corrected-provisioning-recovery.md)). The sibling project created in that journey did preserve its distinct display name and slug through project list readback, isolating the observed loss to environment metadata. This is a repeat confirmation of DEF-055, not a new defect.


## Independent QA-004 CP59 retest

In a separate project-scoped Owner session, the real workspace selector opened a new environment form. I entered `QA004 CP59 MCP Test` and `qa004-cp59-mcp-test`; the POST returned 201 and echoed the distinct values. A subsequent authenticated GET returned `name: qa004-cp59-mcp-test`, `slug: qa004-cp59-mcp-test`, and the refreshed top-bar scope picker displayed the slug. The environment was created to support an authorized isolated MCP lifecycle, and remains scoped to the QA project. Sanitized evidence: [CP59 result](../evidence/CP59-mcp-authorized-lifecycle.json), [create form](../evidence/CP59-mcp-environment-create-form.png), and [post-create scope](../evidence/CP59-mcp-environment-created.png). This is another independent reproduction of DEF-055, not a new finding.

## Independent QA-004 CP107 PostgreSQL retest

On a separate PostgreSQL 16-backed RC6 runtime, the synthetic Org Owner created `QA004 CP107 PostgreSQL Scope` / `qa004-cp107-pg-scope` and then `QA004 CP107 Quality Assurance` / `qa004-cp107-qa` through the real Console. The project list retained the human-readable project name. The environment POST returned HTTP 201 with the requested display name and slug; the subsequent project-scoped GET returned HTTP 200 with `name == slug`, and after browser reload the actual scope picker showed only `qa004-cp107-qa`. The Audit Log showed targeted `project.created` and `environment.created` events and verified the chain intact across 12 events. Screenshots and sanitized API evidence are in [CP107](../checkpoints/CP107-def055-postgres-backend-retest.md). This confirms the same existing defect across PostgreSQL as well as SQLite; it does not add a defect ID.

## Facts and root cause

The Create form has separate required name and slug fields in `web/console/src/shell/ConsoleShell.tsx`. The frozen API request type accepts `name`. `handleCreateProjectEnvironment` validates both values and returns the input name in its create response, but passes only `Slug` to storage. `handleListProjectEnvironments` then populates DTO `Name` from `e.Slug`. This explains the observed 201-versus-GET discrepancy; no root-cause speculation is needed for this behavior.

## Expected and actual behavior

- **Expected:** The entered environment name and stable slug remain separate. Create, list, workspace selection, refresh, and project/environment switching show a consistent name/slug pair.
- **Actual:** Create briefly returns the human name. Persistent list/readback reports `name == slug`, and the workspace selector exposes only the slug.

## Business impact

Operators often use descriptive environment names to distinguish production, staging, development, or regional workspaces while retaining short URL-safe slugs. Losing the label makes environments harder to identify and increases the chance of selecting the wrong one before managing routes, keys, budgets, A2A/MCP access, or other environment-bound settings.

## Acceptance criteria

- Persist the human-readable environment name separately from its slug.
- After creation, `GET /admin/v1/projects/{projectId}/environments` returns the same name and slug supplied to POST; the values remain stable after refresh and a new authenticated session.
- The workspace selector shows the human name and retains enough slug context to disambiguate duplicate-looking labels.
- Existing environments without a stored display name remain usable with a documented slug fallback.
- Regression coverage creates multiple environments with distinct names/slugs and switches between them through the real UI; the selected environment remains correct on page navigation and reload.
- Preserve slug uniqueness, authorization, scope boundaries, and audit behavior.

## Coverage still open

Only a small number of synthetic environments in isolated projects were exercised across SQLite and PostgreSQL. Rename/update/delete behavior, duplicate and invalid names/slugs, migration of preexisting environments, localization of long labels, cross-project scope changes, async switching races, and route/key/budget isolation across two populated environments remain UNVERIFIED.
