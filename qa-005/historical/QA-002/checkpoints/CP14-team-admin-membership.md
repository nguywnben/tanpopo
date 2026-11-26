# CP14 — Project-scoped Team Admin membership

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Org Owner/Team Admin browser sessions on `127.0.0.1:18084`; synthetic accounts and database only. No Tanpopo implementation changed.

## DEF-006 acceptance slice

The Org Owner created a synthetic member with Developer + Team Admin and selected the current project scope. The real Console form sent `project_ids` and received HTTP 201. The persisted member row showed the project-scoped binding. This reproduces the original acceptance criterion for submitting project scope: **VERIFIED FIXED (scoped)**. Screenshots `CP14-def006-member-form.png`, `CP14-def006-scoped-team-admin-form.png`, and `CP14-def006-scoped-team-admin-persisted.png` document the owner path.

The runtime's Admin API creates the member but does not provision local login credentials or send an invitation. To verify an actual Team Admin session, QA set a random Argon2id password for only this synthetic principal in the isolated database using the product's password helper. Credential and hash remain in `/tmp` and are excluded from evidence. This test setup does not claim invitation or password provisioning is covered.

## Role-scoped session and DEF-016

A real Chromium login as the synthetic Team Admin showed the assigned Developer and Team Admin roles. The members screen issued an unfiltered collection request, which returned 403 `insufficient_role_permission`; UI displayed a permission error, zero members and “No members found.” The scoped Add member form submitted a new Developer with the authorized `project_ids`; mutation returned 201 and the row persisted. Immediately afterward, the same screen showed “Member added.” and the collection permission error simultaneously, with a zero-row table.

Under that authenticated session, unfiltered `GET /admin/v1/members?limit=100` returned 403 while the same endpoint with the authorized `project_id` returned 200 and included the new principal. Negative checks for an Org Owner grant and a different project each returned 403; database readback found no principal from either denied attempt. Outcomes are in `../evidence/CP14-team-admin-membership.json`; the actual UI states are captured in `CP14-def006-team-admin-session.png`, `CP14-def006-team-admin-add-form.png`, and `CP14-def006-team-admin-add-result.png`. The add-result screenshot was visually inspected during the run.

**DEF-016: CONFIRMED, Moderate.** Team Admin can create an authorized scoped member, but cannot see or manage that member in the Console because the refresh is organization-wide. This is not a server authorization failure; the API's project-filtered request succeeds and out-of-scope grants fail closed. See `../reports/DEF-016-team-admin-member-list-refresh.md` for full reproduction, expected/actual behavior, impact, root-cause hypothesis, and acceptance criteria.

## Coverage update and limits

- DEF-006: scoped create acceptance slice verified fixed.
- DEF-016: member list/refresh workflow failed; engineering fix required.
- FT-09, SCR-13 and Team Admin authorization: partial pass with DEF-016 open.
- Single authorized project only. Multiple project filters, member edit/removal, pagination, invitation delivery, alternate roles, tenant boundaries and other browsers remain UNVERIFIED.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
