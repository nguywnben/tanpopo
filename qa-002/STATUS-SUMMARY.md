# QA-002 consolidated status summary

**Frozen baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Latest checkpoint:** CP96
**Release gate:** `GATE-REL-07 BLOCKED`  
**QA state:** In progress; coverage remains incomplete. QA-001 results are not inherited.

## Historical defects DEF-001–015

| Status | IDs | Notes |
| --- | --- | --- |
| Verified fixed for scoped post-baseline scenarios | DEF-001–004, DEF-006, DEF-008–015 | Each passes only the specific QA-002 retest scenarios recorded in [DEFECT-RETEST-REGISTER.md](DEFECT-RETEST-REGISTER.md). This is not a full role/platform regression. |
| Partial pass; residual security/support coverage open | DEF-005 | Explicit private-IP/CIDR controls passed for executed cases; CP68 completed one enabled-runtime private A2A register/discover/task journey. CP66/67 confirm mobile invalid-CIDR errors hidden behind both drawers while APIs still reject safely. Redirects were not followed; DNS rebinding, mixed answers, alternate runtimes, and IPv4-mapped endpoint support remain unresolved. See QA-OBS-004/007 and CP25/40–42. |
| Successful audit paths verified; failure atomicity and erasure residuals open | DEF-007 | CP06/16/18/46/79/85 verified successful bootstrap/resource/create/shred events and chain integrity for named paths. Populated-organization erasure and injected audit/storage failures remain UNVERIFIED; do not treat the historical missing-event report as fully closed. |

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
| DEF-023 | Moderate | Organization member list is falsely empty without both project and environment selected; CP71 and CP80 independently reconfirmed the scoped false-empty state against persisted members. |
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
| DEF-034 | Moderate | Webhook subscription cannot be disabled or deleted through the Console or Admin API; active external destinations cannot be revoked through a supported product workflow. |
| DEF-035 | Moderate | Models shows sibling-project environment deployments with Disable/Delete actions while organization-only or an empty project is selected, without identifying deployment ownership. No unauthorized access or mutation was observed. |

All details, reproduction steps, expected behavior, evidence and acceptance criteria are in the linked [defect reports](reports/). DEF-029 is a distinct delayed-response issue from DEF-023’s false empty organization member list. DEF-030 confirms the same general scope-race class in Routes & Policies, where stale routes expose Edit/Delete and enabled Save. DEF-031 concerns invalid-CIDR form feedback in both MCP and A2A and does not weaken or bypass API enforcement. CP47 adds a scoped loopback positive control and confirms Team Admin cross-project sync denial before fixture contact; DEF-026 captures the inconsistent first-review notice.

## Latest phase: CP86

