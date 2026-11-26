# CP12 — Project-scoped Developer journeys and catalog consistency

**Outcome:** PARTIAL RBAC/workflow coverage. A real project-scoped Developer completed a synthetic Playground request, read its persisted trace, and ran Route Explain. New confirmed defect DEF-054 affects model/route inventories. Five other canonical roles and broader isolation remain open.

## Role provisioning and scope

Using the real Owner Console, issued and activated an invitation for a synthetic `.invalid` identity with only `DEVELOPER` and the `default` project selected. The scoped project is visible in `evidence/CP12-developer-invite-form.png`. Credentials and one-time token were kept outside the repository. No test signer or provider secret is included in evidence.

## Playground and trace journey

1. Signed in as the Developer and opened Playground. The form offered the isolated `qa004-loopback` model and displayed the ephemeral-session budget cap.
2. Sent the synthetic prompt `Return exactly: QA004 developer canary.`. The Console created a Playground session (`POST /admin/v1/playground/sessions`, HTTP 201), then sent `/v1/chat/completions` (HTTP 200). The synthetic Ollama adapter returned `Hello from synthetic Ollama.`; the turn was marked Exact and showed $0.000000.
3. Used the actual **Inspect trace** action. The Console navigated to the persisted trace and displayed the request, route, HTTP 200, exact usage, and duration without exposing prompt/completion bytes. The trace inventory retained the request after the Playground page was left.

Screenshots: `CP12-developer-playground-prompt.png`, `CP12-developer-playground-result.png`, `CP12-developer-playground-trace-inspected.png`. The final screenshot was visually inspected. The first Playground browser-tab transcript is intentionally ephemeral per the displayed contract; the trace is separately persisted.

## Route Explain journey

In Routes & Policies → Route Explain, entered `qa004-mock`, chose residency US, and submitted the real dry-run. It returned HTTP 200, `dry_run: true`, and matched `qa004-mock`; zero deployments were eligible and two were rejected with `DATA_RESIDENCY_MISMATCH` and `CONNECTION_INACTIVE`. No upstream call or budget hold occurred. The result screenshot and sanitized response are in `CP12-developer-route-explain-result.*`.

## DEF-054 — permitted catalogs display empty and report unrelated permission errors

**Confirmed / Moderate.** With the same project-scoped Developer:

- The Models screen displayed `insufficient role permission`, zero deployments, and no models. Yet the screen's own model request returned HTTP 200 with one row and its scoped deployment request returned HTTP 200 with two rows.
- The Routes tab displayed the same error and zero routes. Its scoped route request returned HTTP 200 with one row and deployments returned two rows. Route Explain on that page matched `qa004-mock`; returning to Routes still showed zero.
- The page also made organization and provider-connection requests that correctly returned 403. Those denied ancillary requests do not justify clearing permitted model/route data.

Actual rendered screenshots, scoped request status/counts, and reproduction steps are in [DEF-054](../reports/DEF-054-developer-catalogs-empty-with-allowed-data.md) and the `CP12-developer-models-*` / `CP12-developer-routes-*` evidence. This is a current product defect, not an authorization boundary to weaken. The exact frontend state-transition cause remains a hypothesis.

## Other observed role boundaries

- FinOps Viewer coverage is in CP11: read-only budget/chargeback succeeded; write/member operations remained denied.
- Developer member management returned 403. Security Reviewer-only routes were not tested here.
- Access & Keys exposed key prefixes/plane metadata in its Virtual Keys tab; it did not display plaintext credentials.
- The visible navigation includes routes that can still return role-specific access errors; navigation policy across all six roles remains unverified.

## Remaining coverage

Team Admin, Security Reviewer, Operator, and additional Org Owner cases; cross-project and cross-organization isolation; Developer refresh and scope switching; MCP tool catalogs and grants; role-composition interactions; stream/session revocation; mobile/tablet and browser coverage. Do not infer a pass outside the exact slices above.
