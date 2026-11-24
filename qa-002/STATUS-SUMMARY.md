# QA-002 consolidated status summary

**Frozen baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Latest checkpoint:** CP79
**Release gate:** `GATE-REL-07 BLOCKED`  
**QA state:** In progress; coverage remains incomplete. QA-001 results are not inherited.

## Historical defects DEF-001–015

| Status | IDs | Notes |
| --- | --- | --- |
| Verified fixed for scoped post-baseline scenarios | DEF-001–004, DEF-006–015 | Each passes only the specific QA-002 retest scenarios recorded in [DEFECT-RETEST-REGISTER.md](DEFECT-RETEST-REGISTER.md). This is not a full role/platform regression. |
| Partial pass; residual security/support coverage open | DEF-005 | Explicit private-IP/CIDR controls passed for executed cases; CP68 completed one enabled-runtime private A2A register/discover/task journey. CP66/67 confirm mobile invalid-CIDR errors hidden behind both drawers while APIs still reject safely. Redirects were not followed; DNS rebinding, mixed answers, alternate runtimes, and IPv4-mapped endpoint support remain unresolved. See QA-OBS-004/007 and CP25/40–42. |

## Confirmed open defects

| ID | Severity | Current summary |
| --- | --- | --- |
| DEF-016 | Moderate | Team Admin member creation succeeds, but unscoped member-list refresh is forbidden and UI reports failure/empty list. |
| DEF-017 | Moderate | Playground polls a request ID where trace lookup expects a trace ID; success card remains pending. |
| DEF-018 | Moderate | FinOps/API report Virtual Key lifetime budgets as monthly. |
| DEF-019 | Moderate | Playground hides canonical budget rejection reason; CP51 extends the generic-429 reproduction to organization enforcement on a child project/environment request. |
| DEF-020 | Moderate | Security shell badges remain stale after clearing switches or deciding approvals until reload. |
| DEF-021 | Moderate | Local HUMAN member creation has no supported enrollment path for first login. |
| DEF-022 | Moderate | Session UI retains revoked role/navigation/actions until reload after server rejects the session. |
| DEF-023 | Moderate | Organization member list is falsely empty without both project and environment selected; CP71 independently reconfirmed with three persisted members and a Team Admin boundary comparison. |
| DEF-024 | Moderate | Project FinOps view hides an applicable persisted organization ceiling and displays a false empty state. |
| DEF-025 | Low | Models enables Add deployment without environment scope; its form cannot select an environment and reports a page-level validation after submission. |
| DEF-026 | Low | MCP sync banner reports zero schema changes requiring approval while a first-discovered schema is visibly locked pending review. |
| DEF-027 | Moderate | Routes & Policies cannot distinguish same-model deployments with identical region/health labels; provider and deployment identity are hidden from target choices and route summaries. |
| DEF-028 | Moderate | Route Explain hardcodes `openai_compatible` and rejects healthy Ollama deployments as dialect-mismatched even when the same route completes a real Playground request. |
| DEF-029 | Low | Access & Keys can repopulate prior environment-scoped Virtual Key metadata after a delayed response arrives under No project / No env scope. |
| DEF-030 | Moderate | Routes & Policies can repopulate prior environment routes after a delayed response under No project / No env or a different selected project/environment; stale Edit/Delete remain available. |
| DEF-031 | Low | Invalid MCP/A2A CIDR reaches the API, and the 400 validation error is obscured behind the open drawer on WebKit mobile. APIs reject before persistence; no security bypass. |
| DEF-032 | Low | Distinct same-project provider records can be indistinguishable in Providers table, actions and edit forms when display fields duplicate. CP76 did not probe or mutate either record. |
| DEF-033 | Moderate | FinOps Create budget can update an existing same-scope ceiling once while reporting Created, then submit fixed revision v1 and fail 409 at v2. CP78 restored the tested cap. |

