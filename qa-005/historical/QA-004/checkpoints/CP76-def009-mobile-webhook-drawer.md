# CP76 — DEF-009 mobile webhook drawer retest and WebKit blocker

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated A2A-enabled RC6 test clone `127.0.0.1:18128`  
**Actor:** synthetic Org Owner  
**Product source modified:** no

## Chromium result

At 375×844 in real Chromium, opened **Operations → Webhooks → Add webhook** and left the form unsubmitted. The heading, HTTPS receiver URL, signing credential selector, events selector, Cancel and disabled Register webhook actions all appeared within the viewport. The heading's left edge was x=73; document and body widths remained 375 pixels. No page errors occurred. The screenshot was visually inspected. This is a scoped Chromium layout pass only.

The interaction was performed on a canary Org Owner account with an empty webhook list. It did not create a subscription or secret.

## WebKit result

**BLOCKED before page creation.** Exact-RC6 Playwright WebKit could not launch: host dependency validation reports `libGLESv2.so.2` missing. The WebKit/WPE bundle exists under `/tmp/qa004-browsers/webkit-2336`, and the library file exists under `/tmp/qa004-browser-deps/usr/lib/x86_64-linux-gnu`; adding that directory to `LD_LIBRARY_PATH` did not satisfy Playwright's launch validation. No WebKit page opened and no WebKit behavior is inferred.

The managed environment reports remote desktop support enabled but state unknown; this chat has no desktop JavaScript/computer-use tool. Exact launch diagnostics are in [the WebKit blocker record](../evidence/CP76-webkit-launch-blocker.json).

## DEF-009 disposition

**UNVERIFIED on exact RC6.** Historical QA-001 WebKit at 375×844 reproduced the drawer offscreen; QA-001 CP27 later recorded a non-reproduction, and CP49 did not reach the drawer because of a login harness failure. CP76 adds an exact-RC6 Chromium result but cannot reconcile the WebKit disagreement.

- **Expected:** Add webhook places the full registration form and actions inside the viewport or provides a visible way to reach them.
- **Actual:** Chromium meets this layout behavior at 375×844. Current WebKit remains untested due the launch dependency blocker.
- **Next required infrastructure:** a Playwright-supported WebKit/WPE installation with native dependencies registered in the host linker path, or an authorized shared desktop browser tool. Re-run the same workflow and inspect screenshots at 375×844.

Screenshots and geometry are in [CP76 Chromium evidence](../evidence/CP76-chromium-webhook-drawer-375.json) and [the inspected screenshot](../evidence/CP76-chromium-webhook-drawer-375.png). QA continues; **GATE-REL-07 remains BLOCKED**.