- **Historical defects verified fixed:** DEF-001–004, DEF-006, DEF-008–015 retain only their scoped QA-002 results; CP82 independently revalidated DEF-014/015 over four groupings. DEF-005 remains partial. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open. DEF-023 was reconfirmed in CP80. DEF-007 has a fresh successful empty-organization crypto-shred/audit-chain path, while its failure atomicity and populated-resource acceptance remain unverified. DEF-034 is newly confirmed; actual delivery remains environment-blocked rather than a product defect.
- **New findings:** DEF-034 Moderate confirmed: webhook subscriptions have no UI revoke action and authenticated DELETE returns 404. QA-OBS-015 records the direct-egress limitation. DEF-020 also recurred after clear; no new ID for that recurrence. Existing QA-OBS-001–014 remain preserved.
- **Coverage completed:** CP79 project provisioning audit and 315-event chain; CP80 DEF-023 comparison and keyboard tabs; CP81 Overview windows; CP82 CSV/JSON group exports; CP83/84 compatible and blocked-incompatible upgrade preflights; CP85 disposable empty-organization create/shred and intact deleted-org chain; CP86 webhook registration, one synthetic kill-switch event/outbox, switch clear, secret cleanup, and authenticated subscription-removal failure. Browser screenshots were visually inspected.
- **Coverage blocked:** CP86 direct webhook delivery/signature verification is BLOCKED by managed Cloud direct DNS/egress: direct no-proxy HTTPS failed during DNS, while the managed shell proxy returned HTTP 200; Tanpopo’s guarded transport intentionally bypasses ambient proxy. Runtime 18445 A2A remains disabled by configuration; RC4 artifact verification remains UNVERIFIED because the artifact is in a separate unavailable DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance retests remain incomplete overall; DEF-005 DNS rebinding/mixed answers/redirect-chain/alternate runtimes; DEF-007 failure atomicity, populated organization and storage-fault paths; DEF-034 revoke/disable behavior after engineering support; actual upgrade/ABORTED_SAFE and cross-role denial; webhook delivery/HMAC in direct-egress-capable infrastructure; provider outage/recovery for QA-OBS-003; FinOps filters/pagination/roles/locales; and remaining product, browser, platform, integration, accessibility, and operations coverage. PASS/FAIL/BLOCKED/UNVERIFIED remain distinct in the matrix.
- **Recommended handoffs:** prioritize DEF-034 subscription revocation; keep DEF-016/023/029/030 state behavior coupled with authorization acceptance; DEF-033 budget create/update semantics; DEF-005 SSRF acceptance without weakening controls; DEF-007 audit atomicity/populated erasure; and actual upgrade safe-abort behavior.
- **Checkpoint:** [CP86](checkpoints/CP86-webhook-delivery-and-subscription-lifecycle.md), [CP85](checkpoints/CP85-operations-org-crypto-shred.md), [CP84](checkpoints/CP84-operations-incompatible-preflight.md).

## Historical phase: CP84

- **Historical defects verified fixed:** DEF-001–004, DEF-006–015 retain only their individually scoped QA-002 results. CP82 independently retested DEF-014/015 across four supported grouping dimensions. DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–033 remain confirmed open as listed above; broad acceptance remains incomplete. DEF-023 was independently reconfirmed in CP80. DEF-007 failure injection and organization mutation/delete/storage-fault/cross-tenant paths remain UNVERIFIED.
- **New findings:** no new defect ID in CP79–84. Confirmed new defects remain DEF-016–033 with severity in their register/reports. QA-OBS-001–014 remain preserved, including unresolved QA-OBS-003.
- **Coverage completed:** CP79 provisioning audit success path/315-event chain; CP80 DEF-023 false empty/API comparison and keyboard tabs; CP81 Overview windows and alert contract; CP82 four FinOps export groupings in CSV/JSON; CP83 compatible upgrade preflight; CP84 incompatible upgrade blocking response with detailed extension/range/reason and SYSTEM audit attribution. Screenshots were visually inspected.
- **Coverage blocked:** runtime 18445 A2A remains configuration-disabled. Webhook delivery requires usable Operator access plus controlled receiver/signing setup. RC4 artifact verification remains UNVERIFIED because the separate DevBox artifact is unavailable in this workspace.
- **Coverage remaining:** independent DEF-001–015 acceptance retests remain incomplete overall; DEF-005 DNS rebinding/mixed answers/redirect-chain/alternate runtimes; DEF-007 failure/mutation paths; actual upgrade/ABORTED_SAFE execution and non-Operator denial; controlled provider outage/recovery for QA-OBS-003; FinOps date filters/pagination/roles/locales; other Console workflows, integrations, browsers, viewports, accessibility, and operations/platform scenarios. The matrix preserves PASS, FAIL, BLOCKED and UNVERIFIED separately.
- **Recommended handoffs:** keep workspace state defects (DEF-016/023/029/030) coupled to explicit authorization acceptance; prioritize DEF-033 FinOps create/update semantics, DEF-005 SSRF contract without weakening enforcement, DEF-007 audit atomicity, and a real upgrade safe-abort lifecycle.
- **Checkpoint:** [CP84](checkpoints/CP84-operations-incompatible-preflight.md), [CP83](checkpoints/CP83-operations-upgrade-preflight.md), [CP82](checkpoints/CP82-finops-export-grouping-regression.md).

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
- QA-OBS-015: managed Cloud shell proxy can reach public HTTPS, but direct no-proxy DNS/egress used by Tanpopo's SSRF-guarded webhook client failed; public delivery and HMAC verification are blocked pending a direct-egress-capable environment. This is not classified as a Tanpopo defect (CP86).

