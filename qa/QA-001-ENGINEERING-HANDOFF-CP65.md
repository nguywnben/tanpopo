# Tanpopo QA-001 — Independent Engineering Handoff (CP65)

**Status:** QA is incomplete; all 15 defects remain open. **GATE-REL-07: BLOCKED.**  
**Baseline:** `a771311107c4e4c2c7d9b53a3f5a0926f114b799`  
**Scope:** QA evidence and engineering guidance only. No Tanpopo implementation files were modified.

This is the consolidated handoff for the next engineering and QA owners. It preserves the historical, append-only record rather than replacing earlier interpretations. Read this document with the [Defect Register](DEFECT-REGISTER.md), [Product Coverage Matrix](PRODUCT-COVERAGE-MATRIX.md), [Release Readiness](RELEASE-READINESS.md), [Alpha report](ALPHA-TEST-REPORT.md), [Beta/UX report](BETA-UX-REPORT.md), [Business Logic Audit](BUSINESS-LOGIC-AUDIT.md), [Security and Reliability report](SECURITY-AND-RELIABILITY-REPORT.md), [Regression report](REGRESSION-REPORT.md), and phase evidence in [`evidence/`](evidence/README.md). Existing targeted handoffs remain under [`engineering-handoffs/`](engineering-handoffs/).

## Executive summary and recommended order

The highest customer and operational risks are misleading provider health (DEF-002), missing audit attribution for tenant provisioning (DEF-007), the absence of supported A2A key provisioning despite correct authorization enforcement (DEF-010), and an A2A task lifecycle that cannot resume after requesting input (DEF-011). FinOps has internally contradictory spend totals and two export failures (DEF-012, DEF-014, DEF-015). These can mislead spend decisions or produce incomplete downstream records. Fix and verify these before relying on affected workflows.

Recommended engineering sequence:

1. **Provider readiness and audit integrity:** DEF-002 and DEF-007. Require health claims to reflect probe/serving eligibility; make required provisioning audit events atomic or fail-closed as the contract specifies.
2. **A2A complete, authorized journey:** DEF-010 and DEF-011 together. Add an appropriately authorized, narrow provisioning path while preserving the correct 403 for keys without the A2A plane; expose and implement a resumable `input-required` lifecycle.
3. **FinOps truth and exports:** DEF-012, DEF-014, DEF-015. Reconcile headline definitions to settlement data, support CSV response handling, and honor the selected grouping dimension.
4. **Remaining workflow and usability defects:** DEF-001, DEF-003–006, DEF-008–009, DEF-013. Resolve, document, and verify each affected workflow; retain the specific cross-browser uncertainty for DEF-009.

These are priority recommendations, not release approval. Verify each fix against the acceptance criteria below and the broader regression requirements. A single green test or local fix does not close QA-001.

## Evidence interpretation and rules for this handoff

- **FAIL** means the specified behavior was directly observed to fail in the named test state. **PASS** applies only to the named path/state and does not imply feature completion. **BLOCKED** means an environmental or dependency condition prevented the test. **UNVERIFIED** means there is no valid result for the target condition, or evidence conflicts and the target was not resolved.
- **Facts** below are observed behavior, recorded responses, screenshots, read-only data, or contract text. **Hypotheses** are candidate explanations and must be confirmed through engineering investigation.
- Do not classify Cloud egress/provider transport errors as product defects by themselves. Google probes failed in some isolated runtimes because outbound egress/proxy behavior differs; 8081 also reported `probe_runtime_unavailable`, with its precise listener/factory cause unresolved. No successful Tanpopo-mediated Google inference is claimed. DEF-002 is independently confirmed with a loopback HTTP 401 fixture and is a product state-consistency defect.
- The A2A 403 for a provider-only Virtual Key is correct authorization. DEF-010 is the missing supported provisioning and UX/business contract to issue an appropriately scoped key. Do not weaken A2A authorization to make the test pass.
- Screenshots listed below are retained under `evidence/`; key screenshots and interaction results were visually inspected during the QA sessions. Structured JSON records preserve requests, responses, and state. Refer to the phase record for full details; do not infer behavior beyond what that record captured.

