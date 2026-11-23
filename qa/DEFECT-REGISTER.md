# Defect Register

Append-only discoveries and retests. Baseline `a771311107c4e4c2c7d9b53a3f5a0926f114b799`.

## DEF-001 — Setup step button accessible name omits visible text

- Severity: Major accessibility defect (test impact serious); affected users relying on speech input may not be able to activate by the visible step label.
- Expected: button accessible name contains its visible text.
- Observed: `npm test` axe rule `label-content-name-mismatch` reports a button with `aria-label="Go to step 1: Owner ..."` whose visible text is not included in the accessible name.
- Reproduction: from `web/console`, run `npm test`; failures occur in `src/pages/console.test.tsx` cases “hides unavailable presets…” and “offers only adapters…”.
- Evidence: sanitized Vitest output checkpoint in this report; both failures share the same axe rule and setup progress step node. Screenshot unavailable because no browser tool/runtime.
- Root-cause hypothesis: accessible label generated for a Setup progress step does not include the rendered visible label.
- Regression risk: all Setup wizard steps and other uses of the same progress component may be affected; runtime/browser validation still needed.
- Retest history: 2026-11-23 initial run, reproducible in two tests; 38/40 pass. No code changed.


### DEF-001 retest note — 2026-11-23

A full Chromium harness run completed setup and then scanned 61 authenticated route/theme/viewport samples; it reported no blocking axe violations in those samples. This does not close the two existing Vitest failures: they are still preserved as the canonical reproductions. The one-off scan summary is `evidence/phase4-chromium-e2e.json`.


## DEF-002 — Unverified provider can be labeled healthy while the gateway rejects it

- Severity: Major — an operator can receive a green deployment/route health indication and successful activation notice while requests for that route are unavailable.
- Expected: A deployment backed by a connection that remains `Created Unverified` after failed probes cannot be promoted to `ACTIVE_HEALTHY`, or the UI clearly labels it unverified and does not present the route target as Healthy.
- Observed: After two Google connection probes returned `upstream_unreachable`, the owner created a deployment (`PROVISIONED`) and chose Activate deployment. The UI reported `Deployment activated cleanly as ACTIVE_HEALTHY`; Models displayed `Healthy`, and a route targeting it displayed `Healthy`. A one-token `POST /v1/chat/completions` request to that route returned HTTP 503 `no_compliant_deployment_available`. The resolver failed closed before external provider egress; no successful Google inference occurred.
- Reproduction: On an isolated SQLite instance, create a Gemini connection with a valid sealed key; have its probe fail with `upstream_unreachable`; register a model; add a deployment with a nonzero test-only rate card; click `Activate deployment`; create a route to the deployment; issue an environment virtual key; request that route once. Compare provider state, deployment/route state, and response.
- Evidence: `evidence/phase5-unverified-connection-health-mismatch.json`; visual states `providers-live-probe-post-activation.png`, `models-unverified-activation-marked-healthy.png`, and `routes-live-provider-created.png`. The virtual key was shown only once in the browser, not captured, then revoked (`access-live-probe-key-revoked.png`). The test route and deployment were deleted after evidence capture.
- Root-cause hypothesis: The admin activation API accepts `ACTIVE_HEALTHY` without checking connection health/probe evidence; the console maps that declared deployment state directly to a green “Healthy” label. The data-plane resolver independently excludes the unverified connection.
- Regression risk: Operators may assume a newly configured commercial model is serving when all requests will fail; dashboards and route selection can misstate readiness.
- Retest history: Initial reproduction 2026-11-23. No product code changed; retest pending.


## DEF-003 — FinOps Console cannot create the first budget