## Coverage

- **Completed in CP43–58:** real Chromium scope switching across organization-only, project/no-environment, project/environment and a second project; navigation/refresh; visually inspected Access, FinOps, Routes, Traces, Playground, MCP, A2A, Overview, Providers, Models, Extensions and Security states; API/persisted-state comparison for members and budgets; Models no-environment create attempt generated validation but no mutation; real Team Admin sessions confirmed project chooser/MCP filtering and 403 on different-project reads/sync. CP47 owner sync contacted only the allowlisted synthetic fixture, visually exposed the first-schema lock/status mismatch, and owner pin resolved it. CP48 delayed a project1 FinOps budget response during a real workspace switch; skeletons replaced old rows and the selected project rows appeared after settlement. CP49 confirmed refresh returns a manually selected project to the fresh-login/runtime default; affected rows changed with scope and remained persisted, leaving preference behavior as QA-OBS-009. CP50 exercised HTTP 302/303 MCP redirects; both failed closed before target contact with no tool persistence. CP51 exercised one synthetic child project/environment route under an organization budget: positive control settled 2 micro-USD; reduced org limit caused a generic Playground 429 with canonical trace reason and no upstream dispatch; limit restored and temporary Playground keys revoked. CP52 exercised a two-target priority-failover stream: first synthetic provider returned 503, backup returned 200 and the trace waterfall recorded both ordered deployment IDs; a temporary key was revoked. The route UI rendered three distinct deployments with identical labels (DEF-027). CP54 used Route Explain on the same route: UI returned zero eligible deployments and two dialect rejections, while an API dry-run with dialect `ollama` selected both; the same route then returned a real 200 Playground stream selecting the primary. CP55 exercised a real bearer-only RPM-limited Data Plane key: 200 then fail-fast 429/Retry-After without egress, then 200 after the interval, followed by Console revocation. CP56 used the real policy UI to set EU-only organization residency, verified it resolved at Organization/Project/Environment API scopes, visually confirmed child editors exposed only EU, and restored all four regions. CP57 used the same healthy ON_PREM route under EU-only policy, observed 503 before provider dispatch, restored the organization policy, and verified 200 recovery; the key was revoked. CP58 verified hostname-resolved loopback CIDR enforcement: mismatched CIDR caused zero fixture calls; exact loopback `/32` reached the synthetic MCP fixture. DEF-026 and QA-OBS-004 recurred. CP59 compared an A2A-disabled runtime against the enabled 18084 runtime: the former returned the source-configured 404 gate while allowing control-plane provisioning, and the latter completed a fresh input-required task after navigation and hard refresh on the same task ID. The A2A-only key was revoked; no new defect assigned. DEF-028 remains confirmed; DEF-017 and QA-OBS-006 last recurred in prior phases.
- **Blocked:** CP53 originally lacked a usable Operator credential, but CP86 used the working Operator + Org Owner session to register a webhook and queue one synthetic event. Live delivery/signature remained BLOCKED by direct Cloud egress/DNS (QA-OBS-015); no request reached the receiver. CP43 found Routes & Policies guarded in organization-only workspace, while CP56 confirmed an owner can choose Organization edit scope from an active project/environment workspace; the direct org-only policy workflow expectation remains unresolved. Google adapter/runtime failures and RC4 artifact verification remain as recorded in prior checkpoints.
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


