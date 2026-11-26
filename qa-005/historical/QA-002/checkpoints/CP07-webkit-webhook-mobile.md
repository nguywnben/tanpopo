# CP07 — WebKit webhook drawer at 375px

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Status:** PASS for the scoped WebKit 26.5, 375×844 drawer-layout workflow.

Installed Playwright's isolated WebKit build and extracted the required Debian runtime libraries under `/tmp`; no Tanpopo files or system packages were modified. WebKit did not retain the Secure `__Host-` admin session cookie over the QA server's plain HTTP URL (login returned 200, next session lookup returned 401). Repeating through a local HTTPS reverse proxy preserved the session and reached the application; the proxy used a temporary self-signed certificate accepted only by this test browser.

In authenticated WebKit, opened Operations → Webhooks → Add webhook. I visually inspected the actual page and drawer screenshots. At 375×844 the drawer title, URL field, secret selector, event selector, and footer were visible. Measured document width remained 375px with no horizontal overflow; the form right edge was x=359 and title right edge x=323. The page explained that a write-only HMAC credential must first be created in Providers → Vault Secrets, and appropriately disabled registration until a credential exists. No request was submitted. The historical offscreen drawer position did not reproduce in this run.

DEF-009 is verified fixed for this exact engine, viewport, and authenticated HTTPS flow. QA-001's contradictory fail/pass and later invalid-target evidence remain in its immutable record. This does not cover all mobile views, other browser engines, drawer scrolling under long localized text, or webhook registration itself.

Evidence: `evidence/CP07-webkit-webhook-drawer.json`, `evidence/CP07-webkit-pre-drawer.png`, `evidence/CP07-webkit-webhook-drawer-375.png`.
