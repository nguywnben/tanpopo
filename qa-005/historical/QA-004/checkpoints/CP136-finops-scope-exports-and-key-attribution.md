# CP136 — FinOps scope, exports, and per-key attribution

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite at `127.0.0.1:18128`  
**Outcome:** scoped PASS for Org Owner/FinOps Viewer export parity and canary Developer authorization; FAIL for indistinguishable same-name Virtual Key chargeback rows (DEF-077). No writes, inference requests, or provider spend were made.

## Organization rollup and export journey

Used real Chromium sessions for the synthetic Org Owner and an existing synthetic FinOps Viewer. With active workspace set first to `QA-004 Isolated Canary / qa004-scope-canary-env` and then to `default / dev`, both roles saw the same organization usage summary. For `group_by=project`, the single `default` row reported 2038 exact micro-USD, 15 estimated micro-USD, 44 exact requests and 21 estimated requests. `group_by=env` returned the corresponding single `dev` row with identical totals. These match the RC6 organization-scope behavior already documented in CP18; the active-scope breadcrumb and per-scope budget list do not narrow Owner/FinOps Viewer chargeback.

Triggered actual CSV and JSON exports through the Chargeback UI for Project, Environment and Virtual Key. All returned HTTP 200. Project and Environment produced one row each. Virtual Key produced 42 rows, and totals reconciled with the summary cards. The sanitized results include row counts, sizes and export hashes. The UI screenshots for Project/Environment/Virtual Key and for both Viewer scopes were visually inspected. No defect is inferred from the Org Owner or FinOps Viewer aggregation because the tested scope matches the frozen RC6 contract.

## Project-scoped Developer authorization

Signed in as the existing Developer bound only to the canary project. The Console showed the canary scope and zero FinOps spend/budgets. The authenticated project listing returned only `qa004-isolation-canary`. `GET /admin/v1/usage/summary` returned zero rows for each supported grouping (`project`, `env`, `key`, `model`). A sibling `project_id`/`env_id` query against summary and JSON export still returned zero rows; requesting the sibling project's budgets returned HTTP 403 `insufficient_role_permission`. This is a scoped authorization PASS for the empty canary Developer; it is not full Developer-role acceptance.

## DEF-077 — same-name Virtual Keys cannot be reconciled

The Chargeback page's Virtual Key grouping showed 11 distinct `Playground session` rows, each with one exact request and 1 micro-USD. The CSV and JSON exports likewise had 11 duplicate records with the same `group_key` and charge figures. A separate authenticated Virtual Key catalog read returned 13 different key IDs with that same default name. Neither the summary row schema nor export includes a key ID or masked prefix, so the 11 chargeback rows cannot be mapped reliably to one credential. This is DEF-077, Moderate, FAIL; see the standalone report for detailed reproduction and acceptance criteria.

## Evidence

- [Sanitized role, scope, export and key-collision results](../evidence/CP136-finops-scope-and-key-attribution-results.json)
- [Owner: Project grouping in canary scope](../evidence/CP136-chargeback-group-project.png)
- [Owner: Environment grouping in canary scope](../evidence/CP136-chargeback-group-environment.png)
- [Owner: Virtual Key grouping and duplicate labels](../evidence/CP136-chargeback-group-virtual-key.png)
- [FinOps Viewer: chargeback in canary scope](../evidence/CP136-finops-viewer-chargeback.png)
- [FinOps Viewer: canary scope](../evidence/CP136-finops-viewer-canary.png)
- [FinOps Viewer: default/dev scope](../evidence/CP136-finops-viewer-default.png)
- [Developer: empty canary FinOps state](../evidence/CP136-developer-canary-finops-empty.png)
- [DEF-077 standalone report](../reports/DEF-077-chargeback-rows-do-not-identify-virtual-keys.md)

Campaign status after CP136: historical results remain 24 scoped PASS, six PARTIAL, one FAIL (DEF-025), and 19 UNVERIFIED. New findings are 26 FAIL (DEF-051–075 and DEF-077) and one PARTIAL (DEF-076). QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