## CP87 consolidated status update

- **Historical defects verified fixed:** DEF-001–004, DEF-006, DEF-008–015 retain only their prior scoped QA-002 results. CP87 did not retest them; independent acceptance coverage remains incomplete overall. DEF-005 remains partial. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open, including CP86 DEF-034 and the DEF-020 recurrence. No defect status changed in CP87. All prior defect evidence remains preserved.
- **New findings:** No new defect. QA-OBS-016 records unclear organization-wide Chargeback scope labeling when the workspace selector shows a project. QA-OBS-015 Cloud direct-egress blocker remains.
- **Coverage completed:** CP87 real Chromium owner Chargeback view; visual comparison of selected project and organization-wide project grouping; API/UI amount and row agreement; valid recent, malformed and reversed API window handling; selected-project query comparison. Screenshots were visually inspected; no mutation.
- **Coverage blocked:** direct webhook delivery/HMAC remains blocked by managed Cloud direct DNS/egress; runtime 18445 A2A remains disabled by configuration; RC4 artifact verification remains UNVERIFIED because the artifact is in a separate unavailable DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage remains incomplete; DEF-005 DNS rebinding/mixed answers/redirect chains/alternate runtimes; DEF-007 failure atomicity and populated-org erasure; Chargeback roles/pagination/locales/date-filter UI/project-scope contract; all open DEF-016–034 remediation and regression; actual upgrade safe-abort; provider outage/recovery; remaining product, browser, platform, accessibility, integration and operations coverage.
- **Recommended handoffs:** prioritize DEF-034 subscription revocation; DEF-016/023/029/030 scope/state consistency; DEF-033 budget create/update semantics; DEF-005 SSRF acceptance without weakening controls; DEF-007 audit atomicity/populated erasure. Confirm the Chargeback scope-label contract (QA-OBS-016).
- **Checkpoint:** [CP87](checkpoints/CP87-finops-chargeback-scope-and-date-windows.md); prior [CP86](checkpoints/CP86-webhook-delivery-and-subscription-lifecycle.md).


## CP88 consolidated status update

- **Historical DEF-001–015:** no new retests in CP88. Prior QA-002 scoped statuses remain, DEF-005 remains partial, and full independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open; no status changed. Preserve CP86 DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. Team Admin's Security Audit Log request returned 403 and displayed an explicit role-denial message; source RBAC omits audit-read permission for Team Admin while allowing scoped kill-switch/approval actions. Treated as intentional authorization, not a bypass or product defect.
- **Coverage completed:** CP88 real Chromium Team Admin session, Security route, audit endpoint denial, visible UI state, and no returned protected rows. No mutation.
- **Coverage blocked:** direct webhook delivery/HMAC by managed Cloud egress; A2A on runtime 18445 by its disabled configuration; RC4 artifact verification UNVERIFIED in the unavailable separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage remains incomplete; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; open DEF-016–034 remediation and regression; remaining Console roles/scopes, business workflows, cross-feature state, browsers/platforms, integrations, accessibility and operations.
- **Recommended handoffs:** DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF controls, and DEF-007. No handoff added from CP88.
- **Checkpoint:** [CP88](checkpoints/CP88-team-admin-security-audit-boundary.md); prior [CP87](checkpoints/CP87-finops-chargeback-scope-and-date-windows.md).


## CP89 consolidated status update

