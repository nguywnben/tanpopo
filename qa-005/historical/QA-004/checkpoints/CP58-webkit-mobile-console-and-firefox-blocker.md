# CP58 — WebKit mobile Console slice; Firefox environment blocker

**Status:** PASS (scoped WebKit/WPE browser-engine Owner session and responsive Providers state); BLOCKED (Firefox launch). Chromium remains covered by earlier independent checkpoints. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; isolated backend artifact `45581179274` at `127.0.0.1:18132`.
**Product source:** unchanged.

## WebKit/WPE actual browser workflow

Playwright's bundled WebKit/WPE binary and its user-space Debian runtime libraries were prepared under `/tmp`; no system package installation or product-code change was made. The test backend uses a loopback-only HTTPS reverse proxy and temporary self-signed certificate. This was needed because the browser did not accept the app's `Secure; __Host-...` session cookie over the initial loopback HTTP test URL. With local HTTPS and Playwright configured to trust only this isolated test certificate, a real Owner sign-in succeeded; authenticated `/admin/v1/auth/session` returned HTTP 200 and roles `OPERATOR`, `ORG_OWNER`.

At a 390×844 viewport, used the actual mobile hamburger navigation to open **Providers → Vault Secrets**. The empty state rendered with no provider connections and no vault secrets. Measured viewport/document/body/main widths were all 390 pixels. The actual screenshot was visually inspected; no horizontal overflow or clipped Vault empty state was observed in this slice. A separate desktop WebKit Providers connections screenshot was also inspected.

The initial HTTP cookie issue is a test-harness protocol mismatch, not a product finding; the cookie is correctly marked Secure and production UI is expected to run over HTTPS.

## Firefox attempt and blocker

Firefox 1538 was installed into `/tmp/qa004-browsers` using Playwright. Multiple headless launches were attempted (10–20 second timeouts). Logs show the Firefox sandbox cannot write `/proc/self/uid_map` (`EROFS`) and `RenderCompositorSWGL failed mapping default framebuffer`; launch times out before a browser context or page is available. A retry with browser-only sandbox and software-render preferences did not clear the failure. No Firefox Tanpopo screen or behavior is reported as tested. Resolving this blocker needs a compatible Firefox runtime/container profile with working user namespaces and software graphics, or a different runner.

## Evidence and coverage

- [Desktop WebKit Providers screen](../evidence/CP58-webkit-providers.png) — visually inspected.
- [390×844 WebKit Vault Secrets screen](../evidence/CP58-webkit-mobile-vault-secrets.png) — visually inspected.
- [Sanitized browser-engine results](../evidence/CP58-browser-engine-results.json).

- **PASS (scoped):** WebKit/WPE accepts secure auth through local HTTPS; real Owner session; responsive Providers/Vault Secrets route; no horizontal overflow at 390 pixels.
- **BLOCKED:** Firefox browser launch, with runner errors documented above.
- **UNVERIFIED:** Firefox product behavior; WebKit non-Owner roles, populated records, other routes, locale/theme interactions, keyboard/screen-reader semantics, tablet, and broader device/browser combinations.

This is a small cross-browser slice, not broad browser/platform acceptance. QA remains ongoing.
