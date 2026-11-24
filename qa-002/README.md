# QA-002 — RC4 independent Alpha/Beta regression

**Workspace:** branch `qa/qa-002-regression`, based on `40bba5b55c2da6fc00b107b315c9b3167fb20169` (2026-11-24).
**Scope:** independent regression; do not modify Tanpopo implementation.
**Release gate:** `GATE-REL-07` remains **BLOCKED**.
**Latest checkpoint:** CP96 (Native passthrough ingress discovery).

This workspace is separate from QA-001. Historical evidence is reference material only; no QA-001 PASS is inherited. QA-001 remains the immutable record at branch `qa/qa-001-handoff-cp65`, commit `d994f82128f0e78a4e7bb7dd4ecd705cee60d85f` in the sibling checkout `/workspace/tanpopo`.

## Current records

- [CP96 — Native passthrough ingress discovery](checkpoints/CP96-native-passthrough-ingress-discovery.md)

- [CP95 — Console workspace scope and model deployment identity](checkpoints/CP95-console-workspace-scope-and-model-deployment-identity.md)
- [DEF-035 — Models deployment workspace-scope mismatch](reports/DEF-035-model-deployments-ignore-active-workspace-scope.md)

- [CP94 — Vietnamese dark-theme mobile Console regression](checkpoints/CP94-vietnamese-dark-mobile-console-regression.md)

- [Consolidated status summary](STATUS-SUMMARY.md)

- [CP93 — Google provider probe and runtime evidence reconciliation](checkpoints/CP93-google-provider-probe-runtime-comparison.md)
- [CP92 — Fresh-install Setup and DEF-001 retest](checkpoints/CP92-fresh-install-and-DEF-001-retest.md)
- [CP91 — Trace and Overview scope under project selection](checkpoints/CP91-trace-and-overview-scope-under-project-selection.md)
- [CP90 — Vault secret metadata and plaintext exposure check](checkpoints/CP90-vault-secret-metadata.md)
- [CP89 — Trace explorer metadata-only inspection](checkpoints/CP89-trace-explorer-metadata-privacy.md)
- [CP88 — Team Admin Security audit authorization boundary](checkpoints/CP88-team-admin-security-audit-boundary.md)
- [CP87 — FinOps chargeback scope and date windows](checkpoints/CP87-finops-chargeback-scope-and-date-windows.md)
- [CP86 — Webhook delivery and subscription lifecycle](checkpoints/CP86-webhook-delivery-and-subscription-lifecycle.md)
- [CP85 — Operations organization crypto-shred](checkpoints/CP85-operations-org-crypto-shred.md)