- Severity: Moderate — the console cannot complete the first budget-configuration workflow; an administrator must use the Admin API or another out-of-band path before the UI can manage a ceiling.
- Expected: The `SCR-12` FinOps screen lets authorized `ORG_OWNER` / `TEAM_ADMIN` users create an initial organization, project, environment or key budget from the empty state.
- Observed: With no budget records, `/finops/budgets` showed “No budgets configured” and had only Export CSV/JSON controls. There was no Add/Create budget action. Source path exposes `listBudgets` and `updateBudget` only. A canonical budget created through `PUT /admin/v1/budgets/{budgetId}` returned 200; then the owner could view and edit its hard limit from the Console. Negative input `-1` showed a non-negative integer validation message and sent no PUT.
- Reproduction: Sign in as Org Owner to an isolated organization with no budget. Open FinOps → Budgets & Holds. Observe no create affordance. Create a test organization budget through the Admin API; switch workspace to No project and observe it; update its ceiling in the UI.
- Evidence: `evidence/phase6-finops-budget-journey.json`; screenshots `finops-budgets-empty-1440.png`, `finops-budget-org-scope.png`, `finops-budget-edited-ui.png`, `finops-budget-negative-validation.png`.
- Root-cause hypothesis: The FinOps Console exposes only list and update operations and does not surface the backend's documented budget-creation path.
- Regression risk: Operators may not establish the first spend ceiling using the primary UI, delaying or preventing a governance control; an API workaround exists.
- Retest history: Initial reproduction 2026-11-23. No product code changed; retest pending.


## DEF-004 — CLI configuration inspection is shadowed by a duplicate command group

- Severity: Moderate — operators cannot use the advertised CLI command to inspect the live server configuration; the CLI returns local reconciliation help with a success exit code instead.
- Expected: `tanpopo config show` reaches the Admin API configuration summary command, while `tanpopo config reconcile` remains available for local declarative configuration.
- Observed: Root help lists two top-level `config` commands, one for local validation/reconciliation and one for server configuration inspection. `tanpopo config show --output json` exits 0 but prints only the local `config` help, whose sole subcommand is `reconcile`. It produces no JSON configuration output. The duplicate command groups are independently registered by `internal/cli/ops` and `internal/cli/remote`.
- Reproduction: Run `/tmp/qa-tools/tanpopo --help`, then `/tmp/qa-tools/tanpopo config show --output json`; compare the successful exit status with the help-only output. Run `/tmp/qa-tools/tanpopo config reconcile --help` to confirm the reachable group is the local config group.
- Evidence: `evidence/phase9-cli-config-command-collision.json` records command arguments, exit codes, output classification, and the source registration locations. No server state was changed.
- Root-cause hypothesis: Cobra receives two root children with the same command name; the local `config` group handles the name first and shadows the remote group’s `show` child.
- Regression risk: Operators receive a successful CLI exit while getting no server configuration data, which can mask diagnostics in automation.
- Retest history: Reproduced 2026-11-23 against the isolated QA binary. No product code changed.


## DEF-005 — MCP and A2A Console forms cannot register private endpoints

- Severity: Moderate — the Console's explicit “Allow private or loopback endpoint” path cannot complete for either MCP servers or A2A agents, so trusted internal integrations require an out-of-band API call.
- Expected: Enabling the private endpoint option submits a narrowly scoped allowlist (or provides a CIDR field) so the server can validate and register the trusted endpoint.
- Observed: The MCP form submits `allow_private_ip: true` without `allowed_cidrs`; its API responds HTTP 400 `allowed_cidrs is required when allow_private_ip is true`. The A2A form sends the same incomplete contract; its API responds HTTP 400 `invalid agent configuration: allowed_cidrs is required when allow_private_ip is true`. Neither record was created. The endpoint was a local test URL and no request was sent to it.
- Reproduction: In an isolated Org Owner session, open MCP Hub → Register server, enter `http://127.0.0.1:8080/mcp`, enable “Allow private or loopback endpoint”, and submit. Then open A2A Agents & Active Tasks → Register agent, enter `http://127.0.0.1:8080/agent`, enable the same option, and submit. Both are rejected before any endpoint fetch.
- Evidence: `evidence/phase10-mcp-private-endpoint-validation.png` and `evidence/phase10-a2a-private-endpoint-rejected.png`; interaction and response summary in `evidence/phase10-private-integration-endpoint-journeys.json`.
- Root-cause hypothesis: Both frontend registration payloads omit the `allowed_cidrs` field required by their corresponding backend handlers/registry whenever private access is enabled.
- Regression risk: Operators cannot configure explicitly trusted internal MCP/A2A services through the primary Console flow; relying on broader backend defaults would weaken the SSRF boundary, so a properly scoped CIDR control is needed.
- Retest history: Reproduced 2026-11-23 on the isolated QA server. No product code changed; no record or outbound endpoint call was created.