All details, reproduction steps, expected behavior, evidence and acceptance criteria are in the linked [defect reports](reports/). DEF-029 is a distinct delayed-response issue from DEF-023’s false empty organization member list. DEF-030 confirms the same general scope-race class in Routes & Policies, where stale routes expose Edit/Delete and enabled Save. DEF-031 concerns invalid-CIDR form feedback in both MCP and A2A and does not weaken or bypass API enforcement. CP47 adds a scoped loopback positive control and confirms Team Admin cross-project sync denial before fixture contact; DEF-026 captures the inconsistent first-review notice.

## Latest phase: CP79

- **Historical defects verified fixed:** DEF-001–004, DEF-006–015 retain only their individually scoped QA-002 results. CP79 adds successful project-provisioning audit evidence to DEF-007; it does not prove failure atomicity or close DEF-007. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–033 remain confirmed open as listed above. Their broader acceptance criteria remain incomplete. DEF-007 failure injection, organization mutation/delete, storage-fault, and cross-tenant paths remain UNVERIFIED.
- **New findings:** no new defect ID in CP79. Confirmed new defects remain DEF-016–033, including CP33's latest DEF-033 Moderate. QA-OBS-001–014 remain preserved; QA-OBS-013/014 are unresolved observations, not confirmed product defects.
- **Coverage completed:** real Chromium Security → Audit Log filtering found the CP78 `project.created` sequence 300 event and API evidence confirmed owner actor, project target, organization partition, followed by `budget.created` sequence 301. UI and API verification both reported the RFC 8785 JCS chain intact across 315 events. Screenshots were visually inspected.
- **Coverage blocked:** A2A runtime 18445 remains disabled by configuration. Webhook delivery still requires usable Operator access and a controlled receiver/signing setup. RC4 artifact verification remains UNVERIFIED because the separate DevBox artifact is unavailable in this workspace.
- **Coverage remaining:** independent end-to-end retesting of DEF-001–015 acceptance criteria; DEF-005 DNS rebinding, mixed answers, redirect-chain and alternate-runtime cases; DEF-007 failure and mutation paths; other screens, workflows, roles, integrations, browsers, locales, viewports, accessibility, and operational/platform scenarios. Existing reports retain each PASS, FAIL, BLOCKED and UNVERIFIED scope.
- **Recommended handoffs:** retain the current remediation priorities for DEF-016–033, especially DEF-023/029/030 workspace state consistency and DEF-033 budget mutation/revision feedback. Preserve the separate security and contract checks for DEF-005 and the audit atomicity/attribution checks for DEF-007.
- **Checkpoint:** [CP79](checkpoints/CP79-provisioning-audit-event-and-chain.md), [CP78](checkpoints/CP78-finops-budget-create-persistence-and-existing-scope-retest.md), [CP77](checkpoints/CP77-provider-list-workspace-scope-comparison.md).

## Open observations

- QA-OBS-001: A2A graceful key revocation during a dispatched task; contract unresolved.
- QA-OBS-003: overview presents “All systems nominal” despite substantial request failures; CP53 recurred at 4 requests / 25% error / zero alerts.
- QA-OBS-004: MCP CIDR sync failure uses generic retryable diagnostics.
- QA-OBS-005: budget activation/backfill semantics.
- QA-OBS-006: trace and settlement identifiers differ; correlation contract unresolved.
- QA-OBS-007: IPv4-mapped private endpoint notation rejected; supported endpoint contract unresolved.
- QA-OBS-008: MCP Hub org-wide list shown without project labels; global vs selected-project presentation contract unresolved.
- QA-OBS-009: manual project/environment selection returns to the runtime-selected default after refresh; workspace persistence contract unresolved.
- QA-OBS-010: A2A agent registration and A2A-only key provisioning remain available in a runtime whose A2A data-plane handlers return the configured disabled response; control-plane/data-plane availability and UX contract unresolved (CP59).
- QA-OBS-011: legacy numeric host `127.1` resolves to loopback through system NSS but MCP registration rejects it as a DNS resolution failure; support is undocumented, and the endpoint is not contacted (CP60).
- QA-OBS-012: one project-switch run displayed `env_id does not belong to supplied project_id` while switching workspace; a request-logged follow-up returned 200 for both old and new scopes and settled with no error. Keep unclassified pending a repeatable capture (CP70).
- QA-OBS-013: Provider Connections lists organization-wide rows in selected-project context without owner-project labels; the Org Owner API behavior is intentional by authorization design, but presentation contract remains open (CP77).
- QA-OBS-014: repeated A2A-project FinOps captures differed on whether a project budget row existed before the CP78 write; the pre-action API response was not retained, so the cause is unresolved and no defect is assigned.

