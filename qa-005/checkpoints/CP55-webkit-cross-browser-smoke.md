# CP55 — WebKit browser smoke and Firefox environment blocker

**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`
**Runtime:** exact-source local Linux/amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated PostgreSQL/Valkey runtime.
**Browser:** Playwright WebKit/WPE 26.5 on Debian 13; Playwright 1.62.1. A loopback HTTPS reverse proxy used an ephemeral self-signed certificate for 127.0.0.1, and the test browser explicitly ignored that test certificate.
**Disposition:** WebKit UI and role smoke **PASS (scoped)**; Firefox **BLOCKED by the managed container**; no product defect assigned.

## WebKit procedure and result

Playwright-installed WebKit lacked host libraries. To avoid changing host packages as root, downloaded the required Debian 13 library packages to `/tmp`, extracted them into a QA-only path, and linked them into the test browser bundle. After that, WebKit launched.

The initial HTTP login attempt returned `/admin/v1/auth/login` 200 but WebKit retained no cookie and the following `/admin/v1/auth/session` returned 401. Inspection showed the app intentionally sets a Secure `__Host-tanpopo_admin_session` cookie; this is not a valid secure-cookie browser test over plain HTTP. No defect was assigned. To separate the test-origin issue from browser compatibility, placed a temporary HTTPS proxy in front of the same exact-source runtime with a self-signed, ephemeral loopback test certificate. The Owner login and session then both returned 200 and WebKit stored the `Secure`, `HttpOnly`, `SameSite=Lax` cookie. The Owner Overview rendered and was visually inspected.

At 375×812, a separately logged-in pure `FINOPS_VIEWER` loaded the FinOps screen; usage-summary and budgets calls returned 200. Direct budgets read returned 200 while trace list, Virtual Key inventory and kill-switch inventory returned 403 `insufficient_role_permission`; direct Traces navigation showed an explicit restricted state. The document remained 375px wide, without page-level horizontal overflow. FinOps and restricted Traces screenshots were visually inspected. This is a WebKit engine smoke of login/session, one role, two screens, API authorization and one viewport; it is not actual Safari coverage.

## Firefox blocker and remaining browser matrix

Installed Playwright Firefox 153.0 / revision 1538. Playwright launches the process, but it never completes startup and times out at 10 seconds. Diagnostics show Firefox cannot write `/proc/self/uid_map` (`EROFS`) and headless software rendering fails to map the default framebuffer. Disabling software rendering and reducing Firefox sandbox prefs did not resolve it. `playwright install-deps firefox webkit` could not install system packages because `su` has no available password. An elevated apt attempt could not write the managed `/var/lib/apt/lists/partial`; WebKit's required packages were instead extracted rootlessly under `/tmp`. Firefox remains BLOCKED, not tested.

The initial secure-cookie/HTTP mismatch and HTTPS retest are recorded with sanitized attributes only; no cookie value or private test certificate is committed. Evidence is in [CP55 JSON](../evidence/CP55-webkit-cross-browser-smoke.json) and the three visually inspected screenshots. Actual Safari on macOS/iOS, Google Chrome, Microsoft Edge, Firefox, and each vendor's latest two stable major versions remain UNVERIFIED/BLOCKED by platform/runtime availability. No new defect was identified. **GATE-REL-07 remains BLOCKED.**
