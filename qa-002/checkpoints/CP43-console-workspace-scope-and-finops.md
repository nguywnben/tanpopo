# CP43 — Console workspace-scope consistency and FinOps budget visibility

- **Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (RC4; product source unchanged)
- **Result:** FAIL (DEF-023 reproduced and expanded to no-environment; DEF-024 confirmed). Other scoped observations below remain unclassified.
- **Runtime:** isolated SQLite runtime at `127.0.0.1:18086`; real Chromium interactions over local HTTPS proxy `127.0.0.1:18445`; screenshot images were opened and visually inspected. Temporary organization owner was a synthetic QA principal. No secrets, raw database, or runtime logs are part of evidence.
- **Existing QA data:** added one synthetic project, one environment, and three synthetic budget rows through Console workflows. These remain in the disposable QA database for follow-up scope regression; no provider spend/inference was sent.

## Work performed

1. Signed in as synthetic ORG_OWNER and inspected Console workspace chooser, then navigated among organization-only, first-project/no-environment, and project/environment scopes.
2. Reproduced DEF-023 in both organization-only and project-without-environment contexts. The same authenticated `/admin/v1/members` response contained the persisted owner, but Access & Keys → Members & Roles showed count zero and “No members found.” On selecting a project with environment, it showed the same row. Reload reset selection to the runtime's initial workspace and restored the row; this is a separate scope persistence/default observation, not classified as a defect here.
3. Created CP43 secondary project and environment in the real browser. In that selected project, Members & Roles displayed the organization member row, confirming the page content is organization-wide once both project and environment are present.
4. Created an organization budget and project/environment budgets using the real FinOps form. The organization-only scope listed the organization row. The secondary project, with no local budget, displayed “No budgets configured” while the unfiltered API still returned the persisted organization ceiling and the project-filtered API returned an empty list. This confirms DEF-024 (report linked above). Project 1's local project/environment rows were persisted and displayed; the organization row was omitted in the selected-project list.
5. Checked org-only Routes & Policies, Traces, Playground, MCP Hub, A2A tab, Access & Keys, and FinOps. Screenshots visually inspected: org-only routes and policies require project/environment context; traces are an empty state with successful API response; Playground explains that an active environment is required and Send is disabled; MCP lists seven project-1 servers in org-only and secondary-project contexts; A2A agents/tasks are empty in both. MCP ownership labelling remains QA-OBS-008 pending product/API contract clarification.
6. No policy, role assignment, key, provider, routing, task, or source-code mutation was performed. No test was run as part of this phase.

## Evidence

- `../evidence/CP43-scope-menu.png`
- `../evidence/CP43-project-no-env-members.png`
- `../evidence/CP43-org-only-access-members.png`
- `../evidence/CP43-project-no-env-members-reload.png`
- `../evidence/CP43-project2-created.png`
- `../evidence/CP43-project2-env2-created.png`
- `../evidence/CP43-project2-members.png`
- `../evidence/CP43-org-budget-created.png`
- `../evidence/CP43-project-budget-created.png`
- `../evidence/CP43-environment-budget-created.png`
- `../evidence/CP43-project2-finops-budget-retest.png`
- `../evidence/CP43-org-only-routing.png`
- `../evidence/CP43-org-only-traces.png`
- `../evidence/CP43-org-only-playground.png`
- `../evidence/CP43-org-only-mcp.png`
- `../evidence/CP43-project2-mcp.png`
- `../evidence/CP43-org-only-a2a.png`
- `../evidence/CP43-project2-policies.png`

## Results and boundaries