## Consolidated defect register

Severity values preserve the register. Status reflects evidence at CP65, not whether a fix exists. All findings remain open.

### DEF-001 — Setup progress button accessible name omits visible text

- **Severity / status:** Major; **FAIL** in focused component tests, with contradictory live-browser evidence; unresolved.
- **Affected users/workflow/business impact:** Setup users relying on speech input may not be able to activate a progress step by its visible label; accessibility conformance and setup completion are at risk.
- **Expected / actual:** A button’s accessible name includes the visible label. The two original Setup Vitest cases fail axe `label-content-name-mismatch` on the step 1 progress button. CP31 live Chromium axe found no violation after authentication; CP38 completed the wizard. These differing states have not been reconciled.
- **Reproduce:** From `web/console`, run the two original Setup cases in `src/pages/console.test.tsx` (“hides unavailable presets…” and “offers only adapters…”). Inspect the axe node/name and compare with the authenticated live Setup state that passed.
- **Evidence:** `evidence/phase63-def001-targeted-vitest.txt`, `.json`; `evidence/phase31-def001-fresh-setup-labeling.json`; `evidence/phase4-chromium-e2e.json`; CP38 Setup evidence. See register for full history.
- **Facts:** Two targeted tests fail; the cited live scan did not. **Hypothesis:** The rendered label and generated `aria-label` diverge under the test fixture/state, or the runtime/test rendering paths differ.
- **Acceptance:** Both original cases and all Setup progress-step axe checks pass; accessible names contain the visible step wording; keyboard and speech-input activation work at every wizard step; live browser and test fixture use equivalent state and are documented.

### DEF-002 — Unverified provider deployment can be labeled healthy

- **Severity / status:** Major; **FAIL**, freshly reproduced using a local controlled fixture.
- **Affected workflows/business impact:** Provider setup, deployment activation, route health monitoring, and inference availability. Green status can cause operators to direct traffic to a route that cannot serve.
- **Expected / actual:** A failed/unverified backing connection cannot be represented as `ACTIVE_HEALTHY`/Healthy without a fresh successful health criterion. Loopback fixture returned HTTP 401; probe returned HTTP 200 with `valid=false`, `auth_failed`, and connection remained Created Unverified. Deployment activation nevertheless reported clean `ACTIVE_HEALTHY` and UI showed Healthy. Historical CP5 route request returned HTTP 503 `no_compliant_deployment_available`.
- **Reproduce:** On an isolated runtime, configure a loopback provider-compatible fixture that responds 401; probe it; create/activate a deployment and route; compare connection, deployment and route health. CP52–54 did not send inference to the failed fixture; the earlier CP5 request is separate historical evidence. Synthetic failed connection/deployment were deleted after capture.
- **Evidence:** `evidence/phase52-failed-connection.json`; `phase53-unverified-deployment.json`; `phase54-unverified-deployment-activation.json`; `phase54-unverified-deployment-activated.png`; `phase54-def002-fresh-repro.json`; historical `phase5-unverified-connection-health-mismatch.json` and health screenshots.
- **Facts:** The false health promotion is reproduced without Google or external egress. **Hypothesis:** activation API permits a healthy state without validating connection probe/readiness, and Console maps that state to a green label.
- **Acceptance:** Failed/stale probe cannot yield a healthy status; eligibility and UI status agree with the data-plane resolver; activation and route creation surface actionable failure/unverified state; a controlled healthy provider can still activate; error and recovery transitions are tested.

### DEF-003 — FinOps cannot create the first budget in Console