## DEF-006 — Team Admin member form cannot create the project-scoped member it advertises

- Severity: Moderate — the Access & Keys page exposes member management controls to Team Admin, but its form cannot submit the project scope the backend requires, so the advertised workflow always fails for this role.
- Expected: A Team Admin permitted to manage project-scoped principals can select a project scope when adding or editing a member, or the Console hides/disables the action when no supported scope can be submitted.
- Observed: A non-operator `DEVELOPER + TEAM_ADMIN` session displayed an enabled “Add member” action and a drawer titled “Add organization member”. The form accepts only email and roles; it has no project selector. Submitting a Developer member returned HTTP 403 `role_escalation_denied` with `project-scoped admin must specify project_ids and cannot grant organization-scoped bindings`. No principal was created. Backend authorization correctly rejected the incomplete request.
- Reproduction: Sign in as a Team Admin in an isolated project context. Open Access & Keys → Members & Roles → Add member; enter any QA-only email with the default Developer role and submit. Observe the 403 message and unchanged member count.
- Evidence: `evidence/phase15-team-admin-add-member-form.png`, `evidence/phase15-team-admin-add-member-rejected.png`, and `evidence/phase15-team-admin-add-member-journey.json`.
- Root-cause hypothesis: `AccessPage` allows Team Admin under `canManage` and calls `createMember(email, roles)` without `project_ids`; server-side RBAC requires Team Admin grants to include project IDs and rejects organization-scoped bindings.
- Regression risk: project administrators cannot onboard or adjust scoped teammates through the UI despite being shown active controls, while operators may misinterpret the form as an organization-wide invitation flow.
- Retest history: Reproduced 2026-11-23 with a non-operator Team Admin on the isolated QA runtime. No product code changed; request was rejected and no data was added.


## DEF-007 — Runtime tenancy mutations are missing from the audit chain

- Severity: Major — the canonical runtime creates organizations, projects and environments without corresponding audit events, so critical tenant provisioning actions cannot be attributed or reviewed from the audit history.
- Expected: Successful organization/project/environment mutations append their corresponding events to the tenant audit chain in the same transaction or through an explicitly fail-closed audit path.
- Observed: In the isolated runtime, Operations created `qa-boundary-org` and the Console created project `qa-isolation-20261123` plus environment `qa-sandbox` (all HTTP 201). Read-only SQLite inspection found 0 audit events for the new organization, 0 `project.created` events for the project ID, and 0 `environment.created` events for the environment ID. The existing default organization has 167 events. A CLI encrypted backup was created, but `tanpopo backup verify` exited 14: `initialized organization ... has no verifiable audit history`.
- Reproduction: Using the canonical `tanpopo serve` runtime with its SQLite database, create an organization through Operations, then a project/environment through the scope switcher. Query the isolated `audit_events` table by new org/resource IDs; no matching events exist. Create/verify a CLI backup while the empty-audit organization remains present to reproduce the verification failure.
- Evidence: `evidence/phase16-audit-and-backup-verification.json` records sanitized event counts and backup result. The org/project/environment creation visual/API evidence is in `phase11-workspace-provisioning.json` and `phase12-organization-scope-journey.json`.
- Root-cause hypothesis: `cmd/tanpopo/serve.go` opens `sqlite.Store` without a transactional audit sink; it creates `auditLog` afterward and passes that logger to `adminhttp.NewServer`, but not to the store's tenancy repository. The create handlers call the repository and do not append a fallback audit event. Storage-level tests cover event emission only when a `TxAuditSink` is injected.
- Regression risk: Tenant creation and scope provisioning are unauditable; an organization with no verifiable audit history makes encrypted backup verification and restore fail. Later audit events may make backup verification possible, but they do not retroactively attribute org/project/environment creation.
- Retest history: Reproduced 2026-11-23 in the isolated canonical runtime. At checkpoint 21, adding a QA-only owner and authenticating created `member.created` and `auth.login_succeeded` entries; a new backup then verified and restored successfully to a separate directory and the restored app started/logged in. This confirms the recovery failure is gated on the empty audit chain; the missing provisioning audit records remain. No product code changed.


