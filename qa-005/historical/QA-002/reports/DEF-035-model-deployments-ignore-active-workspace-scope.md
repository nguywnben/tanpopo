# DEF-035 — Models exposes sibling-project deployments without scope identity

**Severity:** Moderate
**Status:** Confirmed on RC4, frozen baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Affected workflow:** Models & Deployments review and deployment lifecycle for organization owners, especially after switching to organization-only or a project without environments.
**Security classification:** No authorization bypass observed. The signed-in Org Owner was authorized to read and administer the organization’s deployments. This is an active-workspace and business-state consistency defect.

## Reproduction

1. Sign in as an Org Owner to a workspace containing an empty project `QA002 CP78 Fresh Budget` and another project `QA002 Audit mwto47wa / stage` with three deployments.
2. Switch the workspace selector to `No project / No env`, then open **Models**. The page renders three rows under **Environment deployments**.
3. Switch to `QA002 CP78 Fresh Budget / No env` and revisit Models. The same three rows remain.
4. Inspect deployments through the authenticated Admin API. `GET /admin/v1/deployments?limit=100` returns three rows, each with `env_id=env_01m84ymb8ae00b43av7shak1sf`, which maps to `QA002 Audit mwto47wa / stage`. The selected CP78 project has no environments. The UI itself requested the unfiltered deployment list (`?limit=100`).
5. Open the first foreign-scope row action menu while CP78 / No env is selected. The menu offers **Disable deployment** and **Delete deployment**. No action was chosen and no mutation was sent.
6. Select `QA002 Audit mwto47wa / stage`. The same three rows are then in the selected environment, where they belong.

Evidence: [CP95 checkpoint](../checkpoints/CP95-console-workspace-scope-and-model-deployment-identity.md), [scope/API evidence](../evidence/CP95-models-empty-project-readonly.json), [environment ownership/API map](../evidence/CP95-model-deployment-scope-api.json), [read-only action-menu screenshot](../evidence/CP95-models-empty-project-row-actions.png), [organization-only screen](../evidence/CP95-org-models.png), [empty-project screen](../evidence/CP95-proj-models.png), and [owning-environment screen](../evidence/CP95-models-stage-environment.png). Screenshots were visually inspected.

## Expected versus actual

- **Expected:** Models should show deployments belonging to the selected project/environment, or clearly identify an intentional organization-wide view by showing each deployment’s owning project and environment and making the scope of destructive actions explicit. With an empty selected project, unrelated deployments should not be presented as that project’s environment deployments with unqualified Disable/Delete actions.
- **Actual:** organization/project-empty selections show all organization deployments, omit owning project/environment columns, and enable row actions. The rows belong to a different project’s `stage` environment.

## Business impact

An owner reviewing or administering an empty project can mistake a sibling project’s deployment for a local resource. Disable/Delete are actionable from this ambiguous state, creating a credible path to unintended interruption or removal. No such mutation was performed; impact is based on the visible action affordance and API ownership evidence.

## Root-cause hypothesis (not established fact)

The Models page appears to load the organization-wide deployment collection without the selected `env_id`, and the table omits environment/project ownership. This is consistent with the observed request and UI. Engineering should confirm the intended organization-owner list contract before choosing whether to filter or add explicit scope identity.

## Acceptance criteria

1. For an active environment, only deployments for that environment are shown, unless the page explicitly enters a clearly labeled organization-wide mode.
2. With no environment selected, no sibling deployment is presented as belonging to the current project; any organization-wide inventory displays owning project and environment.
3. Disable/Delete actions clearly identify the target deployment and owning scope, and cannot be triggered from an ambiguous workspace state.
4. Switching between org, project, and environment scope, navigating away/back, and refreshing yields coherent rows and action context.
5. Regression coverage proves that owner authorization remains valid within the intended scope and does not weaken RBAC.

## Boundaries

Tested only with one Org Owner, one empty project, one environment, and read-only action-menu inspection. Team Admin/project-limited authorization, actual disable/delete lifecycle, pagination, delayed responses, and other role contexts remain UNVERIFIED. This report does not claim an authorization bypass.
