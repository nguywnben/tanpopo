# DEF-056 — Team Admin model registration is denied after the Console offers it

**Severity:** Major  
**Status:** FAIL  
**Baseline:** frozen RC6, `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`

## Summary

In the `default / dev` workspace, a credentialed `TEAM_ADMIN` sees and can submit the Console's **Register model** form, but `POST /admin/v1/models` returns HTTP 403 `insufficient_role_permission`. The documented SCR-05 contract explicitly grants `TEAM_ADMIN` model and deployment create/edit/delete access. This prevents project Team Admins from using the documented model-registration workflow. A separate Org Owner path has since completed synthetic embeddings setup and inference, so the capability itself is not universally blocked; the role-contract mismatch remains.

## Reproduction

1. Sign into the frozen RC6 Console as the project-scoped QA `TEAM_ADMIN` in `default / dev`.
2. Open **Models**. The page renders the **Register model** action.
3. Open it and enter display name `QA004 Synthetic Embeddings`, slug `qa004-embedding-synthetic`, and select the declared `embeddings` capability.
4. Submit the form.
5. Observe the red `insufficient role permission` message. The captured request is HTTP 403 with error code `insufficient_role_permission`; no model is created.

## Evidence

- `evidence/CP35-models-before.png` — real Console showing the action to this Team Admin.
- `evidence/CP35-model-register-form.png` — actual registration form.
- `evidence/CP35-model-register-result.png` — submitted form and visible permission error.
- `evidence/CP35-embeddings-and-model-rbac-results.json` — sanitized session role, captured API response/request ID, four capability-guard traces, zero fixture dispatches, and cleanup state.
- `evidence/CP35-embeddings-trace.png` and `evidence/CP35-embeddings-finops.png` — actual Traces and loaded FinOps screens, visually inspected.
- `evidence/CP35-access-key-revoked.png` — Access & Keys shows the temporary test key in `Revoked` state.

## Independent Org Owner comparison

CP39 showed that an Org Owner in a separate fresh-install RC6 runtime can create an embeddings-capable model (HTTP 201), while its display name is lost on readback (DEF-057). CP40 used the test-root runtime, where an Org Owner created the model and a healthy zero-rate deployment against the isolated synthetic Ollama adapter, configured a route, and completed one cookie-free `/v1/embeddings` request with a path-scoped PROVIDER key. The response and fixture record confirmed a vector and 5 provider-reported prompt tokens; the persisted trace and Chargeback both recorded one Exact zero-cost request. The key was revoked and replay returned 401. See [CP39](../checkpoints/CP39-owner-model-create-and-display-name.md), [CP40](../checkpoints/CP40-owner-embeddings-end-to-end.md), `evidence/CP39-owner-model-results.json`, and the CP40 evidence listed in that checkpoint.

This proves only that an Owner can complete one synthetic embeddings lifecycle in the separate configured test-root runtime. It does not authorize Team Admin writes or satisfy external-provider, batch, failure-state, or production adapter-signing coverage.

## Expected and actual behavior

**Expected:** A project-scoped Team Admin can register a logical model and deployment as SCR-05 specifies; the new model is persisted and available to route configuration. Authorization remains confined to the Team Admin's authorized project/organization.

**Actual:** The Console offers model registration, but the model-create request is rejected with HTTP 403. The model is absent from persisted catalogs.

## Business impact and affected workflows

- Team Admin model registration and model/deployment administration.
- Synthetic or customer onboarding when a project administrator must add a model before creating a deployment, route, or key.
- Embeddings and other modality testing: this QA runtime's existing model is chat-only. Four real `/v1/embeddings` requests correctly failed closed with `400 capability_not_supported` and exact `$0`; the fixture received zero calls. The documented Team Admin model-registration step remains blocked by this defect. CP40 demonstrates an Owner-operated synthetic workaround, not equivalent Team Admin access.

## Facts and root-cause hypothesis

**Facts:** SCR-05 lists `ORG_OWNER, TEAM_ADMIN` for model and deployment mutations. The frozen Console's `ModelsPage` includes `TEAM_ADMIN` among `manageRoles` and renders the create action. The actual API response is a 403. Frozen RC6 `handleCreateAdminModel` authorizes `ActionModelWrite` against an organization-only target; the RBAC authorizer requires a matching project target for project-scoped bindings on non-list writes.

**Root-cause hypothesis:** The model-create handler's organization-only authorization target is incompatible with project-scoped Team Admin bindings. The implementation likely needs a documented scope-aware authorization design that preserves organization ownership and tenant isolation. Do not resolve this by broadening Team Admin to organization-wide privileges.

## Acceptance criteria

1. With a project-scoped `TEAM_ADMIN` who is documented to manage models, submit model creation from the Console and receive success; persisted API list/readback shows the selected slug and capability.
2. The same principal can complete an authorized deployment workflow only within its permitted project/environment.
3. A Team Admin scoped to a sibling project cannot mutate models or deployments outside that scope; cross-organization access remains indistinguishable from a missing resource as specified.
4. The UI exposes actions consistently with backend authorization. If product policy intentionally excludes a role, SCR-05 and UI affordances must be changed together through product contract review rather than returning a hidden 403 after submission.
5. Add API and browser coverage for Org Owner and Team Admin create/readback, project-boundary denial, and authorization error recovery.