- **Historical DEF-001–015:** no new retests in CP89; scoped prior QA-002 dispositions remain, DEF-005 remains partial, independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status changed. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no new defect. Existing trace API/UI inspection showed metadata only for two rows, both with `has_debug_payload=false`; full privacy/export acceptance remains unverified.
- **Coverage completed:** real Chromium authenticated trace list/detail, API comparison and visual inspection; read-only YAML/listener check. No mutation.
- **Coverage blocked:** no configured OTLP collector endpoint/access was established and no local 4317/4318 listener or YAML setting was found; process environment read was denied, so collector outage/drop counters were not tested. Direct webhook delivery/HMAC remains managed-egress blocked; A2A on 18445 is config-disabled; RC4 artifact remains unverified in the separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage; full TP-AC-F0804 marker/credential search and exporter outage/recovery; trace roles/search and other waterfall cases; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; all open DEF-016–034 remediation/regression; remaining product/browser/platform/integration/accessibility/operations coverage.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF controls, DEF-007. No new defect handoff.
- **Checkpoint:** [CP89](checkpoints/CP89-trace-explorer-metadata-privacy.md); prior [CP88](checkpoints/CP88-team-admin-security-audit-boundary.md).


## CP90 consolidated status update

- **Historical DEF-001–015:** CP90 did not retest them. Preserve prior scoped QA-002 results; DEF-005 remains partial and independent acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status changed. Preserve CP86 DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. A narrow real owner Vault list/API read exposed only metadata for one existing QA credential; no plaintext observed.
- **Coverage completed:** real Chromium Providers → Vault Secrets and authenticated API field comparison. Existing QA credential left untouched; no mutation.
- **Coverage blocked:** full secret-privacy scan requires credential-bearing inference plus persistent log/API search; OTel fail-open lacks confirmed collector endpoint/access; direct webhook delivery/HMAC is managed-egress blocked; runtime 18445 A2A is disabled by configuration; RC4 artifact verification remains unverified in separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance; full DEF-005 SSRF coverage; DEF-007 populated erasure/failure atomicity; secret binding/rotation/revocation/OAuth and role coverage; all open DEF-016–034 regression/remediation; all remaining features, browsers/platforms/integrations/accessibility/operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Checkpoint:** [CP90](checkpoints/CP90-vault-secret-metadata.md); prior [CP89](checkpoints/CP89-trace-explorer-metadata-privacy.md).


## CP91 consolidated status update

- **Historical DEF-001–015:** no new retests in CP91. Preserve scoped prior QA-002 results; DEF-005 remains partial; independent acceptance is incomplete; no QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain confirmed open; no status changed. Preserve DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. QA-OBS-016 expanded: owner org-wide chargeback, Overview and traces display organization-level rows while a project remains selected, with no explicit report-scope label. Values agree and this is consistent with Owner organization authorization.
- **Coverage completed:** mapped two trace rows to a sibling project environment and reconciled them to the 24-hour Overview total under a selected empty project; real UI screenshots inspected.
- **Coverage blocked:** direct webhook delivery/HMAC by managed Cloud egress; runtime 18445 A2A by configuration; RC4 artifact unverified in separate DevBox; OTel collector outage acceptance awaits known endpoint/access.
- **Coverage remaining:** independent DEF-001–015 acceptance; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; trace/overview scope for non-owner roles and scope contract; open DEF-016–034 regression/remediation; remaining features, browsers/platforms, integrations, accessibility and operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. Clarify global-report scope labeling.
- **Checkpoint:** [CP91](checkpoints/CP91-trace-and-overview-scope-under-project-selection.md); prior [CP90](checkpoints/CP90-vault-secret-metadata.md).


## CP92 consolidated status update

