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

Campaign remains **IN PROGRESS**. Current new defects: DEF-080 Low, DEF-081 Major, DEF-082 Low, DEF-083 Moderate FAIL; historical DEF-069 fresh retest FAIL; DEF-011, DEF-020, DEF-048, DEF-060, DEF-072 and DEF-075 PARTIAL. QA-005 has 34 scoped historical PASSes, 7 PARTIALs, one historical FAIL, and 37 historical findings still UNVERIFIED. CP43 retested DEF-025; CP44 retested DEF-068 active scope-switch receipt; CP45–47 exercised split-role inventory, a three-node Valkey Cluster, cross-Data-Plane RPM, and Control Plane key revocation with Data Plane replay denial. CP48 exercised input-required → completed A2A on PostgreSQL/Valkey; CP49 verified provider-only key denial for discovery and task creation; CP50 reproduced DEF-069 on PostgreSQL/Valkey with nonzero hold and confirmed the unused reservation was released while downstream work continued. CP51 verified the documented Valkey outage RPM slice and PostgreSQL revocation fallback across both Data Planes, then restored all slots. DEF-072's missing-node visibility remains unresolved. CP57 independently reverified DEF-012 through two budgetless exact settlements and DEF-013/014/015 through a new-project workflow plus CSV/JSON export comparison for all four grouping dimensions. CP60 freshly retested DEF-056 as a scoped pass: pure project-scoped Team Admin model and deployment creates both returned HTTP 201 and persisted; prior QA-004 denial retained. CP59 reconciles DEF-011 as PARTIAL because same-task resume/completion is evidenced on SQLite and PostgreSQL/Valkey but the intermediate `working` observation is not. CP58 independently retested DEF-017 through a real streamed Playground journey and trace inspection; its successful slice passes while other adapters, roles, browsers, and failure/cancellation cases remain open. CP61/62 scoped-pass OTLP traces, metrics, and logs on exact RC7; a pinned real Collector decoded all three signal types and representative telemetry fields. CP63 scoped-passes DEF-065 with an authorized Prometheus scrape, missing/wrong-token denials, and real Chromium inspection. CP64 scoped-passes DEF-008 through a fresh real setup, Console backup creation, intact SYSTEM audit linkage, verified archive restore, restored readiness and Owner login. CP65 confirmed the old DEF-004 `config show` shadowing is absent at the dispatch/auth boundary; authorized payload remains unverified. CP66 scoped-passes DEF-006 project-bound invitation persistence and audit linkage. CP67 independently activates a pure Team Admin invite and verifies the project-filtered list after refresh while an unscoped list receives 403 (DEF-006/016 scoped PASS). CP68 clears a project kill switch and verifies badges/active list reconcile in-session (DEF-020 PARTIAL; approval decisions remain untested). Sensitive-payload minimization remains unverified; PG-008/009/010 capture OTLP/scrape configuration and SYSTEM audit UI discoverability gaps. Product gaps PG-001–PG-010 remain separate. TEEP-01 readiness is not determined. See the [ongoing assurance report](BASELINE-ASSURANCE-REPORT.md).
