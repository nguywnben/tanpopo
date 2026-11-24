# CP101 — Organization switching and member-scope consistency

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`<br>
**Runtime:** QA server `18084`<br>
**Role/browser:** Org Owner + Operator, real Chromium at 1440 × 1000<br>
**Product implementation:** unchanged

## Journey and observations

Started in an already authenticated owner session, opened the workspace selector, and explicitly switched to the authorized `QA002 CP18 Audit Status Org` listed in that account's organization choices. Then opened Access & Keys → Members & Roles in organization-only scope.

The screen labeled the collection **Organization members** and showed count `0` / **No members found**. A same-session authenticated `GET /admin/v1/members?limit=100` returned HTTP 200 with four members. The authorized projects endpoint returned three projects, confirming this is a populated second organization; no project or member was created or changed. The UI after the organization switch was captured and visually inspected.

This is a fresh tenant-context reproduction of **DEF-023 Moderate**, not a new defect or an authorization bypass. The account could select this organization explicitly, and the same owner API was authorized there. No other organization's data was queried through an unselected or unauthorized scope.

## Evidence

- `../evidence/CP101-org-switch-members.json` — selected org, API collection counts, visible UI count and empty-state result.
- `../evidence/CP101-audit-org-members.png` — actual post-switch Members & Roles screen, visually inspected.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only scoped QA-002 results; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 failure atomicity/populated erasure remains UNVERIFIED.
- **Historical defects failing / unverified:** DEF-016–036 remain open; DEF-023 is independently reconfirmed in a second organization. No other disposition changed.
- **New defects:** none. DEF-023 remains the existing issue; no authorization bypass or cross-tenant access was found.
- **Coverage completed:** real organization switch into another authorized tenant context; organization-only member screen compared with persisted API count and project inventory; screenshot visually inspected.
- **Coverage blocked:** none specific to this read-only owner journey. Fresh Team Admin credentials remain unavailable as documented in CP100.
- **Coverage remaining:** project/environment scope, other roles and cross-tenant denial in this second organization; complete DEF-001–015 acceptance and all other incomplete product, browser, platform, integration, security, and operations work.
- **Recommended engineering handoffs:** add this second-org reproduction to DEF-023 acceptance. The fix must render organization members independently of environment scope while preserving the existing Team Admin and tenant authorization boundaries. Existing priorities DEF-036, DEF-035 with DEF-024/029/030, DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007 remain.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
