# CP77 — DEF-025 deployment provisioning at organization-only scope

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated A2A-enabled RC6 test clone `127.0.0.1:18128`  
**Role/browser:** synthetic Org Owner, real Chromium at 1440×1000  
**Product source modified:** no

## Result

**FAIL — historical DEF-025 reproduced on exact RC6.** At `QA004 Test Root Org / No project / No env`, Models showed an enabled **Add deployment** action. The opened drawer allowed selection of a logical model and provider connection but exposed no environment selector. After supplying the required upstream model name, choosing the fixture-compatible self-hosted zero-rate option, and clicking **Save deployment**, the form displayed “Select an active environment, logical model, and provider connection.” No `POST /admin/v1/deployments` was sent, and the deployment list remained at two rows.

The global workspace picker is a way to select a project/environment only by leaving the drawer and changing scope; the drawer provides no in-form recovery path from this validation state. This blocks or confuses a user who intentionally starts deployment setup at organization-only scope. The behavior matches the historical report; no authorization bypass or persistence corruption was observed.

## Reproduction

1. Sign in as the synthetic Org Owner in the isolated exact-RC6 runtime.
2. Select `QA004 Test Root Org / No project / No env`.
3. Open **Models**. The organization-level inventory shows one logical model and zero environment deployments; **Add deployment** is enabled.
4. Open **Add deployment**. Observe the logical-model and provider-connection selectors, required upstream model, pricing inputs, and optional self-hosted zero-rate checkbox. There is no environment selector.
5. Enter `qa004-mock` as the upstream model name, check the zero-rate option for the synthetic `SELF_HOSTED_INFRA` provider, and click **Save deployment**.
6. Observe the error and inspect outgoing requests and the deployment list.

The [initial drawer screenshot](../evidence/CP77-model-deployment-drawer-no-scope.png) and [validation screenshot](../evidence/CP77-model-deployment-no-env-validation.png) were visually inspected. Sanitized request/inventory evidence is in [the no-scope results](../evidence/CP77-model-deployment-no-env-submit.json). The earlier screenshot includes only synthetic model/provider labels; no credential was visible.

## Expected versus actual

- **Expected:** from a scope without an active environment, the user can select a valid environment in the drawer, or the Add deployment action is clearly disabled with an actionable route to select scope.
- **Actual:** Add deployment is enabled, no environment picker is present, and Save produces an error requiring an active environment without a way to select one in the drawer. No create request occurs.
- **Impact:** the deployment-registration workflow dead-ends when initiated at organization-only scope; the user must understand that they need to close the form and change the global scope, which is not disclosed in the error.

## Root-cause hypothesis

**Hypothesis only:** the drawer consumes the selected workspace environment as an implicit required value but does not provide a local environment selection control or disable its entry point when that value is absent. This test confirms the UI interaction and validation; it does not establish implementation cause.

## Acceptance criteria

1. In organization-only or project-only/no-environment scope, a user can choose an authorized active environment within the deployment form and complete registration; **or** the entry action is disabled with clear, actionable guidance and an obvious way to set the required scope.
2. In environment scope, existing behavior remains consistent and submits the correct environment ID.
3. Missing/inactive environment errors appear next to the relevant control and preserve all user-entered model, provider, and pricing values for recovery.
4. A successful retry creates exactly one deployment, updates the visible list, and persists the same environment/model/provider/rate fields after refresh.
5. Other roles receive the correct permission error without stale form state, and rapid scope changes cannot submit against an unintended environment.

QA continues; **GATE-REL-07 remains BLOCKED**.
