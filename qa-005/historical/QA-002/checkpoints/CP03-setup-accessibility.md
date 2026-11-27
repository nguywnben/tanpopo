# CP03 — Fresh setup accessibility and persistence

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated A2A-enabled SQLite instance, `127.0.0.1:18084`  
**Status:** PASS for the exercised first-run setup journey in Chromium; broader locale, viewport, keyboard, and browser coverage remains open.

## Actions and observations

Completed the entire first-run wizard using a synthetic owner and tenant. The provider step correctly explained that no adapters were active and allowed setup to continue. The key-custody page stated the consequence of losing the master key and blocked continuation until its acknowledgement was selected. Review showed the entered account and tenant and the skipped initial connection. Bootstrap then returned HTTP 201 and the independent status endpoint reported the instance initialized.

Each step was scanned with axe 2.2 in a real Chromium page. All four scans reported zero violations (26–29 passing checks, zero incomplete checks). Progress controls' accessible names contained their visible step wording in the current live DOM. All four captured screenshots were visually inspected: `evidence/CP03-setup-step1.png` through `CP03-setup-step4-review.png`.

This reconciles QA-001's conflicting evidence at the tested baseline: the old automated failures and the old live-browser pass remain in immutable QA-001 history; on QA-002's baseline, the full frontend suite passed in CP02 and this live full-wizard scan did not reproduce the accessible-name issue. DEF-001 is verified fixed for the scoped tested path, not for every shared progress component, browser, locale, or assistive technology.

## Coverage updates

- DEF-001: **VERIFIED FIXED (scoped)** — current setup wizard steps 1–4 in Chromium, backed by axe counts, live accessible names, actual progression, and screenshot inspection.
- SCR-01 and WF-01: **PARTIAL PASS** — setup completion and persistence verified. Login was already exercised on the separate 18083 runtime in CP01; cross-runtime state is intentionally isolated.
- Cross-cutting accessibility: **PARTIAL PASS** — setup steps only; all other screens and supported modalities remain UNVERIFIED.

Machine-readable evidence: `evidence/CP03-setup-accessibility.json`.