- **Severity / status:** Moderate; **FAIL**, rechecked CP58.
- **Affected workflows/business impact:** First-use spend governance and budget setup require an API or other out-of-band workaround; users may operate without the intended ceiling.
- **Expected / actual:** Authorized owner/team admin can create a budget from the empty state. Fresh tenant showed “No budgets configured,” with only CSV/JSON export and no create action. API-created budget could subsequently be viewed/edited; negative input was rejected.
- **Reproduce:** Sign in as Org Owner to a fresh isolated org, open FinOps → Budgets & Holds, inspect empty state; compare with API-created budget and subsequent UI edit. CP58 current evidence: `evidence/phase58-def003-empty-budget.png` and `.json`; historical `phase6-finops-budget-journey.json` and screenshots.
- **Facts:** No first-budget UI action; API creation/edit path works. **Hypothesis:** Console only wires list/update operations despite a documented create path.
- **Acceptance:** Empty state provides a role-appropriate create action; create/list/edit/validation and scope selection persist and agree across refresh/navigation; unauthorized roles cannot create; first budget is visible in all relevant FinOps views.

### DEF-004 — CLI `config show` is shadowed by duplicate command group

- **Severity / status:** Moderate; **FAIL**, rechecked CP59.
- **Affected workflows/business impact:** Operators and scripts receive exit 0 but no server configuration data, hiding diagnostics.
- **Expected / actual:** `tanpopo config show --output json` returns server config while local `config reconcile` remains available. Actual command prints only local config help and exits 0; `config reconcile --help` remains reachable.
- **Reproduce:** Run `tanpopo config show --output json`, inspect exit/output; then `tanpopo config reconcile --help`. Current record: `evidence/phase59-def004-retest.json`; baseline `phase9-cli-config-command-collision.json`.
- **Facts:** Duplicate root `config` registration is recorded in `internal/cli/ops` and `internal/cli/remote`; remote show is unreachable. **Hypothesis:** Cobra resolves the first same-named child and shadows the second group's subcommands.
- **Acceptance:** Both remote show and local reconcile are unambiguously reachable; show emits valid requested format and meaningful failure exit codes; help documents both; CLI regression test covers command tree and output.

### DEF-005 — MCP and A2A private endpoint forms omit required CIDR allowlist

- **Severity / status:** Moderate; **FAIL**, previously reproduced on both forms.
- **Affected workflows/business impact:** Users cannot register explicitly trusted internal MCP/A2A endpoints through the primary UI. Broadening SSRF access is not an acceptable workaround.
- **Expected / actual:** Private mode gathers/submits a narrow allowlist or otherwise provides an explicit safe contract. Both forms submit `allow_private_ip: true` without `allowed_cidrs`; APIs return HTTP 400 and create no record. CP43's separate provider connection form accepts `127.0.0.1/32`, so the current defect is scoped to MCP/A2A forms; parity elsewhere remains unverified.
- **Reproduce:** In isolated owner session, register loopback MCP and A2A endpoints with private opt-in; observe HTTP 400 `allowed_cidrs is required when allow_private_ip is true`, no record and no endpoint request. Evidence `evidence/phase10-private-integration-endpoint-journeys.json`, `phase10-mcp-private-endpoint-validation.png`, `phase10-a2a-private-endpoint-rejected.png`, plus CP27 and CP43 records.
- **Facts:** Both payloads omit a backend-required field. **Hypothesis:** Frontend form/request models do not carry `allowed_cidrs` for these registration endpoints.
- **Acceptance:** Both screens make the permitted CIDR explicit, validate it, submit it, show the effective allowlist after save, reject unsafe or malformed ranges, and never initiate a fetch before server validation. Cover IPv4/IPv6, loopback/private ranges, update/delete, and cross-screen persistence.

### DEF-006 — Team Admin member form cannot supply required project scope