## DEF-008 — Console backup action cannot reach a configured backup runtime

- Severity: Moderate — the enabled Console action always returns 503 in the canonical unified binary, leaving CLI backup as the only usable creation path.
- Expected: The serving process supplies the configured `tanpopo.yaml` path to the Admin API, and Console backup creation succeeds when the lockfile and artifact store are present; otherwise the action is clearly unavailable before submission.
- Observed: Operations → Backups showed an enabled “Create encrypted backup” control and no backup receipts. POST `/admin/v1/ops/backups` returned 503 `backup requires configured tanpopo.lock, tanpopo.yaml, and extension artifact store`. The isolated lockfile and plugin artifact directory exist, and a valid QA `tanpopo.yaml` was created. Source inspection confirms the unified `cmd/tanpopo/serve.go` `adminhttp.ServerConfig` construction omits `ConfigPath`; `NewServer` stores the empty value and the handler rejects it. The UI remains available/enabled.
- Reproduction: Open Operations → Backups as Operator, click Create encrypted backup, and observe HTTP 503. The CLI workaround `tanpopo backup create` succeeded with explicit `--config`, `--lockfile`, and `--artifact-dir` paths.
- Evidence: `evidence/phase16-backup-console-failure.png`, `evidence/phase16-backup-config-file-still-503.png`, and `evidence/phase16-audit-and-backup-verification.json`.
- Root-cause hypothesis: `ServerConfig.ConfigPath` is never set by the canonical serve command, so environment files cannot satisfy the API handler's non-empty path check.
- Regression risk: Operators relying on Console backups receive a visible failure despite an enabled control; CLI access is required.
- Retest history: Reproduced 2026-11-23 before and after adding a valid config file to isolated test data; the runtime still returned HTTP 503. No product code changed.


## DEF-009 — WebKit mobile webhook registration drawer is positioned offscreen

- Severity: Moderate — at a 375px WebKit viewport, operators cannot see or use the webhook registration form, blocking the workflow in that browser/viewport combination.
- Expected: Opening Add webhook positions its heading, receiver URL, signing-credential selector, events, and actions within the current viewport or provides a visible way to bring them into view.
- Observed: At 375×844 in WebKit 26.5, clicking Add webhook leaves the drawer's heading and fields in the DOM but lays the heading at x=392px, 17px beyond the viewport's right edge. The screenshot still shows the webhook empty state; document `scrollWidth` equals the 375px viewport, so horizontal scrolling does not reveal the form. The same interaction visibly opens the drawer in Chromium and Firefox at 375px. No API errors were observed.
- Reproduction: At 375×844 using WebKit, sign in as an isolated Org Owner, open Operations → Webhooks, and click Add webhook. The accessible text tree contains “Register webhook subscription,” but its bounding box starts at x=392; capture a viewport screenshot and compare with Chromium/Firefox at the same size.
- Evidence: `evidence/phase18-webkit-drawer-layout.json`; comparative screenshots `phase18-webkit-webhooks-375.png`, `phase18-chromium-webhooks-375.png`, and `phase18-firefox-webhooks-375.png`; cross-engine interaction/API record `phase18-ops-cross-engine-mobile.json`.
- Root-cause hypothesis: The shared drawer positioning or transform calculation places the panel beyond the viewport in WebKit at this mobile width. This is observed behavior; implementation-level cause is unconfirmed.
- Regression risk: WebKit users on narrow screens cannot configure signed webhook subscriptions even though the form content exists and the empty state advertises the action.
- Retest history: Reproduced in Playwright WebKit 26.5 at 375×844 on 2026-11-23; no product code changed. Retest pending.