## Coverage

- **Completed in CP43–58:** real Chromium scope switching across organization-only, project/no-environment, project/environment and a second project; navigation/refresh; visually inspected Access, FinOps, Routes, Traces, Playground, MCP, A2A, Overview, Providers, Models, Extensions and Security states; API/persisted-state comparison for members and budgets; Models no-environment create attempt generated validation but no mutation; real Team Admin sessions confirmed project chooser/MCP filtering and 403 on different-project reads/sync. CP47 owner sync contacted only the allowlisted synthetic fixture, visually exposed the first-schema lock/status mismatch, and owner pin resolved it. CP48 delayed a project1 FinOps budget response during a real workspace switch; skeletons replaced old rows and the selected project rows appeared after settlement. CP49 confirmed refresh returns a manually selected project to the fresh-login/runtime default; affected rows changed with scope and remained persisted, leaving preference behavior as QA-OBS-009. CP50 exercised HTTP 302/303 MCP redirects; both failed closed before target contact with no tool persistence. CP51 exercised one synthetic child project/environment route under an organization budget: positive control settled 2 micro-USD; reduced org limit caused a generic Playground 429 with canonical trace reason and no upstream dispatch; limit restored and temporary Playground keys revoked. CP52 exercised a two-target priority-failover stream: first synthetic provider returned 503, backup returned 200 and the trace waterfall recorded both ordered deployment IDs; a temporary key was revoked. The route UI rendered three distinct deployments with identical labels (DEF-027). CP54 used Route Explain on the same route: UI returned zero eligible deployments and two dialect rejections, while an API dry-run with dialect `ollama` selected both; the same route then returned a real 200 Playground stream selecting the primary. CP55 exercised a real bearer-only RPM-limited Data Plane key: 200 then fail-fast 429/Retry-After without egress, then 200 after the interval, followed by Console revocation. CP56 used the real policy UI to set EU-only organization residency, verified it resolved at Organization/Project/Environment API scopes, visually confirmed child editors exposed only EU, and restored all four regions. CP57 used the same healthy ON_PREM route under EU-only policy, observed 503 before provider dispatch, restored the organization policy, and verified 200 recovery; the key was revoked. CP58 verified hostname-resolved loopback CIDR enforcement: mismatched CIDR caused zero fixture calls; exact loopback `/32` reached the synthetic MCP fixture. DEF-026 and QA-OBS-004 recurred. CP59 compared an A2A-disabled runtime against the enabled 18084 runtime: the former returned the source-configured 404 gate while allowing control-plane provisioning, and the latter completed a fresh input-required task after navigation and hard refresh on the same task ID. The A2A-only key was revoked; no new defect assigned. DEF-028 remains confirmed; DEF-017 and QA-OBS-006 last recurred in prior phases.
- **Blocked:** CP53 public webhook registration/delivery remains BLOCKED because no usable Operator credential is available for the isolated runtime. The external receiver token was deleted without receiving any data. Other test areas continue. CP43 found Routes & Policies guarded in organization-only workspace, while CP56 confirmed an owner can choose Organization edit scope from an active project/environment workspace; the direct org-only policy workflow expectation remains unresolved. Google adapter/runtime failures and RC4 artifact verification remain as recorded in prior checkpoints.
- **Remaining:** broader independent retest of DEF-001–030 on additional roles/platforms; full non-owner cross-project authorization; workspace transitions under latency and failure; organization-only policy workflow and other policy enforcement on live inference (CP57 covered one ON_PREM/EU path); multi-project MCP labeling/actions and first-discovery/drift counts; HTTP redirects beyond CP41/50, HTTPS redirects, redirect chains, DNS rebinding/mixed DNS and alternate runtime behavior (CP58 covers a single wildcard hostname resolution only); scope races outside FinOps and delayed/error refresh recovery; workspace persistence preference contract; effective budget display plus isolated spend/enforcement regression (CP51 verifies one organization ceiling enforcement path, while DEF-024 display remains open); TPM/concurrency/parent-policy limit enforcement and simultaneous ancestor/key cap precedence; model/plugin policy dimensions, guardrail confirmation and optimistic-lock recovery; remaining screens, business workflows, browser/platform modes, integrations and operational failures. See [coverage matrix](COVERAGE-MATRIX.md) for detailed PASS/FAIL/BLOCKED/UNVERIFIED entries.

