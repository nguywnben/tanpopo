# CP26 — Project policy UI without an environment

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic bootstrap Owner on the default project  
**Disposition:** DEF-039 no longer reproduces on the read/editor-render path; **PARTIAL** overall. No policy write was issued. `GATE-REL-07` remains **BLOCKED**.

## Independent retest

1. Signed in using the isolated synthetic owner account and selected `Default Organization / default / No env` in the real workspace selector. [Scope screenshot](../evidence/CP26-policy-project-no-env-scope.png).
2. Opened Routes & Policies → Policies. Unlike the frozen-RC4 reproduction in CP113, the RC5 page rendered the Effective resolved policy table and a Project-scope editor while no environment was selected. The form fields were enabled for this owner; Save was disabled because no field had changed. [Visually inspected policy screenshot](../evidence/CP26-policy-project-no-env.png).
3. The browser observed authenticated `GET /admin/v1/policies/effective?project_id=prj_01m87axgx7e008ehq1d016vqwt` and `/admin/v1/policies/effective` returning HTTP 200. A separate in-page authenticated read of `/admin/v1/projects` selected project slug `default`; the project effective-policy read returned HTTP 200, ETag `"v1"`, `scope_tier: PROJECT`, the matching project scope ID, and version 1. Sanitized response details are in [evidence JSON](../evidence/CP26-def039-project-policy.json).
4. No policy, route, project, or environment was mutated. Therefore persistence following a write, `If-Match` conflict, monotonic-relaxation denial, audit attribution, and Team Admin write access were not exercised.

## Reconciliation and disposition

The prior RC4 finding (QA-002 CP113) was grounded in inspected screenshots and a successful project-level API read while the UI showed an environment prerequisite. The RC5 run contradicts that UI behavior: the same class of project-only scope now displays the policy and Project editor without an environment. This is evidence that the reported gate is not present in this tested RC5 owner path; the difference is not explained by a code change in this QA campaign. The RC4 evidence remains preserved and is not overwritten.

The original acceptance criteria require actual editability, role authorization, concurrency protection, monotonic narrowing, audit attribution, and consistent persisted values across scopes and sessions. RC5 establishes that the Project editor is exposed and owner-editable fields render in the no-environment scope, but a disabled Save on an unchanged form does not prove that a save succeeds. Thus this is **PARTIAL**, not complete acceptance closure or a verified fix. The historical defect remains open pending an authorized write/re-read cycle and the other criteria.

No new defect is filed. No security boundary was bypassed or weakened. No policy mutation was made.

## Coverage update

- `SCR-09` changed from UNVERIFIED to PARTIAL for project-only policy read and editor rendering in Chromium.
- DEF-039 changed from UNVERIFIED to PARTIAL; the historical RC4 block is not reproduced for the tested RC5 owner path.
- Historical register totals: 17 VERIFIED FIXED for scoped RC5 scenarios, 4 PARTIAL (DEF-005, DEF-007, DEF-032, DEF-039), 18 UNVERIFIED. A scoped fixed result does not close all acceptance criteria.
- Remaining coverage includes Team Admin project policy write, write persistence and audit, stale ETag conflict, attempted relaxation rejection, environment-tier behavior without an environment, organization-only policy UX, cross-scope/fresh-session consistency, other browsers/platforms, and full SCR-09 acceptance.
- QA-003 remains incomplete. No new defect was confirmed in CP26. `GATE-REL-07` remains **BLOCKED**.