## CP27 regression status (2026-11-23)

- DEF-001 and DEF-002 remain open; no fresh retest was performed in CP27.
- DEF-003, DEF-004, DEF-005 (both MCP and A2A), DEF-006 and DEF-008 reproduced in CP27. See `evidence/phase27-defect-regression-summary.json` and linked interaction evidence. DEF-005 also has the targeted handoff under `engineering-handoffs/`.
- DEF-007 remains open on the prior tenant-creation and audit-chain evidence; no new database audit mutation was performed in CP27.
- DEF-009 did not reproduce in CP27's WebKit 26.5 375×844 interaction: the drawer heading remained within the viewport (x=73) at 0, 100, 250, 500 and 1000ms. The earlier CP18 screenshot/geometry remains preserved; keep the defect open until the CP18 and CP27 harness/interaction discrepancy is explained.

The original nine findings are preserved; DEF-010 through DEF-015 extend the register. All fifteen findings remain open. GATE-REL-07 remains BLOCKED.

- **DEF-001 CP31 retest:** The first Setup step was opened in a fresh disposable runtime and visually inspected in Chromium. The step buttons' accessible names contained the visible step wording; a WCAG axe scan reported zero violations. DEF-001 did not reproduce in that live state. The two original Vitest failures remain unreconciled, so keep the finding open pending exact test-state comparison.

## DEF-010 — Supported Virtual Key issuance cannot grant A2A access

- Severity: Major — with the A2A data plane enabled, all Virtual Keys issued through the UI/API are provider-only, so tenant users cannot use A2A discovery or task workflows through supported key provisioning.
- Expected: An authorized issuer can grant the A2A plane and narrowly scoped A2A routes/agents to a key. A default key remains denied.
- Observed: Access & Keys → Issue key exposes model and MCP tool controls but no A2A plane/route control. The documented key create request similarly omits them. On an isolated listener with `TANPOPO_A2A_ENABLED=true`, a key created by this UI received HTTP 403 `scope_or_plane_denied`, `Virtual Key is not permitted on A2A plane.` Read-only SQLite inspection showed its `allowed_planes` was `PROVIDER`. The key was revoked after the probe; its plaintext was not persisted.
- Reproduction: Enable `TANPOPO_A2A_ENABLED=true`, issue a normal key through Access & Keys, then call `GET /a2a/agents` from a cookie-free client using that key's bearer token. Observe 403. Compare the issue form and create request fields; neither offers A2A grant controls.
- Evidence: `evidence/phase32-vkey-issue-form.json` and screenshot, `phase34-a2a-vkey-bearer-probe.json`, `phase34-vkey-a2a-scope-readback.json`, and `phase34-a2a-enabled-agent-list.png`.
- Classification: The original active runtime had the A2A feature flag unset and returned 404 `A2A plane is disabled`, which is configuration. The 403 on the separately enabled test listener is the confirmed product provisioning gap.
- Root-cause hypothesis: `createVirtualKeyRequest`/OpenAPI and the Console form omit `allowed_planes` and A2A route grants; the admin create handler leaves `vkey.CreateParams.AllowedPlanes` empty, which normalizes to `PROVIDER`.
- Regression risk: A2A appears in the product Console and agents can be registered, but no standard user-created key can pass the data-plane A2A authorization check.
- Engineering handoff: `engineering-handoffs/DEF-010-a2a-virtual-key-scope.md`.


## DEF-011 — A2A input-required tasks cannot resume

