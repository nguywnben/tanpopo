# DEF-040 — Disabling an unverified provider is terminal without recovery guidance

- **Severity:** Moderate
- **Status:** Confirmed behavior / UX and recovery defect candidate; intended archival contract needs product-owner clarification
- **Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- **Affected workflows:** Providers → disable, probe, edit; Models → connection binding and deployment recovery
- **Checkpoint:** [CP07](../checkpoints/CP07-provider-failure-and-duplicate-identity.md)

## Reproduction

1. Create an Ollama connection to a private synthetic endpoint using the exact authorized CIDR `127.0.0.1/32`; let its first probe return 401 so it stays `CREATED_UNVERIFIED`.
2. Open its row action menu and click **Disable connection**.
3. Observe that the UI immediately changes the row to **Disabled**, without confirmation or terminal-state warning.
4. Click the same row's **Probe connection**. The API returns HTTP 422 `invalid_state_transition`, message `cannot test connection in state ARCHIVED`; the synthetic endpoint sees no new request.
5. Using an authenticated Admin API request with CSRF and current ETag, PATCH the connection state to `ACTIVE`. It returns HTTP 422 `invalid_state_transition`; message says it cannot modify the connection in `ARCHIVED`. Fresh list readback is still `DISABLED`.
6. Read the existing model deployment. It remains `PROVISIONED` and retains the same connection ID, while `connection_state=DISABLED`.

The same-name second test connection stayed `CREATED_UNVERIFIED`; action targeting itself was correct. Evidence includes [disabled-state screenshot](../evidence/CP07-provider-disable-confirm.png), [archived Probe failure screenshot](../evidence/CP07-probe-archived-fails.png), [deployment readback screenshot](../evidence/CP07-final-models-state.png), [reactivation API response](../evidence/CP07-provider-disable-reenable.json), and [Probe response/fixture call count](../evidence/CP07-probe-archived-fails.json). All screenshots were visually inspected. The fixture received no request after the connection was archived.

## Expected vs actual

- **Expected:** the product either provides a supported safe recovery path, or clearly warns that disabling an unverified connection permanently archives it and gives replacement steps. Dependent deployments should make their unusable backing connection visible and explain recovery.
- **Actual:** a row action that reads as Disable permanently archives an unverified connection. The row is shown as Disabled, but neither Probe nor reactivation can proceed. An attached deployment stays Provisioned in the Console despite the disabled connection.

## Business impact

A user can strand a provider configuration and its deployment with a single action. The supported replacement procedure and its effect on routes are not established. The user may need to recreate the provider and deployment, then repair route targets. This remains an inference until that workflow is independently tested.

## Facts vs hypotheses

**Facts:** the UI action is immediate; API readback is Disabled; Probe and activation are rejected with 422 because the internal connection state is ARCHIVED; no fixture request occurs; deployment readback contains `state=PROVISIONED` and `connection_state=DISABLED`.

**Hypothesis:** permanently archiving an unverified draft may be intentional. The product contract does not appear in the user-visible interaction. Even if intentional, lack of warning and recovery guidance plus the misleading Provisioned deployment display are unresolved UX concerns.

## Acceptance criteria

1. Define and document whether disabling a connection is reversible, and distinguish disable from permanent archival in the UI and API contract.
2. If reversible, make the reactivation action available to authorized roles and require successful validation before healthy deployment activation.
3. If terminal, ask for explicit confirmation that names the permanent effect, dependent deployment impact, and supported replacement steps.
4. Clearly communicate that dependent deployments cannot serve while their connection is disabled/archived; provide an actionable recovery path.
5. Verify audit history, role boundaries, route behavior, and successful recovery after endpoint/credential repair.

## Remaining uncertainty

No successful provider connection was disabled in this test. No route or inference request was sent. A create-replacement → create-deployment → route-repair workflow has not been tried. The issue is limited to a fresh isolated SQLite runtime and the tested Owner role/browser.
