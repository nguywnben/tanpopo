# CP112 — DEF-009 WebKit/WPE mobile webhook drawer

**Disposition:** The historical WebKit offscreen drawer result is not reproduced in exact RC7 WPE WebKit 26.5 at 375×812 or 320×740. DEF-009 becomes **PASS (scoped)**; prior contradictory QA-001 browser findings remain preserved. `GATE-REL-07` remains **BLOCKED**.

## Runtime and browser

- Frozen source commit `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
- Isolated QA SQLite clone at `127.0.0.1:18164`; synthetic Owner; empty webhook inventory.
- Playwright 1.62.1 with the already-installed WebKit/WPE 26.5 bundle and QA-only Debian libraries. Its user agent reported WebKit `Version/26.5` / Safari compatibility string.
- The Secure admin session cookie requires HTTPS. A loopback HTTPS reverse proxy at `127.0.0.1:19446` used a temporary test certificate and forwarded only to the isolated HTTP runtime. Certificate/key and proxy stayed outside the branch.

## Reproduction

1. Signed in over HTTPS as the synthetic Owner and opened Operations → Webhooks. API list reads returned HTTP 200; the page showed zero subscriptions.
2. Set viewport to 375×812, opened Add webhook, and visually inspected the real drawer. Receiver URL, signing credential selector, event selector, Cancel and Register controls fit onscreen. Document/body widths were each 375px.
3. Resized the same open form to 320×740. The drawer header, inputs, selectors and bottom actions remained visible; document/body widths were each 320px.
4. WebKit reported no page errors. The form was not submitted; no webhook, credential or network delivery was created.

## Disposition and limits

The RC7 WPE WebKit drawer does not reproduce the historical offscreen result in either tested mobile CSS viewport. **PASS (scoped WPE WebKit retest).** The old QA-001 reproduction and non-reproduction are retained. This is not native Safari validation; macOS/iOS Safari, other screen widths/roles, touch, keyboard and assistive-technology behavior remain open.

Three screenshots were opened and visually inspected: [Webhooks page at 375px](../evidence/screenshots/CP112-def009-webkit-webhooks-375.png), [drawer at 375px](../evidence/screenshots/CP112-def009-webkit-add-drawer-375.png) and [drawer at 320px](../evidence/screenshots/CP112-def009-webkit-add-drawer-320.png). Sanitized dimensions, browser identity, API statuses and screenshot hashes are in [evidence](../evidence/CP112-def009-webkit-webhook-drawer.json).
