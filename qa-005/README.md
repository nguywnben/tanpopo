# QA-005 — TEEP-00 Baseline Stabilization

Independent assurance of exact unsigned RC7 source commit `737079ddb847d97dbcf832f610ed0048556cb65b`. This campaign is not final release certification. Production signing and `GATE-REL-07` remain blocked.

## Campaign records

- [Baseline assurance report](BASELINE-ASSURANCE-REPORT.md)
- [DEF-001–079 regression ledger](DEFECT-REGRESSION-LEDGER.md)
- [New defect register](NEW-DEFECT-REGISTER.md)
- [Product gap observations](PRODUCT-GAP-REGISTER.md)
- [Coverage and evidence matrix](COVERAGE-AND-EVIDENCE-MATRIX.md)
- [Residual risk register](RESIDUAL-RISK-REGISTER.md)
- [Environment and artifact identity](ENVIRONMENT-AND-ARTIFACT-IDENTITY.md)
- [Engineering remediation claims](ENGINEERING-REMEDIATION-CLAIMS.md)
- [Historical QA-001–004 records and evidence](historical/)

## Rules

QA-005 uses exact statuses **PASS, FAIL, PARTIAL, UNVERIFIED, BLOCKED**. Every result names source commit, runtime/artifact, environment, steps, evidence and limitations. No historical result is inherited. New defects start at DEF-080; accepted-spec absence is recorded as PRODUCT_GAP, not a defect. No product source, specification, release signature, tag or release will be changed. Paid provider usage is not authorized; use deterministic local fixtures. `GATE-REL-07` remains BLOCKED.

## Current checkpoint

