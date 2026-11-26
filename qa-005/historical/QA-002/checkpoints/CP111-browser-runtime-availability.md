# CP111 — Cross-browser execution environment diagnostics

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Product runtime:** isolated Console on 18084 remained healthy (HTTP 200 `/healthz`)  
**Result:** Chromium **AVAILABLE**; this turn's Firefox/WebKit execution **BLOCKED** by managed host dependencies  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Firefox

Installed the Playwright Firefox 153.0 browser into `/tmp` and verified its binary version. Playwright launch timed out. The driver logs report inability to write `/proc/self/uid_map` (`EROFS`) and `RenderCompositorSWGL failed mapping default framebuffer`. A second attempt used a writable `XDG_CACHE_HOME` and software rendering preferences; the Fontconfig cache warning cleared, but the sandbox and compositor errors remained and the browser did not launch. No browser-sandbox bypass was attempted. No Firefox product interactions or screenshots are claimed.

## WebKit

Downloaded the Playwright WebKit browser. Host validation and launch report six missing shared libraries: `libgtk-4.so.1`, `libgraphene-1.0.so.0`, `libharfbuzz-icu.so.0`, `libmanette-0.2.so.0`, `libhyphen.so.0`, and `libGLESv2.so.2`. Attempted the documented Playwright dependency installer, but the managed runtime's root switch failed with `su: Authentication failure`; no system packages changed. No WebKit product interactions are claimed for CP111. Existing CP07/33/34/64/66/67/74/100 WebKit evidence remains intact and continues to count for those scoped workflows.

## Evidence and coverage

Sanitized capability/status results are recorded in [CP111 evidence](../evidence/CP111-browser-runtime-availability.json). Chromium remains available and was used for real product journeys in CP109–CP110. This is an environment limitation, not a Tanpopo defect.

- **PASS:** current Chromium execution and health check; prior documented WebKit journeys remain scoped PASS results.
- **BLOCKED:** new Firefox and WebKit journeys in the current managed image for the exact host/runtime reasons above.
- **UNVERIFIED:** Firefox full Console journey; current-image WebKit repeat; other browser versions, accessibility and platform combinations.
- **Coverage completed:** installed/probed browser runtimes, attempted approved dependency remediation, preserved Chromium QA continuation.
- **Remaining:** provide Firefox-compatible user-namespace/compositor support and WebKit's GTK4/graphics/input libraries via managed-image provisioning, then resume cross-browser workflows. Continue all independent Chromium and API/CLI QA meanwhile.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
