# CP18 — Project-scope isolation and independent DEF-054 retest

**Outcome:** PARTIAL. Project-only Developer isolation held across the exercised scope picker and APIs. DEF-054 reproduced again on a fresh sibling project: Models displays a permission banner while its allowed model API returns a model. Owner aggregate metrics remain org-wide under project selection, which matches the RC6 Owner contract test; the FinOps summary-versus-active-scope labeling question is recorded without classifying a defect. GATE-REL-07 remains BLOCKED.

## Test data and actual Owner workflow

The existing synthetic Org Owner used the real scope picker to create project `QA-004 Isolated Canary` (slug `qa004-isolation-canary`). The project was immediately selected with no environment. The owner then opened Access & Keys → Members & Roles → Add member. The actual form defaulted `Developer` and Scoped projects to the canary; submitted synthetic `.invalid` account `qa004-scope-canary@example.invalid` and activated its one-time invite through the actual public invite UI. Invite secret/password stayed in mode-0600 `/tmp` files and are absent from this branch.

Owner persistence readback returns exactly the two test projects (`default`, canary) and confirms the canary account is a HUMAN, has only role `DEVELOPER`, is scoped only to `qa004-isolation-canary`, has a password, and no longer has pending enrollment. See `evidence/CP18-project-created.png`, `evidence/CP18-canary-add-member-scope.png`, and `evidence/CP18-owner-persistent-readback.json`.

## Project-only Developer scope and cross-screen evidence

Signed in as the new canary Developer using real Chromium. The visible workspace scope was `QA-004 Isolated Canary / No env`. Its scope menu showed the canary project, `No project`, and `No env`; it did not list the populated `default` project. This project-only scope survived refresh. Selecting `No project` did not expand access: Overview remained at zero and the authenticated project list still returned only canary. Refresh restored the Developer's only assigned project. Role boundary checks returned 403 `insufficient_role_permission` for organizations and provider connections. State transitions are recorded in `evidence/CP18-canary-developer-scope-refresh.json`.

Under the canary project binding, Overview showed zero requests and spend. The following authorized list endpoints returned zero data: usage summary, budgets, deployments, and routes. Traces showed zero recent requests. The model list endpoint returned HTTP 200 and one logical model, `qa004-mock`. On Models & Deployments, both the Deployments and Logical Models tabs displayed a red permission banner; Logical Models showed zero and “No logical models registered.” This independently reproduces DEF-054. After an Owner created a canary environment, the Developer selected it in the real scope picker and repeated Models and Routes: Models still showed zero despite the allowed model API returning one, while the authorized route list returned zero and the page still displayed the generic permission banner. Route content in this empty environment is correctly empty; the permission banner remains an inaccurate global state. Screenshots `CP18-canary-developer-env-models.png` and `CP18-canary-developer-env-routes.png` were visually inspected. Environment naming is documented separately in CP19.

This is a scoped PASS for the tested project isolation boundary and a FAIL retest of the DEF-054 UI state. It does not establish cross-organization isolation, a role's full API contract, or any route behavior with a selected environment.

## Owner aggregate versus selected project — evidence and uncertainty

With the Org Owner selecting the empty canary project, Overview remained at 13 requests / 12 settled micro-USD; the same `/admin/v1/overview/summary?window=24h` values were returned in `default` scope. Owner FinOps showed 11 exact plus 1 estimated micro-USD while the canary budget-ceiling list contained zero records. The endpoint sends the Org Owner's organization context rather than a project identifier. The frozen RC6 HTTP tests in `internal/admin/http/part2_test.go` explicitly expect a project-bound FinOps Viewer summary to be scoped and an Org Owner summary to aggregate at the organization level. Accordingly this is not classified as a data-leak defect.

The UI still shows an “Active workspace scope” project breadcrumb while Owner summary cards remain org-wide, and the FinOps page mixes org totals with an active-project budget list. The endpoint semantics appear intentional; whether the Console should label or separate these aggregation scopes is a business-contract/UX question. No defect is assigned without a user-facing scope requirement. Evidence is `evidence/CP18-owner-canary-finops.png`, `evidence/CP18-org-scope-overview.png`, and `evidence/CP18-owner-persistent-readback.json`.

## Remaining scope coverage

Sibling-project writes/reads, error recovery for project/environment creation, cross-org isolation, scope-switch refresh races, membership revocation, and owner FinOps totals under every scope remain UNVERIFIED. CP19 covers the environment name persistence defect and the limited environment selection retest. No product code was changed; all additions are QA evidence and reports.