- [CP00 — Baseline identity and compiled RC7 artifact](checkpoints/CP00-baseline-identity.md)
- [CP10–12 — RBAC, pricing boundary, backup and restore](checkpoints/CP10-12-rbac-finops-backup-recovery.md)
- [CP13–15 — Runtime recovery, PostgreSQL/Valkey and key lifecycle](checkpoints/CP13-15-runtime-operations-postgres-rbac-rotation.md)
- [CP16 — PostgreSQL Virtual Key and Data Plane authentication](checkpoints/CP16-postgres-virtual-key-and-data-plane-auth.md)
- [CP17–19 — Mobile layout, localization, and FinOps exports](checkpoints/CP17-19-console-mobile-localization-finops-exports.md)
- [CP20 — DEF-079 budget alert remediation retest](checkpoints/CP20-def079-budget-alert-remediation.md)
- [CP21 — DEF-078 FinOps reconciliation retest](checkpoints/CP21-def078-finops-reconciliation.md)
- [CP22 — Webhook SSRF control and Vault cleanup](checkpoints/CP22-webhook-ssrf-and-vault-cleanup.md)
- [CP23 — DEF-002 provider promotion guard](checkpoints/CP23-def002-failed-provider-promotion-guard.md)
- [CP24 — A2A authorization and input-required lifecycle](checkpoints/CP24-a2a-authorized-input-required-lifecycle.md)
- [CP25 — MCP Streamable HTTP lifecycle](checkpoints/CP25-mcp-streamable-tool-lifecycle.md)
- [CP26 — MCP post-pin schema drift re-lock](checkpoints/CP26-mcp-schema-drift-relock.md)
- [CP27 — Multi-organization scope and cross-screen context](checkpoints/CP27-multi-org-scope-cross-screen.md)
- [CP28 — DEF-069 A2A active-work cancellation on RC7](checkpoints/CP28-a2a-active-work-cancellation.md)
- [CP29 — DEF-067 upstream `working` response](checkpoints/CP29-def067-upstream-working-response.md)
- [CP30 — Provisioning audit chain](checkpoints/CP30-provisioning-audit-chain.md)
- [CP31 — DEF-071 reserved extension namespace retest](checkpoints/CP31-reserved-extension-namespace-retest.md)
- [CP32 — DEF-055 environment display-name readback](checkpoints/CP32-def055-environment-name-readback.md)
- [CP33 — DEF-058 quarantine recovery and stale status badge](checkpoints/CP33-def058-quarantine-recovery.md)
- [CP34 — DEF-060 disabled rollback guidance](checkpoints/CP34-def060-rollback-guidance.md)
- [CP35 — first project budget and parent visibility](checkpoints/CP35-first-budget-and-parent-visibility.md)
- [CP36 — private MCP/A2A CIDR input regressions](checkpoints/CP36-private-mcp-a2a-cidr-inputs.md)
- [CP37 — DEF-001 fresh setup progress names](checkpoints/CP37-def001-fresh-setup-progress-names.md)
- [CP38 — DEF-018 Virtual Key period labels](checkpoints/CP38-def018-virtual-key-period-label.md)
- [CP39 — DEF-019 Playground budget reason and ephemeral-key lifecycle](checkpoints/CP39-def019-budget-rejection-and-playground-key-lifecycle.md)
- [CP40 — Native Ollama exact accounting, NDJSON streams, and DEF-083](checkpoints/CP40-native-ollama-accounting-and-streaming.md)
- [CP41 — DEF-074 native Ollama image-unit settlement](checkpoints/CP41-native-ollama-image-unit-settlement.md)
- [CP42 — DEF-023 populated organization-only Members & Roles](checkpoints/CP42-def023-populated-members-org-only-scope.md)
- [CP43 — DEF-025 no-project deployment prerequisite](checkpoints/CP43-def025-no-project-deployment.md)
- [CP44 — DEF-068 active organization shred receipt](checkpoints/CP44-def068-active-organization-shred-receipt.md)
- [CP45 — DEF-072 split-role inventory](checkpoints/CP45-def072-split-node-inventory.md)
- [CP46 — Shared PostgreSQL/Valkey RPM across Data Planes](checkpoints/CP46-postgres-valkey-cross-dataplane-rpm.md)
- [CP47 — Cross-Data Plane Virtual Key revocation](checkpoints/CP47-cross-dataplane-key-revocation.md)
- [CP48–49 — A2A input-required lifecycle and provider-only key boundary](checkpoints/CP48-49-a2a-input-required-and-plane-boundary.md)
- [CP50 — DEF-069 PostgreSQL/Valkey cancellation with nonzero hold](checkpoints/CP50-def069-postgres-nonzero-hold-cancellation.md)
- [CP51 — Valkey full outage fallback and recovery](checkpoints/CP51-valkey-full-outage-fallback-and-recovery.md)
- [CP52 — PostgreSQL + Valkey partition fail-closed behavior and environment recovery](checkpoints/CP52-postgresql-valkey-partition-fail-closed.md)
- [CP53 — Console mobile navigation and empty-state sweep](checkpoints/CP53-mobile-console-navigation-sweep.md)
- [CP54 — `FINOPS_VIEWER` RBAC boundary and fresh DEF-080 reproduction](checkpoints/CP54-finops-viewer-rbac-and-def080.md)
- [CP55 — WebKit browser smoke and Firefox environment blocker](checkpoints/CP55-webkit-cross-browser-smoke.md)
- [CP56 — PostgreSQL provisioning events and audit-chain verification](checkpoints/CP56-postgresql-provisioning-audit-chain.md)
- [CP57 — Budgetless settlement and all FinOps export dimensions](checkpoints/CP57-finops-no-budget-and-all-export-dimensions.md)
- [CP58 — DEF-017 Playground request-to-trace correlation](checkpoints/CP58-def017-playground-trace-correlation.md)
- [CP59 — DEF-011 A2A resume evidence reconciliation](checkpoints/CP59-def011-a2a-resume-evidence-reconciliation.md)
- [CP60 — DEF-056 Team Admin model and deployment provisioning](checkpoints/CP60-def056-teamadmin-provisioning.md)
- [CP61 — OTLP runtime export, signal paths, and limited privacy probe](checkpoints/CP61-otlp-runtime-export-and-privacy.md)
- [CP62 — Real Collector semantic decode of RC7 OTLP signals](checkpoints/CP62-real-otel-collector-semantic-decode.md)
- [CP63 — DEF-065 Prometheus scrape on the served runtime](checkpoints/CP63-def065-prometheus-scrape.md)
- [CP64 — Fresh-install Console backup, audit linkage, and restored service](checkpoints/CP64-def008-console-backup-and-restore.md)
- [CP65 — DEF-004 CLI configuration inspection dispatch](checkpoints/CP65-def004-cli-config-show-dispatch.md)
- [CP66 — DEF-006 project-scoped member provisioning](checkpoints/CP66-def006-project-scoped-member-provisioning.md)
- [CP67 — DEF-006/016 pure Team Admin lifecycle and list boundary](checkpoints/CP67-def006-016-teamadmin-boundary.md)
- [CP68 — DEF-020 kill-switch clear badge retest](checkpoints/CP68-def020-killswitch-clear-badge.md)
- [CP69 — DEF-075 Virtual Key grace expiry boundary](checkpoints/CP69-def075-rotation-grace-boundary.md)
- [CP70 — DEF-038 post-crypto-shred backup restore](checkpoints/CP70-def038-post-shred-backup-restore.md)
- [CP71 — DEF-041 crypto-shred audit failure and retry](checkpoints/CP71-def041-audit-failure-recovery.md)
- [CP72 — DEF-040 unverified provider disable and recovery](checkpoints/CP72-def040-provider-disable-recovery.md)
- [CP73 — DEF-042 organization-member role picker scope](checkpoints/CP73-def042-org-member-role-scope.md)
- [CP74 — DEF-045 Playground failed-request trace visibility](checkpoints/CP74-def045-playground-failed-trace.md)
- [CP75 — DEF-033 FinOps duplicate-create guard](checkpoints/CP75-def033-budget-duplicate-create.md)
- [CP76 — DEF-034 exact-RC7 webhook revoke and audit verification](checkpoints/CP76-def034-webhook-revoke-rc7.md)
- [CP77 — DEF-047 cross-tab Playground logout retest](checkpoints/CP77-def047-cross-tab-playground-logout.md)
- [CP78 — DEF-046 Playground scope-switch stale-state retest](checkpoints/CP78-def046-playground-scope-state.md)
- [CP79 — DEF-021 local member enrollment retest](checkpoints/CP79-def021-local-enrollment.md)
- [CP80 — DEF-022 role-revocation stale-session retest](checkpoints/CP80-def022-role-revocation-stale-session.md)
- [CP81 — DEF-029 Access & Keys stale-response retest](checkpoints/CP81-def029-access-scope-race.md)
- [CP82 — DEF-037 Virtual Key audit contract retest](checkpoints/CP82-def037-vkey-audit-contract.md)
- [CP83 — DEF-028 Route Explain ingress-dialect retest](checkpoints/CP83-def028-route-explain-dialect.md)
- [CP84 — DEF-021 one-time invite replay retest](checkpoints/CP84-def021-invite-token-replay.md)
- [CP85 — Narrow viewport Members & Roles workflow](checkpoints/CP85-mobile-members-responsive.md)
- [CP86 — WebKit mobile Members & Roles workflow](checkpoints/CP86-webkit-mobile-members.md)
- [CP87 — WebKit Members & Roles accessibility slice](checkpoints/CP87-webkit-mobile-a11y.md)
- [CP88 — Team Admin multi-scope isolation](checkpoints/CP88-teamadmin-multiscope-isolation.md)
- [CP89 — Team Admin nested-resource isolation](checkpoints/CP89-teamadmin-nested-resource-isolation.md)
- [CP90 — DEF-066 Team Admin Vault persistence](checkpoints/CP90-def066-teamadmin-vault-persistence.md)
- [CP91 — DEF-057 logical-model display name](checkpoints/CP91-def057-model-display-after-refresh.md)
- [CP92 — DEF-035 deployment scope](checkpoints/CP92-def035-deployment-scope-retest.md)
- [CP93 — DEF-054 Team Admin routes under policy error](checkpoints/CP93-def054-teamadmin-routes-policy-error.md)
- [CP94 — DEF-030 Routes & Policies scope switching](checkpoints/CP94-def030-route-scope-switch.md)
- [CP95 — DEF-030 held prior-scope response](checkpoints/CP95-def030-held-route-response.md)
- [CP96 — DEF-026 MCP first-review sync state](checkpoints/CP96-def026-mcp-first-review-state.md)
- [CP97 — DEF-044 active extension action state](checkpoints/CP97-def044-active-extension-actions.md)
- [CP98 — DEF-050 route capability warning](checkpoints/CP98-def050-route-capability-warning.md)
- [CP99 — DEF-031 mobile invalid-CIDR feedback](checkpoints/CP99-def031-mobile-invalid-cidr-feedback.md)
- [CP100 — DEF-027 route target identity](checkpoints/CP100-def027-route-target-identity.md)
- [CP101 — DEF-084 mobile Overview alert overflow](checkpoints/CP101-overview-mobile-alert-overflow.md)
- [CP102 — DEF-078 RC7 budget/chargeback reconciliation](checkpoints/CP102-def078-rc7-budget-chargeback-reconciliation.md)
- [CP103 — DEF-077 Virtual Key Chargeback identifiers](checkpoints/CP103-def077-chargeback-key-identifiers.md)
- [CP104 — DEF-032 provider connection identity](checkpoints/CP104-def032-provider-identity.md)

