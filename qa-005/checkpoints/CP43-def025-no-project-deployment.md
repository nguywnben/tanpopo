# CP43 — DEF-025 no-project deployment retest

**Classification:** PASS (scoped QA-005 retest)  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`, unsigned RC7 exact-source runtime  
**Environment:** isolated SQLite runtime `127.0.0.1:18145`, Chromium, QA005 Org Owner

## Reproduction and result

1. Sign in as the QA005 organization owner.
2. Select `QA005 Org / No project / No env`.
3. Open Models & Deployments and select **Add deployment**.
4. Inspect the deployment drawer and observe network requests.

The empty list displays “No deployments found.” The drawer explicitly says no environments exist in the active project and instructs the user to create one in the top bar first. **Save deployment** is disabled, and the browser recorded zero deployment POSTs. Both screenshots were visually inspected: [models empty state](../evidence/screenshots/CP43-no-project-models.png) and [prerequisite drawer](../evidence/screenshots/CP43-no-project-deployment-form.png).

This reverses historical RC6 DEF-025 behavior for this tested no-project Owner flow. Scope is limited to this runtime, role, and state; other roles, project scopes, storage backends, assistive technology, and error recovery remain unverified. Detailed machine-readable evidence: [CP43 JSON](../evidence/CP43-def025-no-project-deployment.json).
