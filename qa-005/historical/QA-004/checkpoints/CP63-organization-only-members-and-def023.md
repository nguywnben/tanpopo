# CP63 — Organization-only member visibility / DEF-023

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated offline RC6 on `127.0.0.1:18132`  
**Actor:** synthetic Org Owner  
**Product changes:** none

## Result

- **PASS (scoped; DEF-023 retest):** In real Chromium, selected **No project / No env**, opened **Access & Keys → Members & Roles**, and observed all five organization members. Two UI-triggered `GET /admin/v1/members?limit=100` calls returned HTTP 200; a separate authenticated read also returned the same five.
- **PASS (scoped):** Hard refresh returned the Console scope to CP57 (the account’s observed default), but Members & Roles still displayed the five organization members and the authenticated collection still returned five. The Virtual Keys tab’s environment requirement did not hide the distinct organization-members tab.
- **UNVERIFIED:** Sibling-project filtering where projects have different member sets; Team Admin/Security Reviewer organization-only behavior; pagination; concurrent scope changes or membership updates.

## Reproduction

1. Sign in as the isolated Org Owner.
2. In the workspace picker select **No project**, leaving **No env** selected.
3. Open **Access & Keys → Members & Roles**.
4. Confirm the tab count and organization members table each show five; compare with authenticated `GET /admin/v1/members?limit=100` (HTTP 200, five rows).
5. Hard refresh. The scope returns to CP57, while the members tab still renders all five and the API still returns five.

## Evidence

- [No-project organization members](../evidence/CP63-access-org-only-members.png)
- [Populated members after refresh](../evidence/CP63-access-members-after-refresh.png)
- [Sanitized request, count and scope evidence](../evidence/CP63-org-members-scope.json)

Screenshots were opened and visually inspected. No defect was reproduced in this tested Owner workflow; DEF-023 is marked **PASS (scoped)**, not globally resolved.

`GATE-REL-07` remains **BLOCKED**; QA-004 is ongoing.
