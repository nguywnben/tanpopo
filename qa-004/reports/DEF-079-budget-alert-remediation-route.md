# DEF-079 — Budget alert Remediate action opens Overview instead of FinOps Budgets

**Severity:** Moderate  
**Status:** FAIL  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite `127.0.0.1:18128`  
**Affected workflow:** responding to an active `ALT-BUDGET-80` alert in the Console

## Summary

The active-alert API returns `/budgets` as the budget alert's remediation route, and the Overview renders a **Remediate** button. Clicking it changes the browser address to `/budgets` but leaves Overview content and the Overview navigation item active. Reloading that path still renders Overview. The actual FinOps budget screen is reached through the FinOps sidebar at `/finops/budgets`.

## Reproduction

1. Sign in as the synthetic Org Owner in real Chromium and select `QA004 Test Root Org / default / dev`.
2. Open Overview and locate the active budget alert `ALT-BUDGET-80`.
3. Confirm `GET /admin/v1/alerts/active?limit=100` returns HTTP 200 and a row for this alert with `severity: TICKET` and `remediation_route: /budgets`.
4. Click **Remediate**.
5. Observe URL `/budgets`, heading `Overview`, and `aria-current="page"` on the Overview link. The alert remains visible.
6. Reload `/budgets`; it still renders Overview. After reselecting `default / dev`, click the FinOps sidebar; `/finops/budgets` correctly renders FinOps → Budgets & Holds.

Evidence: [sanitized route/result JSON](../evidence/CP141-budget-alert-route-results.json), [alert Overview](../evidence/CP141-overview-budget-alert.png), [failed destination](../evidence/CP141-budget-alert-remediation.png), and [manual FinOps navigation control](../evidence/CP141-finops-sidebar-control.png). Screenshots were visually inspected.

## Expected versus actual

- **Expected:** the remediation action from an active budget alert opens the FinOps budget management screen for that alert's organization/project context. The destination should also render correctly on direct navigation and refresh.
- **Actual:** it navigates to `/budgets` but displays the Overview page. The operator must discover and use the sidebar, then restore the target workspace scope if the path was reloaded.

## Severity and impact

**Moderate.** The user can eventually recover through the FinOps sidebar, but an operational alert's one-click remediation is broken and a copied/reloaded destination is misleading. This slows budget response and can leave a budget over its configured limit while the operator believes the action has opened remediation.

## Facts and root-cause hypothesis

**Facts:** the alert API returns `/budgets`; after button click the location is `/budgets`; Overview remains the active screen. Reload preserves this mismatch. Sidebar navigation reaches `/finops/budgets`. On reload, active workspace reverted from default/dev to the account-default Canary scope, matching the unresolved CP62 scope-persistence observation; it is not classified as a separate defect here.

**Hypothesis:** the alert route and Console router use different path conventions, or the `/budgets` path is falling through to the default Overview route. This needs source-level routing review; no product source was modified for QA.

## Acceptance criteria

1. `ALT-BUDGET-80` Remediate opens FinOps → Budgets & Holds for the alert's target scope.
2. Direct navigation and browser refresh on the remediation URL continue to render the budget screen, not Overview.
3. Workspace organization/project/environment scope is preserved or explicitly selected from alert metadata; it must not silently redirect the operator to an unrelated scope.
4. The active alert remains visible until its documented resolution condition is met, and the destination shows the relevant budget row and current usage.
5. Cover other alert remediation routes, desktop/mobile layouts, role authorization, and deep links.

No data writes or provider calls were made during CP141. QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
