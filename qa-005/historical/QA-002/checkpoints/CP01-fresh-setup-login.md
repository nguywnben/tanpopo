# CP01 — Fresh install and first authenticated session

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`; local isolated binary on `127.0.0.1:18083`.
**Scenario status:** **PASS** for first-install Setup, owner creation, provider skip, master-key acknowledgement, bootstrap completion, and a separate login session. This is one workflow path, not complete setup or release coverage.

## Actions and observed state

1. Opened `/setup` in actual Chromium 156 at 1440×1000; visually inspected and captured the empty first-run owner form. The page had one H1, visible labels for all six fields, and page width equaled the viewport.
2. Entered the server-generated one-time bootstrap token in a password field and synthetic QA-only owner/org/project values through actual browser inputs. The server accepted owner step and showed the optional provider step.
3. The isolated runtime reported no provider adapters activated; Setup explained that provider setup can be completed after initialization from Extensions → Marketplace. Continued without a provider.
4. Confirmed custody of the disposable instance key file. Attempting to continue without this checkbox showed the expected “Confirm custody…” validation; after checking it, advanced to Review.
5. Review screen showed the expected synthetic owner, org/project and “Initial connection: Skipped.” Visually inspected the screenshot before completing.
6. Clicked Complete setup. `/admin/v1/bootstrap/status` returned `initialized: true`; a new browser context then logged in with the synthetic owner. `POST /admin/v1/auth/login` returned 200 and the app landed on `/overview`; follow-up session, org/project/environment, deployment, alert, approval and overview requests returned 200. No browser console/page errors were observed. Overview showed zero requests, zero spend, no active alerts, and the expected scope.

## Evidence

- `evidence/CP01-setup-first-run.png` — visually inspected; first-run UI.
- `evidence/CP01-setup-provider-step.png` — optional-provider empty state.
- `evidence/CP01-setup-key-custody-step.png` — visibly required acknowledgement; no key material shown.
- `evidence/CP01-setup-review-step.png` — visually inspected synthetic setup summary.
- `evidence/CP01-setup-complete.png` — captured during the in-flight completion response (spinner); not used alone as proof of completion.
- `evidence/CP01-login-before.png` and `evidence/CP01-login-after.png` — separate login and resulting overview; final screenshot visually inspected.
- API/state evidence: bootstrap status, login/session and overview response statuses are summarized here; no credential values or raw server logs retained.

## Defect and coverage relationship

- DEF-001 remains **UNVERIFIED**: this confirms the actual Setup journey works, but no live axe scan was run and the historical discrepancy between two Vitest axe failures and a live Chromium pass is not yet reconciled on RC4.
- DEF-003..015 remain UNVERIFIED; no relevant behavior was exercised.
- WF-01/WF-02 and SCR-01 are **PARTIAL PASS** only for the basic empty-install and owner login path. Provider activation, persisted state across restart, alternate roles, other browser engines, responsive widths, dark theme, keyboard/screen-reader use, and setup error/recovery permutations remain open.

## Notes / no new defect assigned

- `/health` returned the Console HTML fallback with HTTP 200. The documented operational route is `/healthz`, and `/readyz` returned `{"status":"ready"}`. This is recorded as a route-check correction; no defect is assigned without a documented `/health` contract.
- The Setup completion screenshot was captured while the submit control still showed a spinner. Completion was subsequently established by the bootstrap status and separate successful login. It is not treated as evidence of a stuck submit.
- Two initial Playwright attempts had harness selector/await mistakes; they were corrected, and no product failure was inferred from those attempts.