- **Severity / status:** Moderate; **FAIL**, latest direct reproduction CP27 (not retested CP58–65).
- **Affected workflows/business impact:** Team Admin sees an enabled member-management action but cannot onboard a project-scoped teammate; confusing denial costs time and undermines role trust.
- **Expected / actual:** Form requests project scope or hides/disables unsupported action. Form only accepts email and role; submit returns HTTP 403 `role_escalation_denied`, no member created.
- **Reproduce:** Sign in as non-operator Team Admin in isolated project; Access & Keys → Members & Roles → Add member; enter QA-only email and Developer role; submit and verify 403 and unchanged count. Evidence `evidence/phase15-team-admin-add-member-form.png`, `phase15-team-admin-add-member-rejected.png`, `phase15-team-admin-add-member-journey.json`; CP27 regression summary.
- **Facts:** Backend denial is correct for incomplete project-scoped request. **Hypothesis:** UI exposes an action whose payload omits required `project_ids`.
- **Acceptance:** Team Admin can select only projects within its authority and create permitted scoped members, or sees a clear unavailable state; server authorization remains authoritative; role escalation and cross-org/project attempts remain denied; member list reflects successful changes after reload.

### DEF-007 — Organization/project/environment provisioning lacks audit events

- **Severity / status:** Major; **FAIL**, freshly reconfirmed CP48.
- **Affected workflows/business impact:** Tenant creation and workspace provisioning lack actor/action attribution; audit review and backup-chain verification may fail. This weakens accountability for foundational access boundaries.
- **Expected / actual:** Successful create operations append required audit records atomically or follow the documented fail-closed path. Fresh bootstrap created org/project/dev environment, but no corresponding provisioning events existed; 49 unrelated auth/key/provider/model/extension/deployment/route events did exist. Contract: TP-IMP-0024 AC-1 and SCR-08. Historical empty-chain backup verify failed; later owner/login events allowed a separate backup verify/restore, without retroactively adding provisioning entries.
- **Reproduce:** On isolated canonical runtime create org via Operations, project and environment via Console. Read-only query `audit_events` by created resource IDs. Compare zero provisioning events with unrelated audit event count. CP48 fresh evidence `evidence/phase48-fresh-bootstrap-audit.json`; historical creation and backup evidence `phase11-*`, `phase12-*`, `phase16-*`, `phase21-*`.
- **Facts:** Creates succeeded (HTTP 201) while matching audit records were absent. Later audit activity made one backup flow verifiable; this does not resolve missing records. **Hypothesis:** canonical runtime's store is constructed without transactional audit sink, while audit logger is passed only to HTTP server; repository handlers do not append fallback events.
- **Acceptance:** Each create (and relevant update/delete) produces correct event with actor, scope, resource ID, timestamp, and documented event type; event insertion is atomic/fail-closed per contract; verify chain integrity; test replay/rollback/error paths, each resource type, backup verify/restore on empty and populated tenant histories, and cross-tenant visibility.

### DEF-008 — Console backup action is enabled but runtime returns 503

- **Severity / status:** Moderate; **FAIL**, rechecked CP60.
- **Affected workflows/business impact:** Operators cannot create backups through Console despite an enabled action; must use CLI, increasing operational friction and risk of missed recovery points.
- **Expected / actual:** With valid config, lockfile, and artifact store, Console backup succeeds; otherwise UI marks action unavailable before submit. POST returned HTTP 503 `backup requires configured tanpopo.lock, tanpopo.yaml, and extension artifact store`; adding a valid QA config did not make Console path work. CLI backup with explicit paths worked in a distinct flow.
- **Reproduce:** Operations → Backups → Create encrypted backup; observe 503 and no receipt. Evidence `evidence/phase60-def008-backups-before.png`, `phase60-def008-backup-failure.png`, `.json`; historical `phase16-backup-*`.
- **Facts:** Source inspection in the record found `ServerConfig.ConfigPath` omitted by unified `serve`; the UI action remains enabled. **Hypothesis:** empty `ConfigPath` causes handler validation failure.
- **Acceptance:** Console and CLI use the configured paths consistently; successful backup yields receipt/artifact that verifies/restores; missing config disables or explains the action before submission; errors preserve recovery state and are actionable; test empty audit chain, extensions, encryption, restore and recovered login.

### DEF-009 — WebKit 375px webhook drawer was positioned offscreen

