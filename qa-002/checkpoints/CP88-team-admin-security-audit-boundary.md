# CP88 — Team Admin Security audit authorization boundary

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated QA runtime `http://127.0.0.1:18084`  
**Role/browser:** project-scoped Team Admin, real Chromium at 1440×900  
**Gate:** `GATE-REL-07 BLOCKED`

## Reproduction and observed behavior

1. Sign in as the existing QA-only Team Admin assigned to project `QA002 Audit mwto47wa`.
2. Confirm the project/environment selector offers the assigned project and environment, not organization-wide scope.
3. Open Security → Audit Log.
4. Inspect the UI, network response, and persisted/displayed data.

The Security navigation and Audit Log tab were visible. The real Console request `GET /admin/v1/audit-events?limit=100` returned HTTP 403. The page displayed “insufficient role permission”; it showed no event details. The collection was not available through this role.

## Classification

**PASS — intentional authorization restriction; no defect assigned.** Frozen-baseline RBAC grants Team Admin project-scoped kill-switch and approval actions, but omits `audit:read_and_verify`. The shared Security screen therefore presents the audit tab but accurately denies its data request. No authorization bypass or cross-project data exposure occurred. This phase did not submit a kill-switch, approval, or audit mutation.

The empty count/empty-state content underneath the explicit denial is not treated as a second defect here; no contract requires Team Admin audit visibility, and the denial is clear. Whether navigation should hide the Audit Log tab for this role is a UI preference question without an established requirement.

## Evidence

- `evidence/CP88-team-admin-audit-boundary.png` — actual denial state, visually inspected.
- `evidence/CP88-team-admin-audit-boundary.json` — sanitized route, role, request status, and rendered result.
- `evidence/CP88-team-admin-killswitch-dialog.png` — same Security screen screenshot; no dialog or mutation occurred (retained as a visual check of the rendered action area).

## Consolidated status

- **Historical DEF-001–015:** CP88 did not retest these defects; prior scoped results remain intact, DEF-005 remains partial, and independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Confirmed open defects:** DEF-016–034 remain confirmed open; no status change in CP88. DEF-034 and CP86 DEF-020 recurrence remain preserved.
- **New findings:** no new defect. Team Admin audit denial is an intentional RBAC boundary; UI error and 403 agree.
- **Coverage completed:** one real Team Admin session, visible Security route, direct audit page, network 403 and visual denial; no protected audit row was returned.
- **Blocked:** managed Cloud direct egress still blocks actual webhook delivery/HMAC; runtime 18445 A2A is configuration-disabled; RC4 artifact verification remains unverified in the unavailable separate DevBox.
- **Remaining:** independent DEF-001–015 acceptance remains incomplete; DEF-005 unresolved SSRF cases; DEF-007 failure atomicity and populated erasure; remaining open DEF-016–034 remediation/regression; other roles and scopes for Security; Chargeback scope/date UI contract; actual safe-abort upgrade; full browsers/platforms/features/integrations/accessibility/operations.
- **Recommended handoffs:** preserve the existing priorities: DEF-034, DEF-016/023/029/030, DEF-033, DEF-005, DEF-007. No handoff is added from this intentional denial.

QA-002 remains in progress; do not infer release readiness.
