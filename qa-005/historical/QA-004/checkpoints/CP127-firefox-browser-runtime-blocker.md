# CP127 — Firefox startup blocker retest

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** **BLOCKED** before the Firefox page was created. No product behavior or screenshot is claimed; GATE-REL-07 remains BLOCKED.

## Attempted workflow

The intended real-browser slice was the authenticated Owner Console at 390×844: select `QA004 Test Root Org / default / dev`, open FinOps → Budgets & Holds, inspect the 100-micro-USD project budget, and capture a screenshot. The installed Playwright Firefox bundle is available at `/tmp/qa004-browsers/firefox-1538` (Playwright 1.62.0).

Three launch paths were tried: (1) Playwright’s normal headless launch with a 10-second timeout; (2) the same with a writable `/tmp` XDG cache, `MOZ_WEBRENDER=0`, and `--disable-gpu`; and (3) direct headless Firefox CLI screenshot of local RC6 with a 15-second timeout. All failed before any page could be inspected. Playwright diagnostics show `Sandbox: writing /proc/self/uid_map: EROFS` and `RenderCompositorSWGL failed mapping default framebuffer`; the CLI timed out with the same uid-map error and produced no screenshot. The fontconfig cache warning from the first launch was addressed by redirecting XDG cache to `/tmp`, without resolving the browser startup failure.

Cloud environment status reports the instance running with network policy enforced and `remote_desktop` enabled but `state: unknown`. No desktop/browser-control JavaScript tool or viewer entry is exposed in this chat, so that fallback cannot be controlled here. Environment ID: `ccarenv_b64_Y2NhcmVudl80YTAzYTdkM2EyMWM4MTkxOTcwZDI5ZjY3OTg0NzQxMQ`.

## Classification

This is an environment/runtime limitation, not a Tanpopo defect. Firefox coverage remains **BLOCKED**; there is no screenshot and no Firefox PASS. Existing Chromium and WebKit slices remain scoped to their own engines. WebKit/Chromium evidence does not substitute for the unavailable Firefox run.

Sanitized launcher diagnostics are in [`CP127-firefox-startup-diagnostics.json`](../evidence/CP127-firefox-startup-diagnostics.json). QA continues; **GATE-REL-07 remains BLOCKED.**
