# DEF-025 — Add deployment opens a scope-less form with no active environment

- **Severity:** Low (P3)
- **Status:** Confirmed UI usability defect; no deployment mutation occurred
- **First evidence:** CP44
- **Affected workflows:** organization-only/project-without-environment workspace → Models & Deployments → Add deployment
- **Affected roles:** users allowed to manage model deployments, including ORG_OWNER

## Reproduction

1. Sign in to the isolated Tanpopo runtime as an ORG_OWNER and choose `No project / No env`.
2. Open **Models & Deployments**. It displays zero environment deployments and the top-right **Add deployment** button is enabled.
3. Click **Add deployment**. The drawer asks for logical model, provider connection, upstream model name, and pricing. It contains no environment/workspace selector; the global scope selector is blocked by the open drawer.
4. Click **Save deployment** without changing scope. The form remains open and a page-level banner says “Select an active environment, logical model, and provider connection.”
5. Inspect requests: the page makes GETs for models, deployments, and connections; submitting the unavailable form produces no POST/PUT and creates no deployment.
6. Screenshot evidence, visually inspected: `../evidence/CP44-org-only-add-deployment-drawer.png` and `../evidence/CP44-org-only-add-deployment-validation.png`.

## Expected vs. actual

- **Expected:** When no environment is active, the page should disable the action or guide the user to select an environment before opening a deployment form. If the form opens, its active environment must be visible/selectable, and validation should be presented within the form.
- **Actual:** The action is enabled; the drawer offers no environment selector; the only environment-specific feedback appears as a page-level banner behind the drawer after Save. The user must dismiss the form, change global scope, and reopen it.

## Impact

A user starting from organization-only/project-without-environment context enters a form that cannot satisfy one of its required conditions. The user can recover by closing the drawer and selecting a project/environment; no resource is created or authorization boundary weakened. This is a small but reproducible UX dead end and is not a backend defect.

## Root-cause hypothesis (not verified)

The page-level CTA and form submission guard do not share the same `environmentId` precondition. Other environment-bound actions disable themselves when the scope is absent. The form reports the missing scope, but the global selector is inaccessible behind the drawer.

## Acceptance criteria

1. With no active environment, the primary action is either disabled with an adjacent selection prompt or opens a flow that provides a usable project/environment selection step.
2. A valid selected environment is visibly identified before save.
3. Missing-scope validation is associated with the open form and points to a reachable corrective action.
4. No create request is sent while the environment is absent; valid selection permits the normal, authorization-checked create flow.
5. Regression covers org-only, project/no-environment, and project/environment states.