- Severity: Major — multi-turn delegated workflows that require caller input become permanently nonterminal and cannot complete using the exposed HTTP API.
- Expected: Per SM-09, after an agent returns `input-required`, the caller supplies additional input and the same task transitions back to `working`, then reaches a terminal state. The task response and Console should expose enough information to continue the workflow.
- Actual: An isolated authorized task returned HTTP 201 with `state=input-required` and persisted as `INPUT_REQUIRED`; GET returned that same state. POST `/a2a/tasks/{taskId}/transition` with `{ "state": "working", "input_parts": [...] }` returned HTTP 404 `resource_not_found` / `A2A endpoint not found.` `TaskCoordinator` supports a transition request internally, but the mounted task handler exposes only GET and POST `/cancel` for a task ID. The Console presents an “Input required” row without a resume control or prompt.
- Reproduction: On an A2A-enabled isolated runtime, register an authorized synthetic agent whose `POST <endpoint>/tasks` responds `{"state":"input-required"}`. Submit POST `/a2a/tasks` with a valid target agent, nonempty `input_parts`, and zero test budget. Read the returned task ID; GET `/a2a/tasks/{id}` remains `input-required`. Submit POST `/a2a/tasks/{id}/transition` with state `working` and new input. Observe 404. In the authenticated A2A Console task list the row remains Input required and has no resume action.
- Evidence: `evidence/phase41-a2a-input-required.json`, visually inspected `evidence/phase41-a2a-input-required-console.png`, and CP39 discovery/task baseline `evidence/phase39-a2a-lifecycle.json`. Contract references: `docs/specifications/mcp-a2a-boundaries.md` §3.1, `docs/requirements/srs.md` TP-FR-0405, `docs/planning/units/TP-IMP-0042.md` AC-2, and `docs/requirements/acceptance-criteria.md` TP-AC-F0404.
- Affected workflow: Any external agent that pauses for clarification or approval; caller-controlled multi-turn A2A tasks; task status monitoring and eventual budget/TTL cleanup for paused work.
- Engineering handoff: `engineering-handoffs/DEF-011-a2a-input-required-cannot-resume.md`.
- Product implementation was not modified. Retest on the isolated QA runtime 2026-11-23.

## DEF-012 — FinOps headline spend totals ignore usage settlements when no budgets exist

- Severity: Major — the headline can report zero settled spend while exact provider-reported usage is present in the chargeback table and ledger.
- Expected: Exact/estimated settled and unmetered summary metrics reconcile to actual usage settlements and chargeback rows, whether or not a budget has been configured.
- Actual: On a freshly bootstrapped tenant with no budget ledgers, one successful exact provider call settled 1 micro-USD. FinOps → Chargeback displayed Exact settled `$0.000000` in its headline while the project row showed Exact `$0.000001` and `1 / 0` exact/estimated requests. Read-only DB inspection confirmed one `EXACT` settlement for 1 micro-USD and zero budget-ledger rows.
- Reproduction: On an isolated tenant with no budget, make one successful provider request with exact nonzero usage; open FinOps → Chargeback grouped by project. Compare the headline and group row. Evidence is `evidence/phase47-finops-chargeback.png`, `phase47-finops-chargeback.txt`, `phase46-local-inference.json`, and `phase51-finops-kpi-chargeback-mismatch.json`.
- Expected vs actual source: `FinOpsPage.tsx` currently calculates headline totals from `listBudgets()` data while its chargeback table is populated by the usage-chargeback endpoint. An empty budget collection therefore drives zero headline totals independently of actual usage.
- Affected workflows: first-use tenant spend review, reconciliation, finance/operator monitoring and decisions based on whether traffic incurred cost. The detailed project row remains correct in this repro, but the prominent summary is contradictory.
- Engineering handoff: `engineering-handoffs/DEF-012-finops-headline-uses-budget-ledgers.md`.
- Confirmed on isolated 8083 with local synthetic provider. No external provider was used; no implementation changed.

## DEF-013 — Ollama connection creation defaults to commercial billing