## CP55 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP55 retest; retain existing scoped statuses and open DEF-005 residuals.
- **New findings:** none. DEF-016–028 remain open. No new defect ID assigned.
- **Coverage completed:** one environment-scoped RPM=1 Virtual Key accepted one call, rejected the next with canonical 429/Retry-After before provider dispatch, recovered after the interval, and was revoked. The issue form did not expose rate-cap fields; the UI provisioning contract remains unverified.
- **Blocked:** no CP55 blocker. CP53 webhooks still need usable Operator credentials.
- **Remaining:** TPM, concurrency, parent policy and distributed rate limits; UI configuration contract; continue policy and workspace-scope business-state exploration.

## CP58 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP58 full retest. DEF-005 remains partial; the single hostname-resolved CIDR case failed closed, while DNS rebinding/mixed answers, redirects, alternate runtimes, and mapped-address support remain open.
- **New findings:** none. DEF-016–028 remain open. DEF-026 recurred; QA-OBS-004 recurred.
- **Coverage completed:** `127.0.0.1.nip.io` resolution to loopback, mismatched-CIDR zero-egress denial, exact-CIDR positive control, persisted MCP/tool state, and visual review.
- **Blocked:** none in CP58. CP53 webhooks remain blocked on Operator access.
- **Remaining:** DNS rebinding/mixed answers, HTTPS/chained redirects, alternate runtimes, and other coverage gaps in the matrix.
## CP57 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP57 retest; current scoped dispositions are unchanged. DEF-005 remains partial with security-contract gaps open.
- **New findings:** none. DEF-016–028 remain open. QA-OBS-006 recurred because Data Plane request IDs and trace-list IDs differ.
- **Coverage completed:** real synthetic route positive control; EU-only org policy propagated through environment scope; 503 fail-closed rejection before provider dispatch with persisted error trace; policy restore and 200 recovery; temporary key revoked and state verified.
- **Blocked:** no CP57 blocker. CP53 webhook delivery still lacks a usable Operator credential.
- **Remaining:** other policy regions and dimensions, alternate targets/roles, concurrency, broader route failures, and all incomplete areas in the coverage matrix.
## CP56 consolidated defect and coverage update

- **Historical DEF-001–015:** no CP56 retest; current scoped statuses remain unchanged, with DEF-005 partial and its security-contract gaps open.
- **New findings:** none. DEF-016–028 remain open; no new defect assigned.
- **Coverage completed:** organization residency changed through the real UI, resolved effective policy narrowed at Organization/Project/Environment scopes, child editors exposed only the inherited region, and the original organization policy was restored and verified. No inference or provider call was sent.
- **Blocked:** no CP56 blocker. CP53 webhook delivery remains blocked on Operator access.
- **Remaining:** policy route enforcement, other policy dimensions, guardrail workflows, optimistic-lock handling, alternate roles, org-only policy workflow, and broader campaign as listed above.

## CP54 consolidated defect update

- **Historical DEF-001–015:** no CP54 retest; scoped outcomes and residual limits are unchanged. DEF-005 remains partial.
- **Open findings:** DEF-016–028 remain open. DEF-028 confirms the Route Explain/live route inconsistency; DEF-017 recurred. DEF-019 remains expanded by CP51.
- **Coverage completed:** Route Explain on the same healthy multi-target Ollama route, API dry-run dialect comparison, real Playground positive control, and temporary key revocation.
- **Coverage blocked:** CP53 webhook registration/delivery still needs usable Operator credentials.
- **Remaining:** other adapters/dialects and policy/capability explain cases remain unverified; broader regression continues.

