# CP06 — WebKit mobile layout and browser runtime coverage

**Baseline:** RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Status:** Scoped WebKit mobile flow completed; Firefox and other browser/platform coverage remain blocked or unverified. `GATE-REL-07` remains **BLOCKED**.

## WebKit 26.5 at 375×844

The Playwright package was present, but browser binaries were not. Firefox and WebKit browser binaries were downloaded to private `/tmp` test paths. WebKit's six missing host libraries were likewise unpacked under `/tmp`; no application or package manifest was changed. A temporary self-signed local TLS reverse proxy forwarded to the isolated HTTP app runtime so `Secure` session cookies behaved as they do behind production HTTPS. The browser ignored only this local test certificate.

In a real WebKit context with `isMobile`, 375×844 viewport and touch enabled, sign in, open the mobile navigation menu, navigate to Operations → Webhooks, and open the registration drawer. This exercises the historical DEF-009 path. The receiver field, signing credential selector, events selector, Cancel, and Register webhook controls were visible and usable in the screenshot. The drawer bounds were x=57, width=318, right=375, height=844; the footer buttons ended at y=828. Document width and viewport width both remained 375, with no horizontal overflow. See [drawer screenshot](../evidence/CP06-webkit-webhook-drawer-375.png), [Overview screenshot](../evidence/CP06-webkit-mobile-overview-375.png), and [measured layout evidence](../evidence/CP06-webkit-webhook-drawer-375.json).

The first attempt used the plain-HTTP local preview and WebKit rejected the Secure session cookie, leaving the page at “Loading console.” This was a preview-scheme mismatch, not evidence of a Tanpopo defect. Retrying through local HTTPS succeeded: authenticated WebKit requests to session, bootstrap, org/project, overview, and alert endpoints returned 200. No webhook was registered or delivered in this phase.

## Browser limits and defect disposition

- **DEF-009 — VERIFIED FIXED for the historical WebKit mobile drawer layout.** The drawer and all key controls fit at 375×844 with no document overflow. Registration/delivery behavior was separately exercised in Chromium in CP05; WebKit registration/delivery remain unverified.
- Chromium 156: installed and previously used for real desktop journeys.
- WebKit 26.5: installed in `/tmp`; mobile WebKit navigation and drawer layout passed this scoped check.
- Firefox 153: binary installed in `/tmp` but did not start. Playwright launch timed out after the process reported `Sandbox: writing /proc/self/uid_map: EROFS`; Fontconfig also reported no writable cache directories. Firefox coverage is **BLOCKED** by this container's process/filesystem restrictions. The browser sandbox was not disabled.
- macOS/iOS Safari, Android Chrome, other desktop browsers, and cross-platform operating systems remain **UNVERIFIED**. A local reverse proxy is infrastructure for the local HTTPS preview only, not a production topology test.