| Case | Result | Evidence/limits |
| --- | --- | --- |
| DEF-023 org-only member collection display | FAIL, independently reproduced | UI count 0/no members; same-session API count 1. |
| DEF-023 project selected, environment absent | FAIL, expanded | UI still count 0 despite organization-wide API row. |
| Members after selecting project + environment | PASS for this state | UI shows same organization owner; does not close project-without-env/org-only failure. |
| Workspace selection after reload | PARTIAL / UNVERIFIED contract | Fresh page chooses runtime's initial project/environment, not previous no-env/org-only state; no account preference contract found. |
| DEF-024 organization budget visibility in project scope | FAIL | Organization ceiling persists and is documented as an enforced ancestor scope, but project view returns empty and says no budget. No inference spend or enforcement was attempted. |
| MCP Hub cross-project rows in ORG_OWNER view | QA-OBS-008, unresolved | Seven first-project server rows remain visible under no project and secondary project; API is org-wide for owner. No unauthorized access observed; intended UI scope needs contract clarification. |
| Routes / policy in org-only scope | BLOCKED for scope editing; empty page explained | UI says select project/environment. No mutation attempted. Whether org policy management should be available there is unverified. |
| Traces in org-only scope | PASS for empty-state behavior only | UI empty state; `/admin/v1/traces` returned 200. No org-level traces existed in this test data. |
| Playground in org-only scope | PASS for guard state only | UI instructs selection of active environment; Send disabled. No request attempted. |
| A2A org-only/project2 tabs | PASS for empty-state behavior only | No agents/tasks in fixture; broader task/workspace scope behavior remains open. |
| DEF-005 SSRF and private endpoint form | No new test this phase | CP08/09/24/25/40/41/42 evidence and remaining gaps remain in register; DNS rebinding, redirect contract, alternate runtimes and mapped-address contract remain open. |

## Consolidated defect status summary

Historical status is QA-002 evidence only; no QA-001 PASS is inherited. “Verified fixed (scoped)” means the linked post-baseline CP retest passed its specific acceptance path and is not a full product clearance.

- **DEF-001 through DEF-015:** DEF-001–004 and DEF-006–015 are VERIFIED FIXED for the exact scenarios recorded in the QA-002 retest register. DEF-005 is PARTIAL PASS; redirect behavior, DNS rebinding, alternate runtimes, and IPv4-mapped notation support remain open. Broader retesting across roles/platforms remains required.
- **Confirmed open defects:** DEF-016, DEF-017, DEF-018, DEF-019, DEF-020, DEF-021, DEF-022, DEF-023, and DEF-024. CP43 independently reproduced/expanded DEF-023 and first confirmed DEF-024.
- **Open observations:** QA-OBS-001 and QA-OBS-003 through QA-OBS-008; QA-OBS-002 is not listed in the current report directory. QA-OBS-008 is the MCP Hub list-scope label contract. Prior observations and uncertainty remain unchanged.
- **Recommended handoff:** continue the existing defect handoff for DEF-016–023; add DEF-024 to FinOps engineering intake. Prioritize DEF-017/018/020 and DEF-023/024 for user-facing consistency, and keep DEF-005 security validation distinct from any UX defect. DEF-021 requires an explicit local enrollment/business contract.

## Coverage status

- **Completed in this phase:** real-browser scope changes across org-only, project/no-env, project/env and two projects; refresh/navigation; Members page state; FinOps persisted rows and scope-filtered UI/API comparison; org-only empty/guard states on Routes, Traces, Playground and A2A; visual inspection of every screenshot linked above.
- **Blocked:** no CP43 capability was blocked. Editing organization policy from org-only view was not attempted because the UI requires project/environment; exact product contract is unresolved. Google provider availability and configured provider credentials are outside this phase and remain as in earlier checkpoints.
- **Remaining:** repeat authorization matrix with non-owner roles in organization-only and cross-project contexts; verify scope switching under latency/error and multiple browser tabs; org-policy management expectations; trace data in multiple envs; MCP per-project visibility/actions and table labelling; effective budget visibility and enforcement with isolated synthetic spend; complete independent retests for historical defects and DEF-016–024; residual product screens, integration failures, Firefox/browser states and PostgreSQL/Valkey remain incomplete.

`GATE-REL-07` remains **BLOCKED**. QA-002 continues; this checkpoint is not a completion claim.
