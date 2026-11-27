# DEF-025 — Deployment drawer cannot recover from missing environment scope

**Severity:** Low  
**Status:** FAIL on frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Checkpoint:** [CP77](../checkpoints/CP77-def025-model-deployment-no-environment.md)

## Expected

A deployment workflow started at organization-only or project-only/no-environment scope should let an authorized user select an active environment in the form, or should prevent opening the form and explain how to select one.

## Actual

At `QA004 Test Root Org / No project / No env`, **Add deployment** is enabled. The drawer has no environment selector. A fully populated synthetic zero-rate deployment submission shows “Select an active environment, logical model, and provider connection” but does not identify a control that can resolve the missing environment. No deployment POST is made, and inventory remains unchanged.

## Reproduction

1. Sign in as Org Owner at the isolated exact-RC6 runtime.
2. Set the active scope to an organization with **No project / No env**.
3. Open **Models → Add deployment**.
4. Select the synthetic logical model/provider, enter the required upstream model name, and configure zero-rate self-hosted pricing.
5. Click **Save deployment**; observe the error and confirm there is no `POST /admin/v1/deployments` and no list change.

## Impact

The user cannot finish deployment registration from the scope where the action is offered. Recovery requires leaving the drawer and discovering that the global picker must first select an environment; the UI does not explain this step.

## Evidence

- [Initial drawer, no environment selector](../evidence/CP77-model-deployment-drawer-no-scope.png)
- [Validation state after Save](../evidence/CP77-model-deployment-no-env-validation.png)
- [Sanitized request and inventory record](../evidence/CP77-model-deployment-no-env-submit.json)

## Root-cause hypothesis, not fact

The drawer may rely on ambient workspace scope for its environment ID while allowing entry without one. Source-level root cause is not asserted.

## Acceptance criteria

- Provide a clear environment selector in the form, or disable the entry point and clearly guide users to select the required scope.
- Keep field-level error copy actionable and preserve entered values.
- Verify successful create and refresh persistence in selected environment scope.
- Verify permissions, inactive environment handling, and scope-change races.
