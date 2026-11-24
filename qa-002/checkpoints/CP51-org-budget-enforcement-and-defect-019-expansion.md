# CP51 — Organization budget enforcement and DEF-019 expansion

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** scoped enforcement PASS; DEF-019 FAIL/expanded; DEF-017 recurrence; DEF-024 remains open.  
**Release gate:** `GATE-REL-07 BLOCKED`.

## Scope and setup

On the existing isolated SQLite runtime (18086), an owner used real Chromium UI to create and probe an Ollama-compatible loopback provider, register a streaming logical model, create a healthy deployment, and create a healthy route in QA002 CP43 Secondary Project / `qa002-cp43-env2`. The fixture ran only on loopback and returned synthetic responses. No Google provider credentials, external provider, or production service were used.

A successful Playground inference returned HTTP 200 and settled exactly 2 micro-USD. The fixture observed one streaming request. The organization budget was then temporarily reduced in the FinOps UI from 1,000,000 to 1 micro-USD while settled usage was already 2 micro-USD. The next Playground request displayed `Playground inference failed (429).`; the persisted trace recorded `project_budget_exceeded`. Fixture request count remained one, confirming rejection before dispatch. There were no active holds.

The organization cap was restored through the UI to 1,000,000. Database state confirmed the restored limit and both temporary ephemeral Playground keys were revoked using Access & Keys. Screenshots were visually inspected, including the generic rejection and final revoked key state.

## Defect disposition

- **PASS — organization ceiling enforcement (scoped):** a child project/environment request was rejected before upstream dispatch when organization settled spend exceeded its ceiling.
- **FAIL — DEF-019 expanded:** the Playground rendered only HTTP 429 even though persisted trace data held the canonical `project_budget_exceeded` reason. The architecture contract and source comment identify that common canonical code for organization/project/environment/key budget enforcement; this is not a scope-label defect. Add this org-scope case to the existing DEF-019, do not create another defect.
- **DEF-017 recurrence:** the successful Playground card again remained `Trace metadata pending…`.
- **DEF-024 remains confirmed:** CP51 exercised backend enforcement but did not make the applicable organization ceiling visible in project-scoped FinOps.

## Reproduction

1. In the isolated org/project/environment, create and probe a synthetic Ollama loopback connection, register a streaming model, create a healthy deployment and route.
2. With the organization budget at 1,000,000 micro-USD, send a synthetic Playground request. Observe a 200 response, one fixture POST and exact settlement.
3. In organization scope, lower the hard limit to 1 micro-USD after settled usage is 2 micro-USD.
4. Send another Playground request through the same project/environment route. Observe a generic UI 429; confirm the persisted trace error code is `project_budget_exceeded` and fixture count did not increase.
5. Restore the original cap and confirm it through FinOps/API. Revoke the temporary Playground keys and verify the UI/database show both revoked.

## Evidence

- `../evidence/CP51-org-budget-enforcement.json` — sanitized setup, response, trace, fixture counts, restoration and cleanup.
- `../evidence/CP51-budget-positive-result.png`, `CP51-org-budget-lowered.png`, `CP51-budget-rejected-result.png`, `CP51-org-budget-restored.png` — visually inspected live states.
- `../evidence/CP51-provider-probe-result.png`, `CP51-deployment-created.png`, `CP51-route-created.png` — visually inspected integration setup.
- `../evidence/CP51-keys-before-revoke.png`, `CP51-keys-revoked.png` — visually inspected cleanup.

## Acceptance / residual coverage

DEF-019's existing acceptance criteria apply: safely identify the canonical reason and budget scope/action when available; distinguish governance limits from provider rate limits and other failures; retain generic safe fallback; prove pre-dispatch rejection and recovery. Continue testing simultaneous org/project/environment/key caps, provider 429, mixed rejection types, other roles, other browsers, and recovery/navigation states. The organization FinOps visibility problem DEF-024 remains separately unresolved.

QA-002 remains in progress. No implementation was changed and no release approval is implied.
