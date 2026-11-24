# CP27 — Models exposes a sibling deployment under an empty project scope

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** DEF-035 is **PARTIAL — reproduced on RC5**. No deployment action was submitted, and no authorization bypass was attempted. `GATE-REL-07` remains **BLOCKED**.

## Reproduction and evidence

1. Signed in as the isolated Org Owner. The test organization contained `default/dev` with one synthetic deployment. It did not yet contain a second project.
2. Used the visible workspace selector’s **New project** flow to create `QA003 Empty Project DEF035` (`qa003-empty-def035`), then selected that project with `No env`. The project creation form and resulting scope were exercised in the real Console. This adds only a synthetic project to the isolated QA database.
3. Opened Models. The active scope title was `Default Organization / QA003 Empty Project DEF035 / No env`, and the project’s authenticated environment list was empty. Nevertheless, the page showed the deployment `qa003-synthetic-unauthorized` under “All environment deployments.” The visible row showed an opaque environment ID and offered **Disable deployment** and **Delete deployment**. Screenshots were visually inspected: [empty-project Models view](../evidence/CP27-models-empty-project.png), [row action menu](../evidence/CP27-models-empty-project-actions.png). Project-creation dialog evidence: [filled synthetic project form](../evidence/CP27-new-project-dialog-filled.png).
4. The browser observed `GET /admin/v1/deployments?limit=100` while the empty project was selected. Authenticated readback of `/admin/v1/projects` showed the empty project and `default`; `/admin/v1/projects/{empty-project-id}/environments` returned HTTP 200 with no environments; `/admin/v1/projects/{default-project-id}/environments` mapped the displayed `env_id` to `default / dev`. `GET /admin/v1/deployments?limit=100` returned the row with the matching `env_id`. Sanitized IDs, response statuses, scope, and requests are preserved in [evidence JSON](../evidence/CP27-models-empty-project-result.json).
5. No Disable/Delete action was chosen. No deployment, connection, key, route, or environment changed. The owner is authorized for both projects in this organization; this is a workspace-state and resource-identity defect, not an authorization bypass.

As a comparison, the same Org Owner selected `No project / No env`. Models labeled the view “All environment deployments,” which communicates that the list is broad, but the row still displayed the raw environment ID without its project name and retained Disable/Delete actions. See the visually inspected [organization-only inventory](../evidence/CP27-models-organization-scope.png) and [action menu](../evidence/CP27-models-organization-actions.png). This label reduces ambiguity in organization-only scope, but does not explain why the same broad list is presented while a specific empty project is selected.

## Expected versus actual

- **Expected:** When an empty project is selected, a sibling project’s deployment should not appear as part of that project’s inventory. If the product intentionally provides an organization-wide inventory at no-environment scope, the row should clearly identify its owning project and environment, and destructive actions should make that scope explicit.
- **Actual:** The empty-project scope still shows the sibling deployment, while the row omits project ownership, renders only the raw `env_id`, and exposes Disable/Delete. “All environment deployments” clarifies that the table is broad, but does not identify the owner or align the content with the selected project.

## Impact and uncertainty

The RC5 result reproduces the core scope/content mismatch previously observed on RC4. The explicit broad-table label reduces some ambiguity compared with an unqualified environment list, but an Org Owner looking at a named empty project can still see actionable controls for an unrelated project’s deployment without seeing that owner project in the row. No accidental disable or deletion was performed; operational impact is a credible risk, not an observed outage.

The database had only one pre-existing project, so no sibling set with multiple deployments was exercised. One synthetic empty project was created through the supported UI to make the cross-project state reproducible. Team Admin visibility, authorization behavior, pagination, delayed responses, and Disable/Delete confirmation target context remain unverified.

## Status and coverage

- DEF-035 moves from UNVERIFIED to **PARTIAL — reproduced on RC5**; the full acceptance criteria are not closed.
- `SCR-05` remains **PARTIAL**, with a confirmed workspace-scope cross-project leakage case.
- No new defect filed; the existing DEF-035 captures this behavior. No destructive action or security boundary was crossed.
- Historical dispositions now total 17 VERIFIED FIXED for scoped RC5 scenarios, 5 PARTIAL (DEF-005, DEF-007, DEF-032, DEF-035, DEF-039), and 17 UNVERIFIED. Scoped fixed results do not imply complete acceptance closure.
- Next regression work: test organization-only and active-environment filters, navigation and hard refresh, a second role, empty-state recovery, paging/delayed responses, and equivalent scope changes on sibling Console screens. Full Models/deployments lifecycle remains incomplete.
- QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