- **Historical defects verified fixed:** DEF-001 remains VERIFIED FIXED for scoped scenarios; CP92 independently completed a fresh four-step setup on RC4 and verified accessible names include each visible step label. DEF-002–004 and DEF-006–015 retain their previous scoped passes only.
- **Historical defects failing / unverified:** DEF-005 remains partial; DEF-016–034 remain open. DEF-002–015 broader roles/platforms/edge cases are not complete; no QA-001 PASS is inherited. Preserve DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. A no-adapter fresh install completed when optional connection setup was skipped.
- **Coverage completed:** separate runtime began `initialized=false`; real Setup UI completed owner/org/default-project bootstrap and custody acknowledgement; new session login and persisted one-owner/one-project evidence verified. Screenshots inspected; temporary key/token/password/database removed.
- **Coverage blocked:** CP92 itself had no blocker. Direct webhook/HMAC still managed-egress blocked; runtime 18445 A2A disabled by configuration; RC4 artifact verification unverified in separate DevBox; full OTLP collector acceptance awaits endpoint/access.
- **Coverage remaining:** independent DEF-002–015 residual coverage, DEF-005 SSRF cases, DEF-007 failure atomicity/populated erasure, fresh-instance adapter activation/first inference, open DEF-016–034 regression/remediation, remaining browsers/platforms/roles/integrations/accessibility/operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Checkpoint:** [CP92](checkpoints/CP92-fresh-install-and-DEF-001-retest.md); prior [CP91](checkpoints/CP91-trace-and-overview-scope-under-project-selection.md).


## CP93 consolidated status update

- **Historical DEF-001–015:** no defect retest in CP93; CP92 retested DEF-001. Other scoped results remain; DEF-005 is partial and independent acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open; no status change. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no product defect. QA-OBS-017 records the distinction between current RC4 `upstream_unreachable` (Cloud direct-egress mismatch supported) and historical listener-specific `probe_runtime_unavailable` (exact cause unresolved). DEF-032 duplicate Google rows remain open.
- **Coverage completed:** real RC4 Google connection probe on 18084; connection stayed Unverified; 18086 inventory showed no Google connection; historical 3-listener evidence reconciled without inheriting PASS.
- **Coverage blocked:** a current same-config three-runtime Google comparison lacks configured credential/connection on 18086 and was not established on 18083/18085; actual app-mediated Google inference still awaits guarded direct egress. Direct webhook/HMAC, 18445 A2A configuration, RC4 artifact DevBox and OTLP collector remain blocked/unverified.
- **Coverage remaining:** independent DEF-002–015 residual cases; same-config Google runtime comparison; DEF-005 SSRF cases; DEF-007 failure atomicity/populated erasure; all open DEF-016–034 regressions/remediation; other products, roles, browsers/platforms, integrations, accessibility and operations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, DEF-007. No engineering handoff for environment-classified Google transport failures.
- **Checkpoint:** [CP93](checkpoints/CP93-google-provider-probe-runtime-comparison.md); prior [CP92](checkpoints/CP92-fresh-install-and-DEF-001-retest.md).


## CP94 consolidated status update

- **Historical defects verified fixed:** no historical defect retested in CP94; preserve the last independently observed scoped outcomes. DEF-001 remains scoped PASS from CP92 only. No QA-001 PASS is inherited.
- **Historical defects failing / unverified:** DEF-016–034 remain open, including DEF-023 and visually apparent duplicate provider rows DEF-032; DEF-005 remains partial. DEF-002–015 independent acceptance coverage remains incomplete.
- **New findings:** no new defect. CP94 saw the existing duplicate Google connection cards but did not perform destructive actions or independently verify backend identity ambiguity.
- **Coverage completed:** real Chromium at 375 × 844; Vietnamese and dark-theme selection; Overview, Providers and FinOps interaction; persistence after reload; width checks; direct visual inspection of current screenshots. Scoped PASS only.
- **Coverage blocked:** none specific to CP94. Previously recorded egress, A2A runtime, OTel collector and RC4 artifact limitations remain unchanged.
- **Coverage remaining:** other locales/themes/viewports/browsers and accessible mobile interaction; independent DEF-001–015 acceptance; DEF-005 SSRF acceptance; DEF-007 populated erasure/failure atomicity; all open DEF-016–034 regression; remaining Console, API, CLI, integration, security and operational scenarios.
- **Recommended handoffs:** continue existing priorities DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. No new handoff from CP94.
- **Checkpoint:** [CP94](checkpoints/CP94-vietnamese-dark-mobile-console-regression.md); prior [CP93](checkpoints/CP93-google-provider-probe-runtime-comparison.md).


