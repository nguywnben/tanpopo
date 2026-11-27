# CP72 — Team Admin member create/list refresh independent retest

- **Baseline:** frozen RC4 commit `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product implementation changed.
- **Result:** FAIL DEF-016; PASS for the server-side role escalation and project-scope boundaries already covered in CP14/CP45, with this phase limited to the authorized create + forbidden list path.
- **Runtime:** isolated A2A-enabled QA runtime `127.0.0.1:18084`; real Chromium; separate synthetic Team Admin and Org Owner sessions.

## Reproduction and state verification

1. Signed in as `qa002-team-admin@example.invalid`, a Developer + Team Admin assigned only to `QA002 Audit mwto47wa`.
2. Opened Access & Keys → Members & Roles. The unscoped member GET returned HTTP 403 `insufficient_role_permission`; the UI showed a zero count, “No members found,” and an explicit permission error.
3. Opened Add member. The form defaulted to the assigned project. Entered `qa002-cp71-def016@example.invalid` with Developer only and that one project selected.
4. The real Console POST `/admin/v1/members` returned HTTP 201 with one project ID. Its automatic unfiltered GET `/admin/v1/members?limit=100` returned HTTP 403. The UI showed “Member added.” and “Members: insufficient role permission” at the same time and still showed zero members.
5. Signed in separately as Org Owner. The same principal appeared in the organization member API and the real Members & Roles table with Developer and the expected project binding. Screenshot was visually inspected.
6. Signed in again as Team Admin and queried the authorized project-filtered collection directly through the authenticated browser session. `GET /admin/v1/members?project_id=<assigned-project>&limit=100` returned HTTP 200 with three assigned-project principals, including the newly created member. This is the positive authorization control; the Console refresh omits the project filter and receives 403.

This independently reconfirms DEF-016 and also shows the adjacent DEF-023 empty-state mismatch under a role whose member read is denied: the error is visible, so no authorization leak was observed. The authorized create response and separate-owner read verify persistence.

## Evidence

- `../evidence/CP71-def016-submit.png` — real scoped member form immediately before submit.
- `../evidence/CP71-def016-after-create.png` — visually inspected contradictory success/error notices and zero-count state.
- `../evidence/CP71-def016-create-result.json` — sanitized request method, status, response excerpt and visible text.
- `../evidence/CP71-def016-owner-sees-created-member.png` — visually inspected owner table with the persisted scoped principal.
- `../evidence/CP71-def016-owner-edit-member.png` — confirms the role editor exposes no delete action.
- `../evidence/CP72-def016-scoped-members-api.json` — project-filtered Team Admin read returns 200 and includes the new scoped member.

## Test-data note and limits

The disposable principal is retained in the isolated QA runtime. The owner UI offers role editing but no member removal control, and the documented Admin API defines list, create, and role update but no delete operation. No login credential was provisioned for the new principal, and it was not used to authenticate. This additive QA fixture is labelled in the evidence and has only Developer on the QA project. The owner’s four-row list now includes the new test principal; the earlier CP71 three-member screenshots and response remain historical snapshots from before this CP72 create.

Update, membership revocation, pagination, multiple authorized projects, and cross-project denied create remain UNVERIFIED in this phase. No new defect ID was assigned. `GATE-REL-07` remains **BLOCKED**.
