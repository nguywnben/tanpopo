# CP20 — Firefox runtime launch blocker

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Release gate:** GATE-REL-07 remains **BLOCKED**.

Firefox 153.0 was downloaded to `/tmp/qa002-pw-browsers/firefox-1538`, separate from the repository and product dependency tree. Five headless Playwright launch attempts started a Firefox process but did not establish the Playwright control channel. The bounded last attempt timed out at 20 seconds. Browser diagnostics report an inability to write `/proc/self/uid_map` (`EROFS`) and `RenderCompositorSWGL failed mapping default framebuffer`.

A writable cache under `/tmp`, headless mode, software-rendering/WebRender preferences, and a test-only content-sandbox disable setting did not resolve launch. The local Tanpopo runtime itself remained reachable by HTTP 200. No Tanpopo page loaded in Firefox; no UI result or screenshot is inferred. Firefox coverage stays **UNVERIFIED**.

Sanitized launch summary: `evidence/CP20-firefox-launch.json`. Chromium and prior real WebKit test results remain separately scoped. No implementation, product dependencies, or source files were changed. Further Firefox work requires a managed desktop/browser runtime that permits Firefox control IPC and compositor startup, or a host with writable user namespaces and a supported rendering backend.