- [Baseline and environment CP00](checkpoints/CP00-baseline.md)
- [CP55 — Virtual Key RPM enforcement and recovery](checkpoints/CP55-vkey-rpm-enforcement-recovery.md)
- [CP56 — Hierarchical policy scope and residency inheritance](checkpoints/CP56-policy-scope-inheritance.md)
- [CP57 — Policy-controlled live inference and recovery](checkpoints/CP57-policy-inference-enforcement.md)
- [CP58 — Hostname-resolved private IP CIDR enforcement](checkpoints/CP58-hostname-resolved-private-cidr.md)
- [CP71 — Members & Roles workspace scope and role boundary](checkpoints/CP71-members-role-and-workspace-scope-retest.md)
- [CP72 — Team Admin member create/list refresh](checkpoints/CP72-team-admin-member-create-refresh-retest.md)
- [CP73 — Failed provider promotion guard](checkpoints/CP73-provider-auth-failure-promotion-guard.md)
- [CP74 — WebKit mobile webhook drawer](checkpoints/CP74-webkit-webhook-mobile-drawer-retest.md)
- [CP77 — Provider list workspace-scope comparison](checkpoints/CP77-provider-list-workspace-scope-comparison.md)
- [CP84 — Operations incompatible upgrade preflight](checkpoints/CP84-operations-incompatible-preflight.md)
- [CP83 — Operations upgrade compatibility preflight](checkpoints/CP83-operations-upgrade-preflight.md)
- [CP82 — FinOps CSV/JSON export grouping regression](checkpoints/CP82-finops-export-grouping-regression.md)
- [CP84 — Operations incompatible upgrade preflight](checkpoints/CP84-operations-incompatible-preflight.md)
- [CP83 — Operations upgrade compatibility preflight](checkpoints/CP83-operations-upgrade-preflight.md)
- [CP82 — FinOps CSV/JSON export grouping regression](checkpoints/CP82-finops-export-grouping-regression.md)
- [CP81 — Overview alert contract and time windows](checkpoints/CP81-overview-alert-contract-and-time-windows.md)
- [CP80 — Members scope consistency and keyboard tab interaction](checkpoints/CP80-members-scope-and-keyboard-accessibility.md)
- [CP79 — Provisioning audit event and chain verification](checkpoints/CP79-provisioning-audit-event-and-chain.md)
- [CP78 — FinOps budget creation and existing-scope retest](checkpoints/CP78-finops-budget-create-persistence-and-existing-scope-retest.md)
- [CP76 — Provider duplicate connection ambiguity](checkpoints/CP76-provider-duplicate-connection-management-ambiguity.md)
- [CP75 — FinOps CSV/JSON exports](checkpoints/CP75-finops-model-export-retest.md)
- [Coverage matrix](COVERAGE-MATRIX.md)
- [Historical defect retest register](DEFECT-RETEST-REGISTER.md)
- `evidence/` — sanitized command outcomes, API/state snapshots, and visually reviewed screenshots.
- [QA-OBS-004](reports/QA-OBS-004-mcp-allowlist-failure-diagnostic.md) — unresolved error diagnostic/state semantics observation, not a confirmed defect.
- [QA-OBS-005](reports/QA-OBS-005-budget-backfill-semantics.md) — unresolved activation-window contract for budgets created after spend.
- [QA-OBS-007](reports/QA-OBS-007-ipv4-mapped-mcp-endpoint-contract.md) — IPv4-mapped IPv6 loopback is explicitly rejected despite a matching CIDR; supported endpoint contract remains unverified.
- [DEF-018](reports/DEF-018-finops-mislabels-playground-lifetime-budget.md) — confirmed lifetime budget period is surfaced as monthly.
- [DEF-019](reports/DEF-019-playground-hides-budget-rejection-reason.md) — confirmed Playground cap rejection loses its canonical reason.
- [DEF-020](reports/DEF-020-killswitch-badge-stays-active-after-clear.md) — confirmed Security shell badges remain stale after switch clear and approval decisions.
- [DEF-023](reports/DEF-023-org-members-hidden-until-project-selected.md) — confirmed org-only and project/no-environment owner views hide the organization member list.
- [DEF-024](reports/DEF-024-finops-hides-effective-organization-budget-in-project-scope.md) — confirmed project FinOps view hides a persisted, effective organization ceiling.
- [DEF-032](reports/DEF-032-provider-connections-indistinguishable-when-duplicate.md) — distinct provider records can be indistinguishable in the UI when duplicate display values exist.
- [QA-OBS-013](reports/QA-OBS-013-provider-connection-scope-labels.md) — Provider Connections organization-wide listing has unresolved owner-project labeling.
- [DEF-033](reports/DEF-033-finops-create-budget-reuses-stale-revision.md) — Create budget can update a matching ceiling once, then fails with a stale revision.
- [DEF-034](reports/DEF-034-webhook-subscription-cannot-be-revoked.md) — registered webhook subscriptions have no product UI/API revoke path.
- [QA-OBS-014](reports/QA-OBS-014-finops-empty-state-session-discrepancy.md) — conflicting pre-write FinOps empty/one-row captures lack an API baseline.
- [QA-OBS-015](reports/QA-OBS-015-cloud-egress-blocks-guarded-webhook-delivery.md) — direct-egress limitation blocks live delivery from the managed Cloud runtime; no product delivery defect inferred.
- [DEF-025](reports/DEF-025-model-deployment-drawer-unavailable-without-environment.md) — confirmed low-severity Models form dead end without an active environment.
- [DEF-026](reports/DEF-026-mcp-initial-schema-approval-count-message.md) — confirmed MCP sync success copy omits first-review locks while discovered schema remains locked.
- [QA-OBS-009](reports/QA-OBS-009-workspace-default-after-refresh.md) — workspace selection resets to the runtime-selected default after refresh; persistence contract unresolved.
- [QA-OBS-008](reports/QA-OBS-008-mcp-hub-organization-wide-list-scope-label.md) — MCP Hub organization-wide list has no project labels; intended UI scope is unresolved.
- `reports/` — phase reports, new defect findings (DEF-016 onward), and explicitly unclassified QA observations.