- Severity: Moderate — accepting the preselected connection defaults can persist incorrect billing and region metadata for a local self-hosted provider.
- Expected: Selecting the bundled Ollama adapter should default to `SELF_HOSTED_INFRA` and an appropriate local region, consistent with the official adapter manifest, or require an explicit operator choice.
- Actual: With Ollama selected in Providers → New Connection, the form defaults to `COMMERCIAL_API` and `US`. The bundled `extensions/official/ollama/manifest.yaml` explicitly says Ollama defaults to `SELF_HOSTED_INFRA`. CP43 manually changed the values to `SELF_HOSTED_INFRA` and `ON_PREM` before saving, so no wrongly classified connection was persisted in this test.
- Reproduction/evidence: Activate bundled `@tanpopo/ollama`; open New Connection and observe default Billing mode and Data region before editing. `evidence/phase43-new-connection-form.png`, `phase43-ollama-connection-filled.png`, and `phase43-ollama-probe-result.json` preserve the default and corrected path.
- Affected workflows: first-run local Ollama/vLLM setup, connection provisioning, chargeback classifications and policies driven by billing mode.
- Engineering handoff: `engineering-handoffs/DEF-013-ollama-billing-default.md`.
- Confirmed form/manifest mismatch on isolated 8083. No implementation changed.

## CP48–57 defect reconciliation (2026-11-23)

- DEF-001: CP38 completed the complete fresh Setup wizard in Chromium and visually inspected its validation and progress steps; CP31's live accessible-name/axe scan still showed no violation. The two original Vitest `label-content-name-mismatch` failures have not been reproduced by that runtime evidence and remain unreconciled; keep open.
- DEF-002: Reproduced with a local loopback Ollama-compatible fixture returning HTTP 401. Probe API returned HTTP 200 with `valid=false`, `failure_code=auth_failed`; the connection UI showed Created Unverified. Creating and activating a deployment then produced the UI success “Deployment activated cleanly as ACTIVE_HEALTHY,” and the deployment row showed Healthy. Evidence `phase52-failed-connection.json`, `phase53-unverified-deployment.json`, `phase54-unverified-deployment-activation.json`, `phase54-unverified-deployment-activated.png`, and DB readback `phase54-def002-fresh-repro.json`. This confirms the product health-state contradiction independently of Google or Cloud egress. This fresh retest did not send inference to the failing connection; the historical CP5 HTTP 503/no-compliant-deployment result remains the data-plane evidence. The synthetic deployment/connection were deleted after capture.
- DEF-005: Previously reproduced on both MCP and A2A forms with omitted `allowed_cidrs` and HTTP 400. The separate provider connection form in CP43 did accept `127.0.0.1/32` and successfully probe its private Ollama test endpoint, so current evidence scopes the missing-CIDR problem to MCP/A2A registration surfaces; broader shared-component parity remains unverified. Existing handoff remains intact.
- DEF-007: Reproduced on the fresh bootstrap organization/project/dev environment. Read-only SQLite found no org/project/environment provisioning events while 49 unrelated login, key, provider, model, extension, deployment and route events existed. `TP-IMP-0024` AC-1 requires provisioning mutations to append audit entries; `SCR-08` lists project/environment creation audit events. See `phase48-fresh-bootstrap-audit.json`.
- DEF-009: A fresh Playwright WebKit retest was attempted. Host launch is blocked by missing GTK/WebKit dependencies; the official Playwright container launched, but the WebKit auth/navigation attempt ended back at `/login` (`Loading sign-in…`); the script timed out waiting for `Add webhook` and did not reach or visually inspect the drawer. Diagnostic `evidence/phase49-def009-attempt.json`. Therefore CP49 is UNVERIFIED, not a pass/fail. Keep CP18 reproduction and CP27 non-reproduction evidence side by side; exact harness/interaction discrepancy is still open.
- DEF-010: Preserved as a provisioning/product UX defect. The user confirmed A2A correctly rejects a key without the A2A plane; no authorization boundary was weakened. Historical key issue form/API evidence and standalone handoff remain authoritative. CP39's positive lifecycle used only an isolated synthetic key with an explicit QA-only A2A grant, then revoked it.
- DEF-012 and DEF-013: Confirmed new defects; handoffs stored with mode 600 in the mode-700 handoff directory.
- Test cleanup CP50/56/57: the successful provider Virtual Key is revoked and now returns 401; synthetic failed provider deployment/connection removed; fixture listeners on 19088–19090 stopped. Healthy Ollama connection and first route remain. No Tanpopo implementation changed.

