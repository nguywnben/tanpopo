# CP05 — Create the first budget from the Console

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Status:** PASS for project-budget creation and persistence in the tested owner workflow.

Logged in to the isolated 18083 runtime as its synthetic Org Owner and opened FinOps → Budgets & Holds. The initial view reported zero budget records and displayed the empty-state Create budget action. I visually inspected the empty screen and the open creation drawer. I created a monthly project budget with a 2,500,000 micro-USD ceiling. After reloading and signing in in a separate browser context, the UI rendered the row; a fresh authenticated `GET /admin/v1/budgets` returned one matching project record at version 1 with the expected scope and ceiling. Screenshots `evidence/CP05-finops-empty.png`, `evidence/CP05-finops-create-form.png`, and `evidence/CP05-finops-current.png` were visually inspected.

The original DEF-003 Console blocker did not reproduce on this baseline. This result does not verify the other budget scopes, editing, enforcement/reservation behavior, or FinOps exports. A short initial capture attempt had a Playwright locator/wait issue; it was a harness timing/locator issue, not a product error. The evidence above comes from the subsequent actual browser journey and independent re-login read.

Machine-readable UI/API state: `evidence/CP05-finops-budget-create.json`.
