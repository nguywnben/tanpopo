# CP100 — Console workspace scope cross-surface regression

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** QA server `18084`  
**Role/browser:** Org Owner + Operator, real Chromium at 1440 × 1000 and WebKit at 375 × 844  
**Product implementation:** unchanged

## Scope contexts and method

Repeated read-only Console navigation in organization-only, project-only (`QA002 CP78 Fresh Budget`, no environment), and project/environment (`QA002 Audit mwto47wa / stage`) contexts. Chromium covered Providers, MCP Hub, Security, Operations, Access & Keys → Members & Roles, and Playground. WebKit independently repeated the three Members & Roles scope states at 375 × 844. Captured Admin API response status codes where each page loaded its data. Screenshots were visually inspected for the Access empty/populated states, provider rows, MCP inventory, Security approval queue, Operations health, and Playground's no-environment notice. No write or destructive action was submitted.

WebKit host libraries were missing initially. The required runtime packages were extracted under `/tmp` without root/system installation. Because the 18084 QA server is HTTP-only and WebKit did not persist its secure session cookie on that origin, a temporary QA-only HTTPS forwarder on `127.0.0.1:18544` proxied requests to `18084`; it is not part of Tanpopo and made no database changes. The successful WebKit run had no browser-console or page errors.

## Results

| Surface | Org-only | Project-only | Project + environment | Result |
| --- | --- | --- | --- | --- |
| Providers | 5 connections, 2 adapters, 1 vault secret; reads 200 | Same visible inventory; reads 200 | Same visible inventory; reads 200 | Narrow scope-invariant owner inventory; no scope defect assigned. Two visually duplicated Google rows remain the previously recorded DEF-032; no actions taken. |
| MCP Hub | 1 registered server; A2A Agents & Active Tasks tab count 16; list calls 200 | Same visible inventory; list calls 200 | Same visible inventory; list calls 200 | Narrow organization-level consistency. Existing rows were not mutated. |
| Security → Approval Queue | 0 pending, explicit queue-clear state; APIs 200 | Same state; APIs 200 | Same state; APIs 200 | No inconsistency observed. Other roles/queue mutations remain open. |
| Operations → Health & Cluster | Ready, standalone SQLite node, epoch 75; API 200 | Same state; API 200 | Same state; API 200 | Scope-invariant operations view in this owner session. Other roles and failure modes remain open. |
| Access & Keys → Members & Roles | UI says 0 / “No members found”; direct same-session `GET /admin/v1/members?limit=100` is 200/count 4 | Same false empty; API 200/count 4 | UI shows 4; API 200/count 4 | **FAIL — DEF-023 reconfirmed.** This is authorized organization-member data, not a role denial. |
| Playground | Clearly asks for active environment; Send has `aria-disabled=true`; no route request | Same no-environment guard; no route request | Route list 200, configured route shown; no prompt submitted | PASS for scoped no-environment guard. Inference was not attempted. |

DEF-023 was repeated through scope selection, hard refresh, reselection, and navigation away/back. The false-empty UI remained under organization-only and project/no-environment scopes; selecting the environment rendered all four members. The authenticated member API continued to return four in every state. WebKit at 375 × 844 independently showed the same 0/0/4 visible count against the same API count of four; document width remained 375 in all three screenshots. This independently reproduces the user-visible contract violation without changing member state.

Hard refresh reset the selected workspace to runtime default `QA002 CP78 Fresh Budget / No env`, including after the environment had been selected. This recurs the already recorded QA-OBS-009. It was recorded as an observation and not counted as a newly confirmed defect because persistence semantics remain unresolved.

## Authorization and scope interpretation

The broad scope matrix used an Org Owner + Operator. A fresh Team Admin login was then attempted with the retained QA test credential, but the real login page returned “Sign-in failed”; no authenticated page or API request was possible. This is an environment/credential blocker, not evidence about product authorization. The screenshot and sanitized result are `../evidence/CP100-team-admin-login-blocked.png` and `../evidence/CP100-team-admin-permission-boundaries.json`. Existing CP71/CP88 evidence that Team Admin project scope is limited and Security audit access is denied with HTTP 403 remains the separate, narrow permission boundary. No privilege escalation or cross-tenant access was tested here.

The stable provider/MCP/security/operations counts are observed behavior in this owner account; other role-specific scope contracts remain unverified. Playground's explicit environment prerequisite is a clear intentional restriction. Overview/Traces, FinOps, Models, Routes & Policies, Extensions, and earlier scope-race findings retain their prior CP91/CP95/DEF-024/029/030/035 evidence; this phase does not mark those investigations complete.

## Evidence

- `../evidence/CP100-workspace-scope-sweep.json` — three-scope page text and sanitized Admin API status matrix.
- `../evidence/CP100-members-scope-api-refresh.json` — members API counts, visible UI counts, actual selected scope after refresh, reselection and navigation.
- `../evidence/CP100-playground-scope-state.json` — explicit no-environment notice and `aria-disabled` result.
- `../evidence/CP100-webkit-scope-members.json` — independent WebKit 375 × 844 scope/API comparison.
- `../evidence/CP100-webkit-org-members-mobile.png`, `../evidence/CP100-webkit-proj-members-mobile.png`, and `../evidence/CP100-webkit-env-members-mobile.png` — visually inspected WebKit screenshots.
- `../evidence/CP100-webkit-scope-menu.png` — actual mobile workspace selector options.
- `../evidence/CP100-org-access-members.png`, `CP100-proj-access-members.png`, `CP100-env-access-members.png` — visually inspected false-empty/populated contrast.
- `../evidence/CP100-members-*.png` — read-only before/after refresh and navigation states across all three scopes.
- `../evidence/CP100-org-providers.png`, `CP100-proj-mcp.png`, `CP100-env-security.png`, `CP100-org-operations.png`, `CP100-org-playground.png`, and other `CP100-{scope}-{page}.png` files — real Chromium screenshots captured throughout navigation; named examples visually inspected.
- `../evidence/CP100-playground-disabled-without-environment.png` — visually inspected no-environment prompt.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006, and DEF-008–015 retain only their scoped QA-002 results; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 successful audit paths do not close populated erasure/failure atomicity.
- **Historical defects failing / unverified:** DEF-016–036 remain open. DEF-023 is freshly reconfirmed; DEF-032’s duplicate Google rows were re-observed without new identity/API testing. No prior disposition changed.
- **New defects:** none in CP100. Existing DEF-023 is reconfirmed; QA-OBS-009 recurred after refresh.
- **Coverage completed:** three workspace contexts across six additional Console surfaces; scope transitions, direct member API comparison, hard refresh, re-selection, navigation/re-entry, API status, and screenshot inspection in Chromium. WebKit independently reproduced DEF-023 at 375 × 844 with no document overflow.
- **Coverage blocked:** fresh Team Admin permission retest could not authenticate because the retained QA credential was rejected. CP71/CP88 remain prior scoped evidence; no new role PASS is claimed. Direct WebKit use of the HTTP-only origin did not persist a session; the successful WebKit comparison used the QA-only HTTPS forwarder described above.
- **Coverage remaining:** remaining Console surfaces and other scopes/roles, non-owner cross-project authorization, async scope races/error recovery, broader browsers and widths, and all earlier incomplete product/API/CLI/security/integration/operations scenarios.
- **Recommended engineering handoffs:** include CP100’s clean API/UI comparison in DEF-023. Keep DEF-023/024/029/030/035 scope acceptance tied together while preserving their distinct business contracts. Retain DEF-036, DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007 priorities.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
