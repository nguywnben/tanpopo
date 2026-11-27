# CP110 — Local HUMAN member enrollment on a second runtime

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated RC4 SQLite runtime on 18084  
**Result:** **FAIL DEF-021 Moderate reconfirmed; DEF-023 also reconfirmed**  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Reproduction

1. Signed into 18084 as the authorized Org Owner in a real Chromium browser. Opened **Access & Keys → Members & Roles** under the selected project with no environment. The organization member table showed `0` / “No members found.”
2. Opened **Add member**. The form exposed email, role, and scoped-project fields, but no password enrollment, invite, setup-token, or identity-provider fields. Added the synthetic `HUMAN` principal `qa002-cp110-local-onboarding-20261124@example.invalid`, scoped to project `prj_01m868q2jte00abmhf75asqts3` (exact ID in sanitized evidence). The API returned HTTP 201; UI said “Member added.”; role was `DEVELOPER`.
3. The same no-environment screen continued to show `0` / “No members found.” An authenticated explicit-organization `GET /admin/v1/members?limit=100` returned HTTP 200/count 5 and included the new principal. Selecting another project with its `stage` environment caused the UI to load five members and show the synthetic Developer. This is a fresh DEF-023 reproduction; no unauthorized access occurred.
4. In a new Chromium context without owner cookies, opened `/login` and attempted the synthetic address with a throwaway value never issued to that principal. The actual login request returned HTTP 401 and the page showed “Sign-in failed. The email address or password is not valid.” The page provided no invite, activation, enrollment, or reset action; it did show the new-instance first-time setup link, which is not a member-enrollment path.

The new `member.created` event was visible as sequence 548 with the owner actor, synthetic principal target, and role/project metadata; the Audit Log UI verified the JCS chain intact across 548 events. This confirms the successful append path for this operation only and does not close DEF-007 fault atomicity.

Visual evidence: the add-member form without any enrollment fields, success banner paired with the empty organization view, fresh-browser login rejection, populated list after scope change, and verified audit row are retained under `../evidence/CP110-*.png`. Sanitized request and state facts are in [CP110 evidence JSON](../evidence/CP110-local-member-onboarding.json). No password, token, database or raw log was retained.

## Result and limits

- **DEF-021 Moderate:** independently reconfirmed that the supported standalone local-auth workflow creates a role-bearing HUMAN principal but gives it no documented usable credential/enrollment/recovery path. The exact intended contract remains uncertain: a future external identity provider might be expected, but none is configured and the Console does not communicate that prerequisite before it reports success.
- **DEF-023 Moderate:** independently reconfirmed on the same runtime. Under project/no-environment scope, UI displayed zero after creation while the explicit-organization API returned five. Selecting project + environment loaded the same persisted member list. The API permission boundary remained same-tenant; no bypass was attempted or observed.
- The newly created synthetic Developer remains in the isolated QA database because the Console exposes only Edit roles and the Admin API contract has no member DELETE route. It has no credential and its throwaway failed-login value was never issued. No direct database edit was used for cleanup.
- No new defect was assigned. The missing delete route is recorded as a test-data cleanup limitation rather than a product defect because no deletion requirement was established in the contract reviewed.

## Coverage disposition

- **FAIL:** local HUMAN member first login/enrollment, DEF-021; org-only/project-without-env member list consistency, DEF-023.
- **PASS, scoped:** creation persists the requested Developer assignment; switching to project/environment displays the same member records; unauthenticated new context received 401 and no protected session.
- **UNVERIFIED:** intended enrollment identity contract, invitation security/expiry/replay, successful member first login, role boundary after enrollment, removal/deprovisioning requirements, failure atomicity, other roles and browser/platform combinations.

## Consolidated status

- **Historical DEF-001–015:** scoped QA-002 outcomes only, no QA-001 PASS inherited; DEF-005 remains PARTIAL and DEF-007 remains partial for failure atomicity/erasure proof.
- **Other open findings:** DEF-016–020, DEF-022–038 remain preserved/open. CP110 strengthens existing DEF-021 and DEF-023 and verifies a successful member.created audit append/548-event chain for DEF-007; it adds no new defect and removes no earlier evidence.
- **Coverage completed:** new real UI-created HUMAN principal, persistent role readback, fresh-session login attempt/401, workspace-scope cross-check, visual screenshot inspection.
- **Blocked/remaining:** all other full-product workflows and incomplete historical/new defect acceptance remain. No role/session enrollment boundary after successful setup can be tested until a supported enrollment route exists.
- **Recommended handoffs:** carry the fresh CP110 reproduction to the DEF-021 onboarding handoff and preserve CP110’s DEF-023 same-record scope comparison. Continue DEF-038 jointly with DEF-007 and keep SSRF/authorization boundaries intact.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
