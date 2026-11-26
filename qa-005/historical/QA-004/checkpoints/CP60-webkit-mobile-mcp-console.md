# CP60 — WebKit mobile MCP Console

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Status:** PASS (scoped) for Owner navigation, MCP server/tool rendering and one Sync tools action in WebKit at 390×844.  
**Product source/runtime changes:** none.

## Browser and proxy diagnosis

I used Playwright WebKit/WPE through the local HTTPS loopback proxy required for the Secure `__Host-` auth cookie. The first mobile Sync tools attempt returned 403 `csrf_validation_failed`, despite authenticated reads succeeding. A sanitized comparison in both Chromium and WebKit showed the CSRF header matched the current authenticated session and each browser’s origin matched its own host. Source inspection established that the isolated runtime allowlist is generated from its listener at `127.0.0.1:18132`, allowing `http(s)://127.0.0.1:18132`; the test proxy exposed `https://127.0.0.1:19443`. Both engines were denied with the mismatched proxy origin, so this was a test harness configuration issue, not a product defect.

I changed only the temporary local reverse proxy to forward the runtime’s configured loopback HTTPS origin and repeated the actual WebKit UI action. The authenticated session returned 200 and Sync tools returned 200, discovering one tool and reporting zero locked tools. No Tanpopo source, binary, or runtime setting was changed.

## Visual/mobile results

At 390×844, `documentElement`, body and `main` widths all measured exactly 390 px before and after sync. The real screenshot was visually inspected: navigation, server card and success banner reflow vertically; the full schema hash wraps; expanding the tool card exposes the per-tool grant selector within the viewport. The key used for CP59 had already been revoked, and the Grant control correctly remained disabled without an eligible key.

This establishes a narrow WebKit mobile Console slice only. Firefox remains blocked as recorded at CP58. No physical mobile device, other routes, non-Owner roles, locales, dark theme, assistive technology or large datasets were exercised.

## Evidence

- [Sanitized results and proxy diagnosis](../evidence/CP60-webkit-mobile-mcp-results.json)
- [Visually inspected expanded MCP tool card](../evidence/CP60-webkit-mobile-mcp-tool-detail.png)

QA remains ongoing. `GATE-REL-07` remains **BLOCKED**.