- **Severity / status:** Moderate; original **FAIL** CP18, **PASS/non-reproduction** CP27, CP49/65 **UNVERIFIED** because authentication/navigation did not reach drawer. Keep open pending reconciliation.
- **Affected workflows/business impact:** If reproduced, narrow-screen WebKit users cannot configure webhook subscriptions used for operational notifications.
- **Expected / actual:** All drawer controls fit in or can be reached within viewport. CP18 WebKit 26.5 at 375×844 showed drawer heading x=392 (outside viewport); Chromium/Firefox opened it visibly. CP27 WebKit showed heading x=73 at sampled delays. CP49/65 returned to login and never tested drawer; CP65 screenshot is sign-in only.
- **Reproduce:** At 375×844 in WebKit, authenticate, open Operations → Webhooks → Add webhook; capture screenshot and heading/control bounding boxes immediately and at 0/100/250/500/1000ms; compare same build/state in Chromium and Firefox. Evidence `evidence/phase18-webkit-drawer-layout.json`, three CP18 screenshots, `phase18-ops-cross-engine-mobile.json`, CP27 results, `phase49-def009-attempt.json`, and `phase65-def009-webkit-login-attempt.json` / `phase65-webkit-after-login.png`.
- **Facts:** Conflicting target-state observations exist; latest attempt is not a drawer test. **Hypothesis:** viewport/drawer timing, session, or harness differences explain CP18/27 conflict; implementation cause is unconfirmed.
- **Acceptance:** Repeated real WebKit sessions reach the same authenticated screen and drawer; heading, fields, actions and focus remain visible and operable at 375px with no horizontal overflow; verify open/close, validation, submission and screenshot/layout parity across supported engines.

### DEF-010 — Supported Virtual Key provisioning cannot grant A2A access

- **Severity / status:** Major; **FAIL** as provisioning/UX/business-contract gap. A2A authorization denial for provider-only keys is correct and must remain.
- **Affected workflows/business impact:** Tenant users cannot use A2A discovery/tasks with ordinary supported key issuance even when A2A is enabled; admins resort to unsupported direct data grants.
- **Expected / actual:** Authorized issuer can explicitly grant A2A plane and narrow route/agent access; default/provider-only key remains denied. Issue form/API exposes model and MCP controls but no A2A grant. On enabled isolated listener, ordinary UI-issued provider-only key returned 403 `scope_or_plane_denied` on `GET /a2a/agents`, and DB showed `allowed_planes=PROVIDER`. Feature-flag-off listener returning 404 is configuration, not this defect. CP39 positive A2A lifecycle used an isolated QA-only DB-granted A2A key; it was revoked and post-revocation returned 401.
- **Reproduce:** Enable A2A on isolated listener; issue ordinary key in Console; use cookie-free bearer client on `GET /a2a/agents`; inspect 403, form fields and key grants. Do not alter authorization. Evidence `evidence/phase32-vkey-issue-form.json`, `phase34-a2a-vkey-bearer-probe.json`, `phase34-vkey-a2a-scope-readback.json`, `phase39-a2a-lifecycle.json`, `phase41-a2a-fixture-cleanup.json`; detailed `engineering-handoffs/DEF-010-a2a-virtual-key-scope.md`.
- **Facts:** User independently confirmed rejection is correct. No standard provisioning route issues a permitted A2A key; synthetic authorized key proved downstream paths only. **Hypothesis:** create-key schema/handler normalizes absent `allowed_planes` to PROVIDER and the UI/API omit A2A-specific grant fields.
- **Acceptance:** Define documented role- and scope-authorized A2A grants in supported key issuance/management; least privilege and default denial remain; show scope before one-time secret; revoke/expiry immediately deny; audit creation/rotation/revoke; verify unauthorized issuer cannot grant broader scope; complete discovery and task journeys using only supported provisioning.

### DEF-011 — A2A `input-required` task cannot resume