Campaign remains **IN PROGRESS**. Current new defects: DEF-080 Low, DEF-081 Major, DEF-082 Low, DEF-083 Moderate and DEF-084 Low FAIL; historical DEF-022 and DEF-069 fresh retest FAILs; DEF-011, DEF-020, DEF-028, DEF-041, DEF-048, DEF-060, DEF-072, DEF-075, DEF-077 and DEF-078 PARTIAL. As of CP104, QA-005 has 52 scoped historical PASSes, 15 PARTIALs, two fresh historical FAILs, and 10 historical findings still UNVERIFIED. CP82 verified the frozen docs and fresh real Virtual Key lifecycle agree on `virtual_key.created`/`virtual_key.revoked`, and the 726-event JCS chain verifies (scoped PASS; broader audit atomicity remains open). CP81 retested DEF-029 with a held old-scope response; after switching to No project / No env, the nine-key HTTP 200 response did not repopulate the UI (scoped PASS; other scope races remain open). CP80 freshly reproduced DEF-022: Owner downgrade PATCH 200 invalidated the Reviewer session (401) but left Security nav and Trigger kill switch visible until reload. CP79 independently passed one local Developer invite→activation→signed-in Console→persistent readback path for DEF-021; token expiry/replay, audit visibility, recovery and permission-negative cases remain UNVERIFIED. CP43 retested DEF-025; CP44 retested DEF-068 active scope-switch receipt; CP45–47 exercised split-role inventory, a three-node Valkey Cluster, cross-Data-Plane RPM, and Control Plane key revocation with Data Plane replay denial. CP48 exercised input-required → completed A2A on PostgreSQL/Valkey; CP49 verified provider-only key denial for discovery and task creation; CP50 reproduced DEF-069 on PostgreSQL/Valkey with nonzero hold and confirmed the unused reservation was released while downstream work continued. CP51 verified the documented Valkey outage RPM slice and PostgreSQL revocation fallback across both Data Planes, then restored all slots. DEF-072's missing-node visibility remains unresolved. CP57 independently reverified DEF-012 through two budgetless exact settlements and DEF-013/014/015 through a new-project workflow plus CSV/JSON export comparison for all four grouping dimensions. CP60 freshly retested DEF-056 as a scoped pass: pure project-scoped Team Admin model and deployment creates both returned HTTP 201 and persisted; prior QA-004 denial retained. CP59 reconciles DEF-011 as PARTIAL because same-task resume/completion is evidenced on SQLite and PostgreSQL/Valkey but the intermediate `working` observation is not. CP58 independently retested DEF-017 through a real streamed Playground journey and trace inspection; its successful slice passes while other adapters, roles, browsers, and failure/cancellation cases remain open. CP61/62 scoped-pass OTLP traces, metrics, and logs on exact RC7; a pinned real Collector decoded all three signal types and representative telemetry fields. CP63 scoped-passes DEF-065 with an authorized Prometheus scrape, missing/wrong-token denials, and real Chromium inspection. CP64 scoped-passes DEF-008 through a fresh real setup, Console backup creation, intact SYSTEM audit linkage, verified archive restore, restored readiness and Owner login. CP65 confirmed the old DEF-004 `config show` shadowing is absent at the dispatch/auth boundary; authorized payload remains unverified. CP66 scoped-passes DEF-006 project-bound invitation persistence and audit linkage. CP67 independently activates a pure Team Admin invite and verifies the project-filtered list after refresh while an unscoped list receives 403 (DEF-006/016 scoped PASS). CP68 clears a project kill switch and verifies badges/active list reconcile in-session (DEF-020 PARTIAL; approval decisions remain untested). Sensitive-payload minimization remains unverified; CP69 found contradictory exact-RC7 DEF-075 grace-finalization outcomes: one correlated T+30/T+61 run retained ROTATING_DUAL_VALID v2 without the finalization audit event; other correlated/persisted runs finalized. Keep PARTIAL pending repeatable timing and UI/API/database agreement. CP70 freshly scoped-passes DEF-038 on exact RC7: post-shred backup verify/restore with secret-envelope check preserved the shred receipt and intact two-event tenant audit chain; populated-target and other-backend recovery remain open. CP71 freshly retested DEF-041 using an audit-insert failure on an isolated SQLite clone; the Console clearly showed pending erasure/retry and the same UI action successfully finalized after the trigger was removed. Child-resource/restart/other-backend cases remain open (PARTIAL). CP72 scoped-passes DEF-040: RC7 allowed an unverified connection to be disabled with explicit recovery guidance, and a successful synthetic loopback probe restored both its Active status and linked deployment Healthy state. Route-selection and API split-state semantics remain unverified. CP73 scoped-passes DEF-042: the org-member picker omits Operator, while a direct unsupported-role API attempt remains denied with no persisted member. CP78 scoped-passes DEF-046 on a same-tab populated-project switch: after a synthetic 503, changing from `Initial Project/dev` to `QA005 No Budget Project/QA005 Budgetless Dev` cleared the prior error, prompt, selected route and trace. Organization/environment, refresh, in-flight and other role/browser cases remain open. CP77 scoped-passes DEF-047 in a same-context two-tab Chromium test: tab-B logout changed tab A to Session ended without refresh, removed Send, made the Admin session return 401 and made captured-key replay return 401 `invalid_virtual_key`. The pre-logout inference was blocked by no compliant deployment; successful-route correlation and other roles/browsers remain open. CP76 scoped-passes DEF-034 on exact RC7: a synthetic webhook was registered, explicitly deleted through the Console, removed from API readback, and both lifecycle events remained in an intact 618-event JCS chain; the write-only HMAC test credential was deleted, and no receiver delivery was sent. The earlier RC4 failure remains preserved; retries, disabled-event suppression, other roles/scopes and real receiver interoperability remain unverified. CP75 scoped-passes DEF-033 duplicate budget creation on exact RC7: a 3,000,000 micro-USD duplicate was rejected inline, emitted no write request, and left the 2,500,000/version-1 project ceiling intact. Historical QA-002 overwrite evidence remains; explicit update/revision concurrency and other scopes remain unverified. CP74 scoped-passes DEF-045 on two missing-route HTTP 503 Playground requests: the inline trace card appeared both times, and Inspect trace opened the matching persisted Traces record; provider/stream/timeout/cancellation and broader role/backend/browser cases remain unverified. CP83 retested DEF-028 in actual Route Explain UI: default normalized and explicit Ollama dry runs selected the same healthy loopback deployment without rejection. Since a successful same-route live request was not established, DEF-028 is PARTIAL and the live criterion remains open. CP84 verified one-time invite replay rejection for DEF-021 (first enrollment 200; same-token replay 401 `invalid_enrollment_token`; Owner persistent readback unchanged). CP85 exercised Access & Keys Members & Roles at 320px and 375px: mobile navigation, stacked rows, horizontally recoverable tabs and Add member drawer remained within the viewport with no page-level overflow; screenshots were visually inspected. PG-008/009/010 capture OTLP/scrape configuration and SYSTEM audit UI discoverability gaps. Product gaps PG-001–PG-011 remain separate. CP93 scoped-passes DEF-054 route retention after a browser-injected policy-read 403: actual route API/UI retained all 3 routes and 5 target choices. CP92 scoped-passes DEF-035 current-scope filtering: organization-only displayed no deployments, and project-wide no-environment views showed only rows from the selected project's named environment; its organization-only inventory contract remains open. CP91 scoped-passes DEF-057 for one persisted Team Admin logical model whose display name survived actual UI reload and API readback. CP90 scoped-passes DEF-066 on RC7: project-owned secret metadata persisted in a real Team Admin flow across hard refresh and new login; unfiltered reads remained forbidden, plaintext was absent, and the canary was deleted. CP88 verifies a scoped pure-Team-Admin collection-read boundary and records PG-011 for the ambiguous no-project selector/list-label state; no unauthorized record was exposed. TEEP-01 readiness is not determined. See the [ongoing assurance report](BASELINE-ASSURANCE-REPORT.md).