## CP53 consolidated coverage update

- **Historical defects:** no defect retested in CP53; existing scoped statuses are preserved.
- **Findings:** no new defect. QA-OBS-003 recurred.
- **Coverage completed:** exact Org Owner Operations navigation boundary; attempted alternate Operator credentials; disposable public receiver provisioned and deleted without any event payload.
- **Coverage blocked:** webhook registration, delivery, signature validation and retry until an Operator test credential is available.
- **Coverage remaining:** all unaffected regression areas continue; webhook blocker does not suspend other QA.

## CP52 consolidated defect update

- **Historical DEF-001–015:** no CP52 retest; preserve scoped statuses and limits. DEF-005 remains partial.
- **Open findings:** DEF-016–028 remain open. DEF-017 recurred after successful failover; DEF-027 and DEF-028 are confirmed. DEF-019 retains the CP51 organization-budget expansion; DEF-024 remains a visibility failure.
- **Coverage completed:** real provider connection/probe, same-model second deployment, priority-failover route, synthetic 503→200 streaming response, persistent ordered trace and key revocation.
- **Coverage blocked:** none in CP52.
- **Coverage remaining:** multi-target all-fail/timeout/cancel/weights, route edit/reorder and provider-specific failures; see matrix.

## CP51 consolidated defect update

- **Historical DEF-001–015:** CP51 changed no historical retest disposition; preserve their scoped QA-002 results and residual limits as recorded above/register. DEF-005 remains partial.
- **Confirmed findings:** DEF-016–026 remain open. CP51 expands DEF-019 to organization-scope budget rejection and observes DEF-017 pending trace metadata again. DEF-024 remains a UI visibility failure despite the scoped backend enforcement pass. No new defect ID was added.
- **Coverage completed:** one full synthetic provider→model→healthy deployment→route→streaming Playground journey; organization cap positive/rejection/recovery; UI and DB verification; test-key revocation.
- **Coverage blocked:** none in CP51. Google credentials/provider adapter and RC4 artifact limitations remain as historical environment statuses.
- **Remaining:** simultaneous budget scope precedence, additional roles/browsers, provider-side 429 differentiation, and cross-screen recovery remain unverified; see matrix.

## Recommended engineering handoffs

1. DEF-017, DEF-018, DEF-019, DEF-020, DEF-023 and DEF-024: prepare cross-screen state and business visibility fixes with end-to-end persistence regressions.
2. DEF-027: disambiguate route target selection and fallback summaries by provider connection and deployment identity.
3. DEF-016 and DEF-021: agree the member enrollment/list contract and align UI success/error states with supported onboarding.
4. DEF-005: keep security boundaries fail-closed; engineering should define redirects and supported mapped-address notation before changing behavior. Do not weaken SSRF or authorization.
5. DEF-022: ensure live session role changes reconcile client state without exposing actions after server invalidation.
6. DEF-026: align sync result counts/copy with initial schema lock state while retaining the pin-before-use control.
7. DEF-028: make Route Explain use or expose the live route's actual request dialect semantics; preserve dry-run/no-egress and reject truly incompatible native requests clearly.

`GATE-REL-07` remains **BLOCKED**. No product implementation was changed, no release was approved, and QA-002 is not complete.

## CP59 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested in CP59; retain the existing scoped states. DEF-005 remains partial.
- **Open findings:** DEF-016–028 remain open; no new defect. QA-OBS-010 records the A2A control-plane/data-plane configuration contract question.
- **Coverage completed:** actual A2A discovery, A2A-only key scope, input-required lifecycle, persisted task identity across navigation/refresh, real UI resume to completion, fixture isolation metadata, and key revocation.
- **Blocked:** A2A use on the 18445 runtime because its A2A plane is disabled by configuration; config provenance not readable. CP53 webhook delivery still needs Operator access.
- **Remaining:** full DEF-023 all-screen scope sweep; unresolved DEF-005 DNS-rebinding/transport and alternate-runtime cases; independent retests and all remaining feature/role/platform areas. See CP59 and the matrix.