- **Severity / status:** Major; **FAIL**, reproduced CP41. This is the unresolved input-required task lifecycle prioritized for engineering.
- **Affected workflows/business impact:** Any agent requesting clarification/approval leaves a persisted task stuck nonterminal; callers cannot continue, and task resources/budget/TTL handling may remain unresolved.
- **Expected / actual:** Under SM-09, caller supplies input and resumes same task to working, then a terminal state. Synthetic agent returned `input-required`; create returned 201 and task persisted; GET stayed `input-required`. POST `/a2a/tasks/{id}/transition` with state `working` and input parts returned 404 `resource_not_found`. Mounted task routes expose GET/cancel, not transition. Console shows Input required with no resume action.
- **Reproduce:** On A2A-enabled runtime with an authorized isolated synthetic endpoint returning `{"state":"input-required"}`, create task with target and nonempty input, GET it, then POST documented transition with new input; inspect 404 and Console row. Evidence `evidence/phase41-a2a-input-required.json`, visually reviewed `phase41-a2a-input-required-console.png`, CP39 baseline `phase39-a2a-lifecycle.json`. Contracts: `docs/specifications/mcp-a2a-boundaries.md` §3.1, SRS TP-FR-0405, TP-IMP-0042 AC-2, TP-AC-F0404. Detailed `engineering-handoffs/DEF-011-a2a-input-required-cannot-resume.md`.
- **Facts:** Internal `TaskCoordinator` supports transition request but mounted public route is absent; caller cannot resume via exposed API/UI. **Hypothesis:** route/handler and UI action were omitted while internal coordinator state transition exists.
- **Acceptance:** Supported API/UI accepts new input against same authorized task, transitions `input-required → working → terminal`, returns updated task history/state, preserves owner/tenant/agent authorization and idempotency, and settles/releases budget holds correctly on completion/cancel/timeout. Test malformed/late input, duplicate requests, expiry, cancellation, unauthorized caller, SSE/poll consistency and audit events.

### DEF-012 — FinOps headline omits settled usage when no budgets exist

- **Severity / status:** Major; **FAIL**, confirmed CP51.
- **Affected workflows/business impact:** First-use spend review and operator/finance decisions can report zero despite actual billed usage; users may understate cost.
- **Expected / actual:** Headline exact/estimated/unmetered metrics reconcile to usage settlement and chargeback independent of budget existence. One successful synthetic provider call settled 1 micro-USD; headline showed `$0.000000`, row showed `$0.000001` and `1 / 0`; DB contained one EXACT settlement and zero budget ledgers.
- **Reproduce:** Fresh no-budget tenant, issue successful local synthetic inference with exact nonzero usage, open FinOps → Chargeback grouped by project and compare KPI, row and settlement. Evidence `evidence/phase46-local-inference.json`, `phase47-finops-chargeback.png` / `.txt`, `phase51-finops-kpi-chargeback-mismatch.json`; handoff `engineering-handoffs/DEF-012-finops-headline-uses-budget-ledgers.md`.
- **Facts:** UI headline is zero while exact usage row/ledger is nonzero. **Hypothesis:** `FinOpsPage` derives KPI from budget list while rows use usage chargeback endpoint.
- **Acceptance:** KPI definitions and source data are explicit and reconcile with row totals/settlements for empty and populated budgets, all scopes and groupings, estimated/unmetered usage, rounding, refunds/adjustments, pagination/date filters; refresh and navigation show consistent values.

### DEF-013 — Ollama connection defaults to commercial API / US

- **Severity / status:** Moderate; **FAIL** form/manifest mismatch confirmed CP43; no bad record persisted in this test because values were corrected before save.
- **Affected workflows/business impact:** Local self-hosted provider setup can be misclassified for billing, region or policies if user accepts defaults.
- **Expected / actual:** Selecting bundled Ollama defaults to manifest `SELF_HOSTED_INFRA` and appropriate local region or requires explicit choice. Form preselected `COMMERCIAL_API` and `US`; official manifest declares `SELF_HOSTED_INFRA`. Tester corrected before save.
- **Reproduce:** Activate bundled Ollama, open Providers → New Connection, select Ollama and record untouched Billing mode/region before changing. Evidence `evidence/phase43-new-connection-form.png`, `phase43-ollama-connection-filled.png`, `phase43-ollama-probe-result.json`; manifest `extensions/official/ollama/manifest.yaml`.
- **Facts:** Form default contradicts manifest; no erroneous persisted connection in this run. **Hypothesis:** generic connection form defaults override or ignore adapter metadata.
- **Acceptance:** Adapter-specific defaults match manifest across create/edit/reopen; user can override intentionally; saved billing/region persists and is reflected in chargeback/policy views; test other adapters and legacy records.