Each checkpoint records the tested revision/runtime, actions, evidence, and PASS / FAIL / BLOCKED / UNVERIFIED result. A PASS is limited to the exact scenario evidenced. No test or artifact may be called PASS based only on source inspection, HTTP success, or an automated test.

## Evidence hygiene

Use disposable local databases and synthetic provider/integration fixtures. The configured provider keys may only be passed to the intended provider request from the runtime environment; do not print, persist, or include them in screenshots or reports. Sanitize response headers and logs. Keep databases, credentials, key material, and raw runtime logs outside version control. Store reproducible summaries and screenshots here.

## Checkpoint status

| Checkpoint | Scope | Result |
| --- | --- | --- |
| CP00 | Exact baseline, artifact inventory, environment/tooling diagnosis | PARTIAL PASS; no published RC4 build/release artifact found; see checkpoint |
| CP01 | Fresh-install Setup and first authenticated session | PASS for the executed setup/login path; broader persistence, roles, and features remain open |
| CP02 | Release linker provenance and full Go race suite | Linker settings verified; Hydrogen artifact UNVERIFIED; race suite PASS (86 packages) |
| CP03 | Complete fresh setup on A2A-enabled runtime; live axe and visual inspection | PASS for scoped Chromium wizard path; DEF-001 verified fixed in tested path; broad coverage continues |
| CP04 | Built CLI config show/help/reconcile using sanitized local HTTP fixture | PASS for command routing and fail-closed validation; real Tanpopo Admin API interaction remains UNVERIFIED |
| CP05 | First FinOps project budget from empty state; new-session persistence check | PASS for tested workflow; other scopes and budget policy remain open |
| CP06 | Bootstrap and standalone provisioning audit events, duplicate rejection, chain verification | PASS for tested creation paths; mutation/error paths remain open |
| CP07 | Real WebKit 26.5 webhook drawer at 375×844 using local HTTPS | PASS for scoped layout; registration requires a synthetic HMAC secret and remains untested |
| CP08 | Authorized A2A discovery, input-required task API/Console lifecycles, provider-only denial, key revocation | PASS for scoped lifecycle and key scenarios; MCP path, concurrent drain, budgets, and other roles remain open |
| CP09 | MCP private endpoint CIDR form, wildcard rejection and persistence | PASS for registration form and saved CIDR; actual MCP sync/network gating remain UNVERIFIED |
| CP10 | Ollama extension permission review, provider defaults, persistence and synthetic live probe | PASS for scoped Ollama lifecycle; other adapters and model/deployment inference remain open |
| CP11 | Exact no-budget provider settlement, FinOps headline, selected model grouping, CSV/JSON exports | PASS for scoped reconciliation; quota policies and other group dimensions remain open |
| CP12 | Local 401 provider probe, deployment promotion guard, route health and fail-closed inference | PASS for scoped rejection/readiness path; Google and other failure modes remain open |
| CP13 | Console encrypted backup, CLI verification, secret-decryption restore, restored startup/login | PASS for SQLite path; cluster and damaged-archive cases remain open |
| CP14 | Project-scoped Team Admin member creation, list refresh and role-boundary retest | CREATE/BOUNDARY PASS; UI list refresh FAIL (DEF-016), separately recorded |
| CP15 | Authorized A2A task paused at synthetic endpoint during Console graceful key revocation | Revocation PASS; dispatched task returned 400 `context canceled` and persisted Failed. A2A task drain contract UNVERIFIED; not classified as defect. |
| CP16 | Project/environment create, project teardown, audit attribution and chain verification | PASS for successful synthetic lifecycle and audit chain; standalone org/fault paths remain open. |
| CP17 | Fresh isolated bootstrap, separate-session persistence, Google provider boundary and empty-model Playground recovery | Fresh setup/login PASS for scoped path; Google model validation BLOCKED in-app by proxy-only egress; no-provider Playground correctly surfaces HTTP 503. Wider setup/platform and provider coverage remain open. |
| CP18 | Standalone organization provisioning, duplicate rejection and tenant audit isolation | PASS for new-organization audit attribution and one-event chain verification; duplicate slug rejected with no second organization. Fault injection, delete and cross-tenant access remain open. |
| CP19 | DEF-001 exact original Setup test retest on the new source baseline | Both original targeted Vitest cases PASS on 40bba5b; QA-001 failures on the pre-engineering baseline remain historical evidence, while CP03 provides separate live-browser evidence. Broader accessibility remains open. |
| CP20 | Firefox browser provisioning and launch diagnosis | UNVERIFIED: Firefox 153 installed in temporary `/tmp` cache, but Playwright launch timed out before app navigation or screenshots due read-only sandbox UID mapping and software compositor failures. Chromium/WebKit remain usable for their already-scoped cases. |
| CP21 | Fresh SQLite runtime clean restart, readiness and persisted Console state | PASS for startup probes, separate-session login, workspace persistence and retained failure trace; clustering and crash/fault recovery remain open. |
| CP22 | Fresh-runtime provider install → connection → deployment → route → streaming Playground inference and trace correlation | PASS for scoped synthetic provider setup/inference and exact-usage persistence; FAIL DEF-017 because Playground's trace card remains pending on repeated 404s despite a persisted trace. Google and real-provider behavior remain BLOCKED/UNVERIFIED; further product and platform coverage continues. |
| CP23 | Rerun interrupted full Go race suite using verified Go 1.26.11 | PASS: 86 test-bearing packages, 22 packages without tests, zero failures/race reports; source diff from baseline empty. Automated result does not replace product coverage. |
| CP24 | Streamable HTTP MCP governance and data-plane lifecycle | PASS for the synthetic read-only flow: registration/discovery, exact-tool key grant, JSON-RPC call, ungranted and cross-plane denial, drift lock/reapproval, and revoked-key 401. Broader MCP risk/role/transport coverage remains open; see checkpoint. |
| CP25 | MCP explicit CIDR negative test with reachable positive control | PASS for one literal loopback allowlist mismatch: sync failed and instrumented fixture received no request; same endpoint with matching /32 synced successfully. SSRF/address-resolution permutations remain unverified; generic retryable error and Active-state semantics tracked as unresolved QA-OBS-004. |
| CP26 | FinOps period fidelity and project-budget inference path | FAIL DEF-018 and DEF-019; DEF-017 independently recurred. Playground lifetime budgets are mislabeled monthly; generic 429 hides project budget reason. Project cap serial exact-boundary rejection and cap-increase recovery passed with persisted holds/settlements; retroactive prior-usage semantics are QA-OBS-005 and unresolved. See [checkpoint](checkpoints/CP26-finops-budget-cross-feature-regression.md). |
| CP27 | Vault/webhook boundary and emergency kill-switch lifecycle | FAIL DEF-020: same-session shell/navigation active badges remain 1 after clear while page/API show zero; reload reconciles. Scoped freeze→503→clear→200 route recovery passed after increasing the exhausted test budget; audit hash chain intact. Vault secret create/delete and private-IP SSRF rejection passed. Actual public webhook delivery BLOCKED pending a controlled receiver. DEF-017 recurred. See [checkpoint](checkpoints/CP27-webhook-and-killswitch-journeys.md). |
| CP28 | Destructive MCP HITL, independent reviewer and exact-once replay | PASS for pending, self-approval denial, separate reviewer approve/reject, argument mismatch, single-use exact replay, post-rejection no-dispatch, persistence and audit chain. DEF-020 stale Security nav badge recurred after decision until reload. Both MCP test keys were revoked; plaintext removed. Invitation/credential provisioning, expiry/concurrency and other clients remain open. See [checkpoint](checkpoints/CP28-mcp-destructive-approval-lifecycle.md). |
| CP29 | Security Reviewer read access and denied key/budget mutations | PASS for scoped Security/key/FinOps reads and 403 default-deny writes; persistent key/budget state remained unchanged. UI hid issuance and budget-create actions and rendered limits readonly. Normal invite/password onboarding, other roles, cross-tenant boundaries and other permission actions remain open. See [checkpoint](checkpoints/CP29-security-reviewer-rbac-and-read-only-finops.md). |
| CP30 | Local HUMAN member creation through first login | FAIL DEF-021: member was created/persisted and reported added, but fresh-context local login returned 401; no invite, first-password, reset or signup action appeared. Contract boundary recorded; other roles and identity modes remain open. See [checkpoint](checkpoints/CP30-local-member-first-login.md). |
| CP31 | Live session after owner role downgrade | FAIL DEF-022: a reviewer’s persisted role changed to Developer and its session returned 401, but the open browser retained stale Security controls/empty queue until reload. No authorization bypass observed. See [checkpoint](checkpoints/CP31-live-role-downgrade.md). |
| CP32 | Developer role and Playground integration | Developer read boundary and restricted Security/member views behaved as scoped; one synthetic stream returned 200 and persisted. DEF-017 independently recurred: five `req_…` lookups returned 404 while persisted `trc_…` was 200; the Playground card stayed pending. See [checkpoint](checkpoints/CP32-developer-playground-and-boundaries.md). |
| CP33 | WebKit mobile Console at 375×844 | PASS for scoped responsive navigation and five screens; actual routes, persisted rows and scoped API reads loaded without horizontal document overflow. Screenshots visually inspected. QA-OBS-003 recurred at 13 requests/53.85% errors with “All systems nominal.” Other widths, routes, touch-tab behavior and states remain UNVERIFIED. See [checkpoint](checkpoints/CP33-webkit-mobile-console-regression.md). |
| CP34 | WebKit narrow Console at 320×720 | PASS for scoped Overview, drawer, Security queue/audit and Operations health. KPI cards stacked legibly; Security Audit Log selection and 100 persisted rows loaded; Operations readiness was Ready; no document overflow. Tab strip discoverability by touch remains UNVERIFIED. QA-OBS-003 recurred. See [checkpoint](checkpoints/CP34-webkit-320px-narrow-console.md). |
| CP35 | Provider key → synthetic inference → graceful revocation | PASS for one owner-issued PROVIDER key: real streamed inference returned 200, exact usage persisted, Console revoke returned 200 and persisted REVOKED/version 2, and replay returned 401. Trace/settlement IDs differ; QA-OBS-006 retains the unresolved correlation contract. Rotation, concurrent drain and other roles remain open. See [checkpoint](checkpoints/CP35-provider-key-inference-revocation.md). |
| CP36 | Virtual Key rotation grace and revocation | PASS for one key: old secret worked before rotation; old and replacement secrets both worked in `ROTATING_DUAL_VALID`; graceful revoke persisted REVOKED/version 3 and both credentials then received 401. Three exact settlements persisted. Actual 24-hour grace expiry and active stream draining remain open. See [checkpoint](checkpoints/CP36-virtual-key-rotation-grace-and-revocation.md). |
| CP37 | FinOps reconciliation and DEF-018 retest | PASS for the current exact aggregate and Model grouping: $0.000010 across ten exact requests. FAIL DEF-018: all 14 Virtual Key budgets returned/displayed MONTHLY by UI/API persist as TOTAL_LIFETIME; the Project period matches. Other roles/scopes and exports remain open. See [checkpoint](checkpoints/CP37-finops-post-key-reconciliation.md). |
| CP38 | FinOps Viewer read/write boundary | PASS for one real FINOPS_VIEWER session: read budgets/chargeback, Create budget hidden, budget PUT and Virtual Key list denied with 403, project budget unchanged. The synthetic QA login was suspended and its hash cleared. Other finance operations and roles remain open. See [checkpoint](checkpoints/CP38-finops-viewer-read-write-boundary.md). |
| CP39 | Fresh org-only owner, Members & Roles no-project state, tenant boundary | FAIL DEF-023: visually inspected no-project UI showed zero members while authenticated API/persistence showed one; after project/environment selection the row appeared. A scoped cross-tenant budget update returned 404 without mutation. Other tenant routes/roles remain UNVERIFIED. See [checkpoint](checkpoints/CP39-org-owner-tenant-isolation.md). |
| CP40 | Instance-only Operator and hostname-resolved private MCP CIDR enforcement | PASS for the scoped Operator read and `localhost` CIDR cases: explicit `::1/128` reached the dual-stack fixture; mismatch and v4-only lists blocked before fixture contact. Generic sync error repeats QA-OBS-004. Operator writes, DNS rebinding/redirects and other runtimes remain UNVERIFIED. See [checkpoint](checkpoints/CP40-operator-and-hostname-cidr-boundary.md). |
| CP41 | MCP redirect SSRF behavior with same-CIDR and cross-CIDR targets | PASS for scoped fail-closed behavior: direct private target succeeded; MCP sync did not follow either 307 redirect and neither target received a request. Redirect-target revalidation and support semantics remain UNVERIFIED. See [checkpoint](checkpoints/CP41-mcp-redirect-ssrf-boundary.md). |
| CP42 | IPv4-mapped IPv6 private MCP endpoint form | PASS for fail-closed behavior: exact `::ffff:127.0.0.1/128` input returned 403 `egress_private_ip_blocked`, no row persisted, no fixture request. Mapped notation support remains UNVERIFIED (QA-OBS-007). See [checkpoint](checkpoints/CP42-ipv4-mapped-mcp-address.md). |
| CP43 | Console workspace-scope consistency and FinOps budget visibility | FAIL DEF-023 (expanded) and DEF-024. MCP Hub list scope remains unresolved QA-OBS-008. See [checkpoint](checkpoints/CP43-console-workspace-scope-and-finops.md). |
| CP44 | Org-only Console screens and Models deployment form | FAIL DEF-025; Add deployment is enabled without environment scope but cannot succeed and has no in-drawer scope selection. No backend mutation. See [checkpoint](checkpoints/CP44-org-only-console-surface-and-model-drawer.md). |
| CP45 | Project-scoped Team Admin and MCP authorization boundary | PASS for tested role boundary: Team Admin only sees assigned project1 and MCP rows; project2 budget query returned 403. See [checkpoint](checkpoints/CP45-project-scoped-role-and-mcp-boundary.md). |
| CP54 | Route Explain versus live Ollama route | FAIL DEF-028: dry-run showed zero eligible / two dialect mismatches for the same Ollama route that returned HTTP 200 in Playground; `ollama` API dry-run selected both targets. No live upstream dispatch from explain; temporary key revoked; DEF-017 recurred. See [checkpoint](checkpoints/CP54-route-explain-ollama-eligibility.md). |
| CP55 | Virtual Key RPM enforcement and recovery | PASS for one environment key: first Data Plane request returned 200, immediate second returned 429 `rate_limit_exceeded` with Retry-After 60 and no provider dispatch, and a later request recovered with 200; temporary key revoked. Cap provisioning in UI remains unverified; no defect classified. See [checkpoint](checkpoints/CP55-vkey-rpm-enforcement-recovery.md). |
| CP56 | Hierarchical policy scope and residency inheritance | PASS for reversible organization EU restriction: effective organization/project/environment API state narrowed to EU, both child editors exposed only EU, and organization policy was restored to the original four regions. No widening child write or inference was sent. See [checkpoint](checkpoints/CP56-policy-scope-inheritance.md). |
| CP57 | Policy-controlled live inference and recovery | PASS: synthetic ON_PREM route returned 200; EU-only organization policy caused a pre-egress 503; restoring all four regions returned 200 again. Persisted no-compliant-deployment trace; temporary key revoked. QA-OBS-006 recurred. See [checkpoint](checkpoints/CP57-policy-inference-enforcement.md). |
| CP58 | Hostname-resolved private IP CIDR enforcement | PASS: `127.0.0.1.nip.io` resolved to loopback; mismatched CIDR sync returned 502 with zero fixture contact, while exact loopback `/32` reached only the synthetic fixture. DEF-026 and QA-OBS-004 recurred; DNS rebinding remains untested. See [checkpoint](checkpoints/CP58-hostname-resolved-private-cidr.md). |
| CP53 | Public webhook prerequisite and Operator access | BLOCKED for registration/delivery because no usable Operator credential was available. Org Owner correctly lacks Operations navigation. A disposable synthetic receiver was created/deleted (201/204) with no payload sent. QA-OBS-003 recurred. See [checkpoint](checkpoints/CP53-public-webhook-prerequisite-and-operator-access.md). |
| CP52 | Multi-target priority failover and route selection identity | PASS for one synthetic streaming failover: first provider returned 503, backup returned 200; persisted attempt order matched. FAIL DEF-027: multiple healthy same-model deployments from different connections are indistinguishable in route-target choices and summary. DEF-017 recurred; temporary key revoked. See [checkpoint](checkpoints/CP52-multi-target-failover-and-route-selection.md). |
| CP51 | Organization budget enforcement and DEF-019 expansion | A synthetic child project/environment route succeeded under the organization ceiling; after temporary cap reduction, Playground returned generic 429 while the persisted trace carried `project_budget_exceeded` and no upstream request was dispatched. Cap restored and both temporary keys revoked. DEF-019 expanded; DEF-024 remains open. See [checkpoint](checkpoints/CP51-org-budget-enforcement-and-defect-019-expansion.md). |
| CP46 | Provisioning audit event and chain retest | PASS for CP43 project/environment and CP45 member creates; 55-event JCS chain verified. Failure injection and full organization provisioning coverage remain open. See [checkpoint](checkpoints/CP46-provisioning-audit-fresh-retest.md). |
| CP59 | A2A disabled-runtime comparison and refresh-resilient task lifecycle | PASS on the enabled isolated runtime; A2A use BLOCKED on the disabled runtime by configuration. Same task ID persisted across navigation/reload, resumed through UI, completed, then key revoked. QA-OBS-010 tracks control-plane/data-plane availability contract. |
| CP60 | DEF-023 source reconciliation and private hostname aliases | FAIL DEF-023 confirmed in source and prior UI/API evidence; `127.1` and trailing-dot localhost fail closed under explicit mismatched CIDRs. QA-OBS-004 recurred; QA-OBS-011 tracks undocumented shorthand-host support. |
| CP61 | Direct IPv6 MCP endpoint CIDR controls and first discovery | PASS exact `::1/128` synthetic sync; mismatch `::2/128` failed closed with zero fixture contact. DEF-026 and QA-OBS-004 recurred. |
| CP62 | Delayed Access & Keys response after scope switch | FAIL DEF-029: held 200 response showed ten old environment keys under No project / No env; navigation cleared them. See CP62. |
| CP63 | Delayed Routes & Policies response after scope switch | FAIL DEF-030: three old environment routes reappeared in No project / No env with Edit/Delete and enabled Save; no mutation. See CP63. |
| CP64 | WebKit mobile Access & Keys scope race | FAIL DEF-029 reconfirmed at 375×844: ten old environment key rows appeared after a held response was released under No project / No env. Screenshots visually inspected; no horizontal overflow. See CP64. |
| CP65 | Access & Keys cross-project delayed-response race | FAIL DEF-029 expanded: ten secondary-project rows appeared under a different selected project/environment; refresh returned to runtime-default scope (QA-OBS-009). No auth bypass demonstrated. See CP65. |
| CP66 | WebKit mobile MCP CIDR validation and error feedback | FAIL DEF-031: invalid `/33` was safely rejected with HTTP 400/no persistence, but feedback was obscured behind the open drawer. See CP66. |
| CP67 | WebKit mobile A2A CIDR validation feedback | FAIL DEF-031 expanded to A2A: invalid `/33` was rejected with HTTP 400/no persistence, but feedback was obscured behind the drawer. See CP67. |

| CP68 | Enabled-runtime A2A private registration, discovery and input-required task lifecycle | PASS for one synthetic A2A-only flow through discovery, same-task refresh, UI resume, settlement and revoked-key denial. Provider-plane request returned 403. See CP68. |

| CP69 | A2A input-required cancellation and held-budget release | PASS for corrected synthetic run: canceled the same task after refresh; database confirmed released reservation and zero settled cost; fixture had no follow-up call. Initial fixture port mismatch recorded as harness error. See CP69. |

| CP70 | Routes & Policies cross-project delayed-response race | FAIL DEF-030 expanded: three prior-project routes appeared while a different project/environment remained selected; Edit/Delete available, no mutation. QA-OBS-012 tracks a one-run transient error that was not reproduced. See CP70. |
