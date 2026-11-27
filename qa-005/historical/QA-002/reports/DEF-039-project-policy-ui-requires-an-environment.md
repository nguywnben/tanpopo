# DEF-039 — Policies page blocks project policy management without an environment

**Severity:** Low  
**Status:** CONFIRMED on frozen RC4 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Affected workflows:** Project policy setup, project onboarding, policy review before environment provisioning  
**Principal tested:** QA Org Owner + Operator on isolated runtime 18084

## Reproduction

1. In real Chromium, select organization `QA-002 A2A Org`, project `QA002 Audit mwto47wa`, and `No env` in the workspace chooser.
2. Open Routes & Policies → Policies. The Console displays “Select a project and environment in the top bar to manage routes and policies.” It does not render the effective policy table or editor.
3. During this project-only visit, the Console's authenticated `GET /admin/v1/policies/effective?project_id=prj_01m84ymb7we009px5hb5pfwsns` returned HTTP 200 with ETag `"v1"`. No write request was made.
4. Select the same project's `stage` environment. The Policies view now shows the effective policy table and editor. “Scope to edit” offers Organization, Project, and Environment. Select Project: the project editor appears with its current form values and Save remains disabled because no field was changed.
5. Navigate to Overview and back through the Console navigation. The environment selection and policy view remain coherent. Hard refresh resets to the runtime default `QA002 CP78 Fresh Budget / No env`; Policies again displays the environment prerequisite (recurrence of QA-OBS-009, not a separate defect).

Screenshots were inspected after each interaction: organization-only prerequisite, project/no-environment prerequisite, environment policy table, editor scope options, project editor selected, and hard-refresh state. The authenticated account was authorized as an Org Owner; no authorization bypass or cross-tenant request was attempted. No policy, route, project, or environment was modified.

## Expected versus actual

- **Expected:** A project-level policy can be reviewed and managed when that project is selected, even if it has no active environment. The project policy tier is part of the documented Organization → Project → Environment hierarchy; the UI itself exposes Project as a policy edit scope once an environment is selected. Environment-scoped policy controls may still require an environment.
- **Actual:** The Policies tab is hidden behind a generic project-and-environment prerequisite. In project-only scope the API successfully returns the effective document, but the UI omits both the project policy and its editor. Selecting an unrelated environment in the same project is required before a user can choose Project scope.

## Business impact

An owner or project administrator cannot configure a project's policy during project onboarding until an environment exists. This creates an undocumented ordering dependency between project governance and runtime-environment provisioning and makes the policy screen's required scope broader than the project policy being edited. The tested path was read-only; no weakened policy or authorization was observed.

## Evidence and contract basis

- [CP113 checkpoint](../checkpoints/CP113-project-policy-scope-and-console-state.md)
- `../evidence/CP113-org-only-policy-prerequisite.png` — organization-only state and explicit prerequisite.
- `../evidence/CP113-project-no-env-message-settled.png` — project selected, no environment, prerequisite visible.
- `../evidence/CP113-environment-policies-closed.png` — environment-selected effective policy table and editor.
- `../evidence/CP113-policy-edit-scope-options.png` — editor offers Organization, Project, and Environment.
- `../evidence/CP113-project-policy-editor-selected.png` — Project editor visible; unchanged form; Save disabled.
- `../evidence/CP113-policy-after-refresh-closed.png` — refresh returns to runtime-default project/no-environment and hides policy editor.
- `../evidence/CP113-policy-project-only-ui-api.json` — sanitized workspace/API response evidence (HTTP statuses, effective-policy paths/ETags, visible UI summaries, zero mutations).
- Contract references: `docs/planning/units/TP-IMP-0029.md` describes a hierarchical `Org -> Project -> Env` policy manager; `docs/requirements/business-entity-map.md` assigns project policy administration to Team Admin; OpenAPI `getEffectivePolicy` accepts `project_id` without requiring `env_id`.

## Root-cause hypothesis (not established)

The Routes & Policies screen appears to gate both route management and policy management on the same active-environment condition. That shared prerequisite is consistent with the UI message and API/UI comparison. The test does not establish whether the gate is implemented in a common page component or is an intentional but undocumented policy contract.

## Acceptance criteria

1. With an authorized project selected and no environment selected, the Policies tab displays the effective Organization/Project policy state and permits editing the Project tier.
2. Environment-tier policy editing remains unavailable until an environment is selected, with scope-specific copy that explains the prerequisite.
3. Route creation and environment-specific route editing retain their environment requirement; this finding does not request weaker route authorization or relaxed policy inheritance.
4. Project policy writes continue to enforce role authorization, `If-Match`/ETag concurrency, monotonic narrowing, audit attribution, and `422 policy_relaxation_forbidden` for attempted parent-policy relaxation.
5. Organization/project/environment switching, navigation, refresh, and a fresh session render the same persisted policy values and provenance; failed loads are shown as errors rather than empty/default policies.
6. Retest both Org Owner and assigned Team Admin project-scope workflows. No role or tenant boundary is broadened.

**Limits:** Owner-only read/editor scope was exercised. A Team Admin project-policy edit, write/conflict/relaxation behavior, organization-only policy editor contract, other browsers/platforms, and a fresh-session policy journey remain UNVERIFIED. `GATE-REL-07` remains **BLOCKED**.