### DEF-014 — Console CSV export parses CSV response as JSON

- **Severity / status:** Moderate; **FAIL**, confirmed CP61–62.
- **Affected workflows/business impact:** Finance/operators cannot download CSV from Console for spreadsheets and external reconciliation, despite server serving valid data.
- **Expected / actual:** CSV action downloads valid `text/csv`; JSON action downloads JSON. Backend returned HTTP 200 valid CSV, but UI attempted JSON parse, displayed `Unexpected token 'g', "group_key,"... is not valid JSON`, and no download occurred. JSON export succeeded.
- **Reproduce:** FinOps → Chargeback → Export CSV; inspect browser request, content type, UI error and downloads; compare Export JSON. Evidence `evidence/phase61-export-csv-ui.png`, `phase61-chargeback-export-csv.json`, `phase62-export-json.json`, `phase62-export-json-ui.png`. Detailed `engineering-handoffs/DEF-014-finops-csv-export.md`.
- **Facts:** Valid CSV reaches UI and is rejected as JSON; JSON works. **Hypothesis:** generated JSON API client is used for both formats.
- **Acceptance:** CSV downloads with correct MIME type, filename, encoding and rows; errors are distinct from parsing; test empty, quoted/Unicode/large/multiline data, filters/grouping, browser download behavior, and JSON regression.

### DEF-015 — Chargeback export ignores selected grouping dimension

- **Severity / status:** Moderate; **FAIL**, confirmed CP64.
- **Affected workflows/business impact:** Exports may silently group by project while user sees/model-selects another dimension, yielding misleading reconciliation. Multi-scope total effects remain unverified.
- **Expected / actual:** Export `group_key` and grouping match visible Group by selector or UI clearly states otherwise. Selecting Model showed `llama3-2-qa-local`; JSON export request still used `group_by=project`, and row key was `qa-fresh-project`. Single project means monetary total happened to match; multi-scope aggregation has not been tested.
- **Reproduce:** FinOps → Chargeback, select Model, verify visible row then Export JSON; inspect request query and downloaded `group_key`. Evidence `evidence/phase64-export-selected-model-group.png`, `phase64-export-selected-model-result.png`, `phase64-selected-group-export.json`, downloaded `phase64-export-after-model-group.json`. Detailed `engineering-handoffs/DEF-015-finops-export-ignores-group-by.md`.
- **Facts:** Selector, request and output dimensions disagree. **Hypothesis:** frontend export callback hardcodes `group_by: 'project'`.
- **Acceptance:** Export uses current grouping and other visible filters; output schema/key and UI agree for project/environment/key/model; test fixtures with multiple values per dimension so grouping differences are observable; verify CSV and JSON, filter changes, reload, and stale-state behavior.

## A2A evidence and business-contract reconciliation

The A2A testing established bounded positive behavior as well as the two blockers. CP39 exercised discovery, a synchronous completed task/artifact/SSE path, asynchronous cancellation, hop-depth rejection, and no bearer forwarding using an isolated authorized synthetic endpoint/key. CP41 exercised an `input-required` task and confirmed it cannot resume. The positive key was a narrowly QA-only grant, not a supported user journey; it was revoked. DEF-010 prevents normal provisioning; DEF-011 prevents multi-turn continuation. These are separate layers and both must be fixed/tested without weakening denial for unauthorized keys. See `evidence/phase39-a2a-lifecycle.json`, `phase41-a2a-input-required.json`, cleanup `phase41-a2a-fixture-cleanup.json`, and the two standalone A2A handoffs.

## Historical coverage and unresolved results

