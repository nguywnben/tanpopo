# CP39 — Authenticated Console navigation in Chromium and WebKit

**Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated SQLite service `18116`; synthetic Org Owner; real Playwright Chromium and WebKit; local HTTPS test shim for the browser session cookie<br>
**Disposition:** scoped browser navigation PASS; QA-003 remains incomplete and `GATE-REL-07` remains **BLOCKED**.

## Journey and observations

In separate clean browser contexts, authenticated the existing synthetic owner, opened Overview, then used the visible Console navigation to open Providers and Traces. Captured actual rendered screens at 1440×1000. Both engines rendered all three pages and the full sidebar without page JavaScript exceptions.

The same persistent view appeared in both engines: Overview showed five requests, 100% errors, no active alerts, and zero settled spend; Providers showed three synthetic connections (two Unverified, one Disabled) and one vault secret; Traces showed five recent 503 requests with `no_compliant_deployment_available`. Screenshot comparison found no obvious visual discrepancy between Chromium and WebKit for these pages. The view is a narrow interaction sample, not an accessibility or compatibility certification.

- Chromium screenshots: [Overview](../evidence/CP39-chromium-overview.png), [Providers](../evidence/CP39-chromium-providers.png), [Traces](../evidence/CP39-chromium-traces.png).
- WebKit screenshots: [Overview](../evidence/CP39-webkit-overview.png), [Providers](../evidence/CP39-webkit-providers.png), [Traces](../evidence/CP39-webkit-traces.png).
- Sanitized interaction summary: [CP39 result](../evidence/CP39-cross-browser-result.json).

## Coverage and limits

- **PASS, scoped:** authenticated Overview → Providers → Traces navigation and visible state in Chromium and WebKit on one existing populated SQLite runtime.
- **UNVERIFIED:** forms and mutations, mobile/touch layouts in this phase, authorization matrices, other routes, locales/themes, fresh/upgrade flows, PostgreSQL/Valkey, native platforms, and browser versions outside the tested installed builds.
- **BLOCKED:** Firefox remains blocked by the previously observed container sandbox failure (`writing /proc/self/uid_map: EROFS`) in [CP06](CP06-webkit-mobile-and-browser-runtime.md). The browser sandbox was not disabled. This phase successfully reused WebKit’s isolated `/tmp` runtime dependencies after correcting the launch wrapper; that setup does not alter Tanpopo.

No new product defect was confirmed. No implementation was changed. This phase does not change any historical defect disposition; prior PASS labels remain scoped to their cited evidence and are not promoted to full acceptance closure.
