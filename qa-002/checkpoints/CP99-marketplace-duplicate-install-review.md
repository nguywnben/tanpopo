# CP99 — Marketplace duplicate install review

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4)  
**Runtime:** QA server `18084`; Org Owner and Operator  
**Browser:** Chromium, 1440 × 1000; actual UI interactions and screenshots visually inspected  
**Product implementation:** unchanged

## Work performed

Opened Extensions → Installed and Marketplace in the actual Console. The Marketplace rendered eight official provider adapter entries with origin, publisher signature, execution tier, capability, and **Stage for review** controls. The page was visually inspected; the list showed `@tanpopo/gemini` as a catalog entry.

With two active extension installation records already present, clicked **Stage for review** on Google Gemini. The API returned HTTP 409 `resource_already_exists`; the response message stated that `@tanpopo/gemini` was already registered in `ENABLED_ACTIVE` and said to use `StageUpgrade`. The error banner displayed that storage/unique-constraint text directly. Authenticated installation-list reads before and after were both HTTP 200 and contained the same two active rows. The action made no persistent change.

This confirms **DEF-036 Low**. It is a Marketplace lifecycle and error presentation defect. The backend correctly rejected a duplicate, so no integrity or authorization failure is claimed. See [DEF-036](../reports/DEF-036-marketplace-duplicate-install-error.md).

## Evidence

- `../evidence/CP99-extensions-installed.png` — Installed view before the attempt.
- `../evidence/CP99-marketplace.png` — eight-entry catalog and stage controls, visually inspected.
- `../evidence/CP99-duplicate-stage-error.png` — real post-action 409 banner, visually inspected.
- `../evidence/CP99-duplicate-stage-result.json` — sanitized status/message and before/after persistent install states.
- `../evidence/CP99-google-stage-review.png` and `../evidence/CP99-stage-review-inspection.json` — first reproducible interaction and its rendered error state.
- `../evidence/CP99-marketplace-inspection.json` — rendered package descriptions, controls, and catalog state.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only the scoped QA-002 results in the register; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007's successful audit paths do not close failure atomicity or populated erasure.
- **Historical defects failing / unverified:** DEF-016–035 remain open as previously recorded; CP99 did not retest them. DEF-023 remains confirmed across the prior organization-only/project/no-environment reproductions. No historical disposition was downgraded or removed.
- **New defects:** DEF-036 Low confirmed as above. No additional defect from this phase.
- **Coverage completed:** actual Chromium interaction with installed and Marketplace extension views; visible package lifecycle action; duplicate install 409; unchanged persistent installation state; screenshot inspection.
- **Coverage blocked:** none specific to CP99.
- **Coverage remaining:** full install/permission-consent/independent-approval/activation/upgrade/uninstall workflow; runtime adapter invocation, sandbox and credential broker; cross-role Marketplace authorization; extension filters/search and package state across more lifecycle states; all open historic defects and remaining product/browser/platform coverage.
- **Recommended engineering handoffs:** add DEF-036 to the extension lifecycle/UI handoff. Existing priorities remain DEF-035 with DEF-023/024/029/030, DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