## CP95 consolidated status update

- **Historical DEF-001–015:** CP95 did not retest these. Preserve prior QA-002 scoped outcomes only: DEF-001–004, DEF-006, and DEF-008–015 have limited passing paths; DEF-005 remains partial; DEF-007’s successful-path audit evidence does not cover populated erasure/failure atomicity. Full independent acceptance remains incomplete. No QA-001 PASS is inherited.
- **Historical defects verified fixed / failing / unverified:** no prior defect disposition changed. DEF-016–034 remain open, including DEF-023/024/029/030/034. DEF-035 is newly confirmed. All historic evidence and checkpoint uncertainty remain intact.
- **New finding:** DEF-035 Moderate confirmed. Org-only and empty-project Models views show three deployments owned by another project’s `stage` environment, without scope identity, while exposing Disable/Delete. No mutation or authorization bypass was demonstrated.
- **Coverage completed:** real Chromium scope selection and SPA navigation across org-only, project-only, and environment contexts; Extensions installed/catalog/lockfile, Models, and Routes & Policies; deployment owner/project mapping via authenticated API; screenshots visually inspected.
- **Coverage blocked:** no CP95-specific blocker. Cloud direct-egress/webhook delivery, A2A runtime 18445 configuration, OTel collector access, and separate DevBox RC4 artifact verification limitations persist.
- **Coverage remaining:** all incomplete feature, role, platform and integration work; complete independent DEF-001–015 retests; DEF-005 SSRF acceptance without weakening boundaries; DEF-007 fault atomicity/populated erasure; DEF-016–035 remediation regression; page-by-page scope and permission checks; Models action lifecycle and recovery; other browsers/platforms.
- **Recommended handoffs:** include DEF-035 with scope/state findings DEF-023/024/029/030. Keep DEF-034, DEF-033, DEF-005, and DEF-007 priorities.
- **Checkpoint:** [CP95](checkpoints/CP95-console-workspace-scope-and-model-deployment-identity.md); prior [CP94](checkpoints/CP94-vietnamese-dark-mobile-console-regression.md). `GATE-REL-07` remains **BLOCKED**.


## CP96 consolidated status update

- **Historical DEF-001–015:** CP96 did not retest them. Preserve limited scoped QA-002 outcomes: DEF-001–004, DEF-006, DEF-008–015 have passing paths; DEF-005 remains partial; DEF-007 successful audit paths do not close fault atomicity/populated erasure. No QA-001 PASS is inherited; full independent acceptance remains incomplete.
- **Historical open DEF-016–035:** remain open; CP96 did not change any defect disposition. DEF-035 is still the newest confirmed defect.
- **New defects:** none. The native attempt was environment-blocked/inconclusive, not assigned to product.
- **Coverage completed:** real UI issue and revoke of two PROVIDER keys restricted to `/native/*`; authenticated API confirmed both REVOKED; exposed endpoint attempt returned Console method fallback 405 and did not establish dispatch. Secrets were removed and excluded from evidence.
- **Coverage blocked:** FT-02 native passthrough positive exchange/fidelity needs the correct enabled QA Data Plane native-ingress address/configuration. No product defect is inferred from a Console fallback.
- **Coverage remaining:** complete independent DEF-001–015 acceptance; DEF-005 residual SSRF checks; DEF-007 fault/erasure cases; open DEF-016–035 regressions; all other feature, browser/platform, role, integration and operational coverage.
- **Recommended handoffs:** no new engineering handoff; continue DEF-035 with DEF-023/024/029/030 and existing DEF-034/033/005/007 priorities.
- **Checkpoint:** [CP96](checkpoints/CP96-native-passthrough-ingress-discovery.md); prior [CP95](checkpoints/CP95-console-workspace-scope-and-model-deployment-identity.md). `GATE-REL-07` remains **BLOCKED**.