## CP60 consolidated defect and coverage update

- **Historical DEF-001–015:** no new retests; preserve scoped dispositions. DEF-005 remains PARTIAL.
- **New findings:** none confirmed. DEF-016–028 remain open. QA-OBS-004 recurred; QA-OBS-011 records unsupported/noncanonical host behavior without security bypass evidence.
- **Coverage completed:** confirmed DEF-023 source cause and cross-scope API/UI mismatch; renewed visual review of existing CP43 contrasts; explicit-CIDR fail-closed checks for `127.1` and `localhost.` against a metadata-only fixture.
- **Blocked:** fresh Team Admin access because CP45 intentionally suspended its credential; CP53 webhooks remain blocked on Operator credentials.
- **Remaining:** full scope-switch matrix on remaining screens/roles; DEF-005 rebinding, multiple answers, HTTPS chains and alternate runtimes; independent historical retests; other platform/integration coverage.

## CP61 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested; all prior scoped statuses remain. DEF-005 stays PARTIAL.
- **Open findings:** DEF-016–028 remain open; DEF-026 reproduced again on direct IPv6 loopback. No new defect ID. QA-OBS-004 recurred.
- **Coverage completed:** direct IPv6 literal with mismatching and exact CIDR, fixture zero-contact/positive controls, repeat sync, persistent schema lock, and screenshot inspection.
- **Blocked:** none specific to CP61; CP53 Operator credential gap remains.
- **Remaining:** DNS rebinding/mixed-answer races, redirect chains/HTTPS, alternate runtime, direct IPv6 grants/calls, workspace scope coverage, and all other incomplete feature/role/platform cases.

## CP62 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested in CP62; preserve scoped dispositions. DEF-005 remains partial.
- **New finding:** DEF-029 Low confirmed. Ten old environment keys appeared under No project / No env after a held HTTP 200 response; navigation cleared them. No secret or authorization bypass was observed.
- **Other open defects:** DEF-016–028 remain open; DEF-023 and DEF-029 are related to AccessPage scope but distinct. DEF-026 and QA-OBS-004 recurred in CP61.
- **Coverage completed:** forced response-order test for Access & Keys, exact project/env IDs in returned rows, organization-only before/after screenshots, and navigation recovery.
- **Blocked:** no CP62 blocker. Fresh Team Admin test remains blocked because prior synthetic credential was intentionally suspended; CP53 webhook integration remains blocked on Operator access.
- **Recommended handoff:** include DEF-029’s stale response guard with DEF-023’s scope-dependent member loading, but retain separate acceptance tests.
- **Remaining:** scope races on other pages and between environments/projects, limited-role exposure matrix, additional DEF-005 cases, independent historical retests, and all remaining feature/browser/platform coverage.

## CP63 consolidated defect and coverage update

- **Historical DEF-001–015:** not retested; retain their prior scoped dispositions. DEF-005 remains partial.
- **New finding:** DEF-030 Moderate confirmed. Three environment routes reappeared under No project / No env after a delayed HTTP 200; Edit/Delete remained visible and Save route was enabled. No mutation or auth bypass occurred.
- **Other new findings:** DEF-029 Low remains confirmed for stale Virtual Key metadata; DEF-023 remains a separate organization-members false-empty issue.
- **Coverage completed:** real-browser race on route listing, scope switch, old environment response, route edit/delete menu, enabled edit drawer and screenshot review.
- **Blocked:** none in CP63. Prior Team Admin credential and webhook Operator limitations remain.
- **Recommended handoff:** fix scope-bound async state across Access & Keys and Routes & Policies, preserving separate acceptance tests for membership visibility and route mutation controls.
- **Remaining:** cross-environment route races, page-by-page race sweep, role-limited actions, full historical retests, integrations and platform/browser coverage.