All fifteen findings remain open. GATE-REL-07 remains BLOCKED.

## DEF-014 — Console CSV chargeback export parses the response as JSON

- Severity: Moderate — the primary CSV export workflow fails, preventing spreadsheet/ledger export from the Console.
- Expected: Export CSV downloads the CSV returned by the server; Export JSON downloads JSON.
- Actual: `GET /admin/v1/usage/export?format=csv&group_by=project` returned HTTP 200, `text/csv; charset=utf-8`, and a valid header/data row. The UI attempted to parse the body as JSON, displayed `Unexpected token 'g', "group_key,"... is not valid JSON`, and no download occurred. Export JSON succeeded and downloaded a row with `exact_micro_usd: 1`.
- Reproduction: As Org Owner, open FinOps → Chargeback and click Export CSV; compare with Export JSON. Evidence `evidence/phase61-export-csv-ui.png`, `phase61-chargeback-export-csv.json`, `phase62-export-json.json` and visually reviewed `phase62-export-json-ui.png`.
- Affected workflow: operator/finance user CSV export to spreadsheet, accounting, or external reconciliation tools.
- Likely cause: frontend `exportUsageChargeback` uses a JSON-parsing generated API client for both CSV and JSON formats.
- Engineering handoff: `engineering-handoffs/DEF-014-finops-csv-export.md`.
- Confirmed 2026-11-23 with retained synthetic local usage data. No implementation changed.

- **DEF-001 CP63 focused retest:** Both original Setup Vitest cases still fail `label-content-name-mismatch` on the step 1 progress button. Fresh output is in `evidence/phase63-def001-targeted-vitest.txt` and `.json`. CP31 live Chromium axe found no violation after authentication, leaving the runtime/test-state discrepancy unresolved.

## DEF-015 — FinOps exports ignore the selected group-by dimension

- Severity: Moderate — CSV/JSON report dimensions can disagree with the visible grouping and mislead downstream reconciliation.
- Expected: Exported `group_key` matches the current Group by selector, or the UI labels project-only export behavior.
- Actual: In the UI, Group by Model displayed `llama3-2-qa-local`; clicking Export JSON produced a successful download whose row had `group_key: qa-fresh-project`. Captured request URL was `/admin/v1/usage/export?format=json&group_by=project`.
- Reproduction/evidence: `evidence/phase64-export-selected-model-group.png`, `phase64-export-selected-model-result.png`, `phase64-selected-group-export.json` and downloaded file `phase64-export-after-model-group.json`.
- Affected workflows: model-, environment- and key-level cost exports and any later reconciliation based on the selected table dimension. One-project fixture means total value was unchanged; wrong grouping key was directly verified. Multi-scope aggregation impact remains untested.
- Likely cause: frontend `exportUsageChargeback` hard-codes `group_by: 'project'` rather than using the current UI `groupBy` state.
- Engineering handoff: `engineering-handoffs/DEF-015-finops-export-ignores-group-by.md`.
- Confirmed on isolated fresh-install UI. No implementation changed.

- **DEF-009 CP65 retest boundary:** Official-container WebKit rendered the 375×844 sign-in screen but returned to the empty login form before Operations → Webhooks. Visually inspected `evidence/phase65-webkit-after-login.png`; structured boundary `phase65-def009-webkit-login-attempt.json`. No drawer layout result is inferred.