CP94 scoped-passes DEF-030: the real Owner Routes & Policies screen switched among two populated environments and organization-only scope; filtered API IDs/counts matched each rendered set, no stale rows appeared, and the original set returned on switching back. The historical delayed-response race, refresh/navigation and other roles/backends/browsers remain open. Four screenshots were visually inspected. See [CP94](checkpoints/CP94-def030-route-scope-switch.md) and [sanitized evidence](evidence/CP94-def030-route-scope-switch.json). Current historical distribution: 50 scoped PASS, 9 PARTIAL, 2 fresh FAIL and 18 UNVERIFIED. `GATE-REL-07` remains BLOCKED.

CP95 additionally retested the DEF-030 delayed-response race on exact RC7: the actual HTTP 200 four-row Initial Project response was held, the real QA project response then rendered three matching rows, and releasing the old response did not change the current list. Hard refresh, navigation, repeated rapid switches, stale actions, other roles/backends/browsers remain open. Three screenshots were visually inspected. See [CP95](checkpoints/CP95-def030-held-route-response.md) and [sanitized evidence](evidence/CP95-def030-held-route-response.json). The scoped status distribution is unchanged. `GATE-REL-07` remains BLOCKED.


CP96 scoped-passes DEF-026: a fresh read-only MCP tool stayed visibly and persistently locked on first discovery and unchanged resync; the Console reported one pending review, schema drift was counted and remained locked, and authorized first pin aligned UI/API state. Active-key grant denial and pre-pin invocation denial remain open. Six screenshots were visually inspected. See [CP96](checkpoints/CP96-def026-mcp-first-review-state.md) and [sanitized evidence](evidence/CP96-def026-mcp-first-review-state.json). Current historical distribution: 51 scoped PASS, 9 PARTIAL, 2 fresh FAIL and 17 UNVERIFIED. `GATE-REL-07` remains BLOCKED.


