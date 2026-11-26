# CP71 — Members & Roles workspace scope and role boundary retest

- **Baseline:** frozen RC4 source `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product implementation changed.
- **Result:** FAIL DEF-023, independently reconfirmed. PASS for the tested project-scoped Team Admin read boundary.
- **Runtime:** isolated A2A-enabled runtime at `127.0.0.1:18084`, real Chromium, synthetic QA accounts and existing QA-only tenant data.

## Org Owner journey

1. Authenticated as `qa-002-a2a-owner@example.invalid` and switched through the real workspace selector.
2. In **organization-only** scope, navigated to Access & Keys → Members & Roles. The screenshot displayed “Organization members 0” and “No members found.”
3. In the same session, `GET /admin/v1/members?limit=100` returned HTTP 200 with three persisted organization principals.
4. Selected project `QA002 Audit mwto47wa` with **No env**. The Members & Roles screen again displayed count 0 and “No members found.”
5. Selected `stage`. The same screen displayed all three persisted rows with roles.

No member or role was changed. The active scope in each screenshot was inspected visually. This independently reproduces DEF-023 on the frozen RC4 baseline with a separate three-member test dataset.

## Project-scoped Team Admin boundary

1. Authenticated separately as `qa002-team-admin@example.invalid`, assigned to `QA002 Audit mwto47wa`.
2. The workspace chooser showed that project, `No project`, `stage`, and `No env`; it did not expose organization choices.
3. Access & Keys → Members & Roles returned HTTP 403 `insufficient_role_permission`. The UI displayed the error. The zero count / “No members found” panel was also present underneath.

The denial and limited chooser are consistent with the tested role boundary. No cross-project member information or mutation was exposed. Preserve the secondary empty-state presentation as an adjacent usability observation, not as an authorization defect.

## Evidence

- `../evidence/CP71-members-organization-only.png`
- `../evidence/CP71-members-project-no-env.png`
- `../evidence/CP71-members-project-env.png`
- `../evidence/CP71-members-scope-comparison.json` — sanitized member fields and visible UI text; no credentials or tokens.
- `../evidence/CP71-team-admin-members.png`
- `../evidence/CP71-team-admin-scope-options.png`
- `../evidence/CP71-team-admin-login.png`

All listed screenshots were visually inspected during this phase.

## Disposition and limits

- **FAIL:** DEF-023 persists for an authorized Org Owner in organization-only and project/no-environment contexts, despite a successful three-row organization API response.
- **PASS:** Team Admin member listing remained forbidden with HTTP 403, and the scope chooser remained limited to its assigned project/environment.
- **UNVERIFIED:** member create/edit/revoke from project/environment scope; refreshing while organization-only or project/no-environment; other Console pages under the same scope transition; cross-project requests by Team Admin; other roles and browsers.
- **New defect IDs:** none.
- `GATE-REL-07` remains **BLOCKED**; QA-002 is in progress.
