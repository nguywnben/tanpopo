# DEF-033 — Create budget overwrites a matching ceiling once, then submits a stale revision

- **Severity:** Moderate
- **Status:** Confirmed
- **First reproduced:** CP78, frozen RC4 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`
- **Affected workflow:** FinOps → Budgets & Holds → Create budget for a project/period that already has a budget
- **Environment:** isolated runtime `127.0.0.1:18084`; real Chromium; authenticated Org Owner; synthetic QA project with no inference or active holds

## Reproduction

1. Select project `QA002 CP78 Fresh Budget` and open FinOps → Budgets & Holds. The project has a monthly ceiling for 2,500,000 micro-USD, version 1. Screenshot: `evidence/CP78-existing-budget-before-create.png`.
2. Click **Create budget**. Keep Scope = Project, Target = the already budgeted project, Period = MONTHLY. Set the hard limit to 3,000,000 and submit.
3. The Console shows **Created budget ceiling**. The same row now contains 3,000,000. The API shows the same deterministic budget ID at version 2. Screenshots: `CP78-existing-budget-create-form.png` and `CP78-existing-budget-after-create.png`.
4. Open **Create budget** again for the same project and period, set 2,800,000 and submit. The Console sends `PUT /admin/v1/budgets/{same-id}` with `If-Match: "v1"` although the current resource is version 2. API responds 409 `optimistic_lock_conflict`, `current revision is v2`. The error banner is behind the still-open drawer; after dismissing the drawer it becomes visible. Evidence: `CP78-budget-duplicate-conflict.json`, `CP78-budget-duplicate-conflict-drawer.png`, and `CP78-budget-duplicate-conflict-dismissed.png`.
5. Restore the original 2,500,000 ceiling through the row's Save action. The API returns 200/version 3 and the screenshot confirms the restored value. Evidence: `CP78-budget-duplicate-conflict.json` and `CP78-budget-duplicate-restored.png`.

No inference, hold, settlement, or provider request was made during the test.

## Expected and actual behavior

- **Expected:** the Create flow should detect that a budget already exists for the selected scope and period and prevent an ambiguous overwrite, or clearly transition to an update flow using the current resource version with accurate confirmation and success copy. Conflict feedback should remain visible in the drawer.
- **Actual:** the first submission overwrites the existing 2.5M ceiling with 3.0M and reports it as created. A subsequent submission uses fixed revision `v1`, fails with 409 against current revision `v2`, and leaves its error behind the drawer.

## Business impact

A user can unintentionally raise or lower an existing hard ceiling through a control labelled Create, changing enforcement without an explicit update confirmation. After one write, repeating the same user workflow fails due to a stale version precondition. The backend rejects the stale write safely; CP78 observed no unauthorized action or spend. The cap was restored before ending the test.

## Facts and hypotheses

**Observed facts:** before the first submission the row was 2,500,000; after submission it was 3,000,000 and the same budget ID had version 2; the UI success copy said Created. On the next attempt, the request carried `If-Match: "v1"`, the API returned 409/current revision v2, the displayed error was obscured by the drawer, and no state change occurred. A separate clean-project first-budget flow succeeded at 2,500,000 and persisted across reload.

**Hypothesis:** the Console constructs a deterministic ID from scope and period and hardcodes the create precondition to v1, so it behaves as an upsert for a matching v1 record and conflicts afterward. The source implementation is a lead, not the basis for the finding; the report is based on browser/API behavior.

## Acceptance criteria

1. When scope + period already has a budget, the UI does not silently overwrite its hard limit from the Create flow.
2. The UI either blocks the duplicate with an inline explanation and link/focus to the existing row, or clearly switches to an Update flow showing the existing value and requiring explicit confirmation.
3. A valid update uses the current resource version and reports **Updated**; a concurrent conflict refreshes or presents the authoritative current value without hiding the message.
4. Regression covers a new budget, duplicate same-scope/period, two sequential updates, a stale concurrent write, and persistence after refresh.

## Evidence and limits

Sanitized API history: `evidence/CP78-budget-duplicate-conflict.json`; real UI screenshots are listed above. CP78 restored the tested project's limit to 2,500,000 micro-USD at version 3. The finding is limited to the project/monthly UI path; organization, environment and virtual-key variants remain unverified. `GATE-REL-07` remains **BLOCKED**.