CP97 retests DEF-044 read-only: the active official adapter's real action menu no longer presents Uninstall, avoiding the historical confirm-then-409 dead end. Quarantine remains available, but its prerequisite relationship to removal is not explained. No mutation was done because quarantining the shared QA Ollama adapter would interrupt subsequent provider workflows; isolated clone creation is blocked by overlay ENOSPC. DEF-044 remains PARTIAL pending post-quarantine lifecycle/audit and switch cleanup evidence. See [CP97](checkpoints/CP97-def044-active-extension-actions.md) and [sanitized evidence](evidence/CP97-def044-active-extension-actions.json). Current historical distribution: 51 scoped PASS, 10 PARTIAL, 2 fresh FAIL and 16 UNVERIFIED. `GATE-REL-07` remains BLOCKED.


CP98 retested DEF-050 in the real RC7 route drawer with a single primary and then two priority-failover targets. The old false “fallback target” language is absent, but the new generic `target deployment` warning does not name the affected primary or identify which fallback lacks tool_calling. Neither form was saved; no provider was contacted. DEF-050 remains PARTIAL; direct capability API readback and no-egress rejection are still open. Three screenshots were visually inspected. See [CP98](checkpoints/CP98-def050-route-capability-warning.md) and [sanitized evidence](evidence/CP98-def050-route-capability-warning.json).