The historical phase evidence, checkpoint records, screenshots, prior reports, and handoffs are retained as QA artifacts in this branch. Do not flatten earlier status into a single pass/fail:

- **PASS, limited to tested paths:** CP38 fresh Setup journey; synthetic local provider connection/probe/model/deployment/route/key and chat/trace/FinOps journey; CP39 bounded A2A discovery/completion/cancellation/hop-depth and no-bearer-forwarding; specific role denial and one cross-tenant read denial; selected backup create/verify/restore/recovered login after audit activity; JSON chargeback export; selected component/browser rendering and route accessibility/layout sweeps.
- **FAIL:** all 15 findings as qualified above. Several findings were rechecked at different checkpoints; the latest evidence and limits are stated per item. “All 15 open” is not a claim that each was freshly re-run at CP65.
- **BLOCKED:** Google provider application-level probing/inference is not established. 8080/8082 `upstream_unreachable` aligns with guarded direct egress failing while Cloud proxy access works; 8081 `probe_runtime_unavailable` is isolated to one runtime/listener and its precise cause is unresolved. Host WebKit launch also lacks dependencies, while official-container CP65 could not preserve authentication to the target page. These are environment/test-path blockers unless separate product evidence establishes otherwise.
- **UNVERIFIED:** DEF-001 test/live state discrepancy; DEF-009 target drawer on CP49/65. Full A2A multi-turn input lifecycle is FAIL for the missing transition path but remaining success/error/authorization lifecycle permutations remain unverified.
- **Coverage remains partial or absent:** other roles/write boundaries, comprehensive tenant isolation, full authorization/policy matrix, full budgets/reservations/enforcement/concurrency, provider failover and real integrations, complete MCP discovery/tool grants/calls, A2A authorization matrices and multi-turn recovery, key expiry/rotation/quota, audit tamper detection and full event matrix, backup retention/restore failure modes, deletion/retention, complete API/CLI, alternate-browser per-feature workflows, performance percentiles, all locales/platforms, operational upgrade and disaster recovery. Consult the full matrix for subsystem-level records.

## Regression and completion criteria for engineering follow-up

For every defect: retain a sanitized before/after request, response and state record; visually inspect resulting UI states where applicable; run the exact reproduction plus neighboring success and failure cases; verify state after refresh and on a second relevant screen/API; check authorization and tenant scope; add regression coverage; update the defect register and coverage matrix with **PASS/FAIL/BLOCKED/UNVERIFIED** and evidence paths. Do not close based only on a unit test, DOM assertion, successful HTTP response, or source inspection.

Cross-cutting regression focus:

- Provider: probe freshness, activation gates, health summaries vs actual data-plane eligibility, recovery/reprobe, mixed healthy/unhealthy routes, and external-provider infrastructure separation.
- Tenancy/audit: actor attribution and atomic events for org/project/environment lifecycle, errors/rollback, audit-chain verification, backups/restores, roles, and isolation.
- A2A: default denial; supported least-privilege grants; discovery, task creation, input-required resume, completion, cancellation, timeout, SSE/poll agreement, artifacts, budget settlement/release, key revoke/expiry and tenant boundaries.
- FinOps: settled/estimated/unmetered totals, no-budget and budgeted tenants, all grouping dimensions with multiple entities, export filters, CSV/JSON payload fidelity, stale selections and refresh.
- UX/platform: empty/loading/error/success states, keyboard/screen reader names, 375px layouts, real supported browser sessions, shared forms/components, and truthful action availability.
- Operational: backup availability and restore, CLI command routing/exit status, startup/runtime configuration, recovery and observability.

## Artifact and publication note

This handoff intentionally includes QA documentation and sanitized test evidence only. It excludes Tanpopo source changes, releases, credentials, secrets, database files, and sensitive runtime logs. The detailed source-of-truth documents and phase evidence remain alongside this handoff so historical uncertainty and context are reviewable. `HANDOFF-PACKAGE.md` is the package index. GATE-REL-07 remains BLOCKED.
