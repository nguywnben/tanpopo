# CP19 — Environment display-name persistence

**Outcome:** FAIL for environment metadata persistence; PARTIAL for environment lifecycle. New confirmed Moderate defect DEF-055. The create response returns the human-readable name, but subsequent list/readback and the workspace selector replace it with the slug. GATE-REL-07 remains BLOCKED.

## Real-user reproduction

The synthetic Org Owner used the real workspace selector to open **New environment** in `QA-004 Isolated Canary`. The form presents separate required **Environment name** and **Environment slug** fields. Submitted distinct values:

- Name: `QA-004 Human Label Check`
- Slug: `qa004-human-label-check`

The real POST request body preserved both values and returned HTTP 201 with the human name. A subsequent authenticated environment list returned HTTP 200 but set both `name` and `slug` to `qa004-human-label-check`. The real workspace picker displayed the slug. A first canary environment shows the same slug-as-name behavior in CP18. Request, create response, readback, and UI scope are in `evidence/CP19-env-human-name-reproduction.json`; `CP19-environment-create-form.png`, `CP19-env-human-name-displayed.png`, and `CP19-environment-picker-slug-options.png` are the actual inspected screenshots.

## Classification and source facts

This is a product defect, not a Cloud limitation. The UI explicitly distinguishes a human name and slug. The create handler validates both but passes only the slug to storage; its immediate response uses the submitted name. The list handler fills `name` from the persisted slug. This source behavior exactly matches the independent black-box create/list/UI discrepancy. Root cause is established in the frozen RC6 source; no implementation was changed.

The isolated project-only Developer was then moved into a selected canary environment. Its allowed `GET /admin/v1/models` still returned one model while the real Logical Models tab showed zero with `insufficient role permission`. The authorized route list returned zero and its real Routes page showed zero with the same generic banner. This extends DEF-054 under a selected environment; see `CP18-canary-developer-env-models.png`, `CP18-canary-developer-env-routes.png`, and the DEF-054 report. Data isolation remained intact.

## Defect and acceptance

DEF-055 is **Moderate / FAIL** because environment names help distinguish deployment contexts before an operator manages environment-bound routes, keys, budgets, A2A/MCP access, and related settings. The create response, GET list, and picker should preserve a consistent separate name and slug across refresh and new sessions. See [the standalone report](../reports/DEF-055-environment-display-name-not-persisted.md) for reproduction, impact, source facts, and complete acceptance criteria.

## Remaining coverage

Only two synthetic, empty environments in one isolated sibling project were created. Rename/delete, invalid and duplicate values, migration of existing environment metadata, localized long names, and populated cross-environment isolation remain UNVERIFIED. No product code or live-provider configuration changed; no external spend occurred.