CP99 retested DEF-031 in both real mobile registration drawers. MCP `127.0.0.1/33` and A2A wildcard `0.0.0.0/0` each produced a visually inspected inline error at 375×844, no registration POST, and no persisted record on API readback. This is a scoped PASS for visible client-side rejection, not proof of server-side CIDR enforcement or the broader SSRF matrix. Desktop, corrected retry, keyboard and alternate browser cases remain open. See [CP99](checkpoints/CP99-def031-mobile-invalid-cidr-feedback.md) and [sanitized evidence](evidence/CP99-def031-mobile-invalid-cidr-feedback.json).

CP100 retested DEF-027 in the current real route picker: candidate names include connection, model, deployment suffix, region and health; selecting one same-model candidate retained its suffix in the target card. The drawer was canceled with zero route writes and no inference. This remains PARTIAL because the three-connection healthy failover and saved summary acceptance was not repeated. See [CP100](checkpoints/CP100-def027-route-target-identity.md) and [evidence](evidence/CP100-def027-route-target-identity.json). Current historical distribution: 52 scoped PASS, 12 PARTIAL, two fresh historical FAILs and 13 UNVERIFIED. `GATE-REL-07` remains BLOCKED.


CP101 discovered DEF-084: a long active budget-alert identifier makes the loaded Overview page 413px wide at both 375px and 320px viewports. The full-width screenshot shows the ID clipped. The alert remained after hard refresh; the summary API returned 200 and no errors or writes occurred. Low severity because the Remediate button remains visible and no budget or access failure was found. See [CP101](checkpoints/CP101-overview-mobile-alert-overflow.md) and [evidence](evidence/CP101-overview-mobile-alert-overflow.json). Historical DEF-001–079 distribution remains 52 scoped PASS, 12 PARTIAL, two fresh historical FAILs and 13 UNVERIFIED; the campaign remains in progress and `GATE-REL-07` BLOCKED.


CP102 retested DEF-078 read-only on exact RC7. For the current QA005 project, monthly budget API/UI, Chargeback UI and November-filtered usage API agreed at 3,000,000 exact / 0 estimated micro-USD; all relevant APIs returned 200. No writes or inference occurred. This does not overturn QA-004's historical 1-micro difference in both buckets, which came from another project and was corroborated on PostgreSQL. DEF-078 is now PARTIAL pending recreated default-project and exact/estimated budget-active cases. See [CP102](checkpoints/CP102-def078-rc7-budget-chargeback-reconciliation.md) and [sanitized evidence](evidence/CP102-def078-rc7-reconciliation.json). Current historical distribution: 52 scoped PASS, 13 PARTIAL, two fresh historical FAIL and 12 UNVERIFIED. `GATE-REL-07` remains BLOCKED.


CP103 retested DEF-077 in the real exact-RC7 Owner Chargeback UI. Duplicate key names were individually identified by stable key ID and masked prefix, and the actual CSV and JSON exports each contained both fields for all ten rows. QA-004's 11-row duplicate-name failure remains in history. The fresh duplicate rows had equal usage values, so different-usage acceptance remains unverified and DEF-077 is PARTIAL. No key creation, provider calls or paid usage occurred. See [CP103](checkpoints/CP103-def077-chargeback-key-identifiers.md) and [sanitized evidence](evidence/CP103-def077-chargeback-identifiers.json). Current historical distribution: 52 scoped PASS, 14 PARTIAL, two fresh historical FAIL and 11 UNVERIFIED. `GATE-REL-07` remains BLOCKED.


CP104 retested DEF-032 on the exact RC7 real Providers screen. The five available connections have distinct IDs; list rows and the tested edit drawer display stable short-ID suffixes. The historical duplicate-name fixture was unavailable, and Probe/Disable/Delete target identity was not exercised, so DEF-032 moves from UNVERIFIED to PARTIAL. No provider calls or writes occurred. See [CP104](checkpoints/CP104-def032-provider-identity.md) and [sanitized evidence](evidence/CP104-def032-provider-identity.json). Current historical distribution: 52 scoped PASS, 15 PARTIAL, two fresh historical FAILs and 10 UNVERIFIED. Five new findings DEF-080–084 remain FAIL. `GATE-REL-07` remains BLOCKED.
