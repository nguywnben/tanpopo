# QA-002 full-product coverage matrix

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`. This is a new regression campaign. QA-001 coverage is reference only and does not count as QA-002 PASS.

Status definitions: **PASS** = the named user journey and observable postconditions succeeded; **FAIL** = behavior contradicted the expected contract; **BLOCKED** = a prerequisite prevented the test; **UNVERIFIED** = not yet executed or inconclusive. Each PASS must link to current evidence.

## Product features FT-01..16

| ID | Coverage scope | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| FT-01 | Unified inference, multimodal, streaming, cancellation, errors/recovery | UNVERIFIED | |
| FT-02 | Native dialect passthrough and lossless fields | UNVERIFIED | |
| FT-03 | Adapter → connection → model → deployment lifecycle | PARTIAL PASS | CP10 healthy Ollama adapter→connection→model→deployment→inference path passed with synthetic fixture; CP12 failed probe remained Unverified, activation was rejected, and dependent route stayed Provisioned/fail-closed. Alternate adapters, auth, upgrade and failover remain UNVERIFIED. |
| FT-04 | Secret vault, authorized OAuth, federated identity | UNVERIFIED | |
| FT-05 | Extension isolation and blind credential broker | UNVERIFIED | |
| FT-06 | Registry, package install, lockfile, contract harness | UNVERIFIED | |
| FT-07 | MCP discovery, grants, schema pinning, approvals | PARTIAL PASS | CP09 real UI registration rejects wildcard and persists explicit private CIDR; no valid MCP tool sync/grant/invocation fixture yet. |
| FT-08 | A2A discovery, authorization, delegation, tasks/lifecycle | PARTIAL PASS | CP08 authorized discovery, task submit/input-required/resume/complete/cancel, terminal transition rejection, and same-ID persistence against isolated synthetic endpoint. CP15 graceful key revocation during a deliberately paused A2A request caused HTTP 400 `context canceled`, a persisted Failed task, and RELEASED_UNUSED hold; contract does not define this non-stream task interaction. Task drain semantics, budgets, delegation depth, other roles remain UNVERIFIED. |
| FT-09 | Org/project/environment, six-role RBAC, key lifecycle | PARTIAL PASS | CP06 provisioning audit; CP08 A2A-only/provider-only issuance, authorization, persistence and revocation; CP14 actual project-scoped Team Admin membership and escalation/cross-project denial. Member list refresh defect DEF-016 remains open; other roles/tenant boundaries remain UNVERIFIED. |
| FT-10 | Capability routing, health, failover, route explain | PARTIAL PASS | CP11 single-target synthetic route served; CP12 route to unverified connection remained Provisioned and failed closed. Multi-target failover, weights, policy explain, health recovery remain UNVERIFIED. |
| FT-11 | Runtime policy, rate limit, SSRF/security boundaries | UNVERIFIED | |
| FT-12 | Audit chain and emergency kill-switch | PARTIAL PASS | CP06 create/duplicate paths; CP15 A2A/key audit rows and hash verification (122 events); CP16 synthetic project/environment create and project crypto-shred rows with correct actor/target and hash verification (127 events); CP18 standalone organization create in its own partition with actor/target and one-event chain verification. Emergency kill-switch, tamper detection, injected storage faults and broader mutation/error paths remain UNVERIFIED. |
| FT-13 | Budgets, reservations, settlement and chargeback | PARTIAL PASS | CP05 created first project budget from empty Console state; CP11 settled one exact provider request without any budget ledger and reconciled the FinOps headline, project/model chargeback rows, CSV and JSON exports. Enforcement, estimates, multi-budget scopes and larger reconciliations remain UNVERIFIED. |
| FT-14 | OTel export, trace privacy and explorer | UNVERIFIED | |
| FT-15 | Console, CLI, Admin API, Data Plane API | PARTIAL PASS | CP04 built CLI config show/help/reconcile command routing with local synthetic API fixture; real Admin/Data Plane API paths remain UNVERIFIED. |
| FT-16 | Setup/startup, GitOps, encrypted backup, restore, upgrade | PARTIAL PASS | CP13 Console SQLite backup, archive authentication, secret-decryption-checked restore, restored server startup and fresh login with persisted model/FinOps state. GitOps, PostgreSQL/cluster restore, upgrade and corrupt archive recovery remain UNVERIFIED. |

**CP00–02 test capability note:** Local Go 1.26.11 linux/amd64 binary is built from the exact baseline. `go test ./...` and `go test -race ./...` both pass all 86 packages after applying environment-specific `GOFLAGS=-buildvcs=false` and `umask 0022`; `go run ./scripts/ci/check.go --all`, `go vet ./...`, `go run ./scripts/docs/check_drift.go`, Console typecheck, all 45 Console tests, and production build pass. These are build/automated quality results, not product-feature PASS results. RC4 DevBox artifact remains UNVERIFIED.

## Canonical screens SCR-01..16

| ID | Screen | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| SCR-01 | First-run setup/login | PARTIAL PASS | CP01 login, CP03 complete bootstrap, and CP17 fresh owner/org/project bootstrap plus separate-session persistence; other states/locales/browsers remain open. |
| SCR-02 | Overview/system posture | PARTIAL PASS | CP17 empty fresh Overview visually showed one 503 request at 100% error, zero spend/holds and zero alerts with “All systems nominal”; see QA-OBS-003. Health alert interpretation remains unresolved. |
| SCR-03 | Provider adapters | PARTIAL PASS | CP10 Ollama catalog review and activation; other adapters, upgrade, rollback and permission paths remain UNVERIFIED. |
| SCR-04 | Connections/credentials | PARTIAL PASS | CP10 Ollama defaults, connection persistence and successful live synthetic probe; secret binding and other auth methods remain UNVERIFIED. |
| SCR-05 | Models/deployments | UNVERIFIED | |
| SCR-06 | Extension marketplace | UNVERIFIED | |
| SCR-07 | Installed plugins/lockfile | UNVERIFIED | |
| SCR-08 | Capability routes/route explain | UNVERIFIED | |
| SCR-09 | Policies/guardrails | UNVERIFIED | |
| SCR-10 | MCP/A2A registry and tasks | PARTIAL PASS | CP08 visually inspected actual agent registration, A2A-only key grants, input-required tasks and completion; CP09 MCP server registration/persistence visually inspected. MCP tool sync/invocation and error recovery remain UNVERIFIED. CP15 screenshot shows a Working task at pause and a Failed row after graceful key revocation; whether that violates drain semantics is UNVERIFIED because documented contract covers active streams. |
| SCR-11 | Request traces | PARTIAL PASS | CP17 Playground 503 appeared as a persisted metadata-only trace with `no_compliant_deployment_available`; prompt/response content was not shown in the trace. Additional query, trace privacy and error-class coverage remain open. |
| SCR-12 | FinOps budgets/chargeback | PARTIAL PASS | CP05 visually reviewed empty budgets, create drawer and persisted budget row; CP11 visually reviewed no-budget chargeback headline/table and CSV export success; export contents matched selected Model grouping. Other dimensions and budget enforcement remain UNVERIFIED. |
| SCR-13 | Keys, projects, members, roles | PARTIAL PASS | CP06 project/environment provisioning; CP08 scoped key creation, grant visibility and revocation; CP14 Team Admin Add member and scoped grant. Team Admin list refresh is blocked by DEF-016; rotation and other role boundaries remain open. |
| SCR-14 | Approvals/audit/kill-switch | PARTIAL PASS | CP06/16/18 live audit-log screens visually reviewed; create, project crypto-shred and A2A/key event attribution and scoped chain verification passed. CP18 verified new standalone organization partition and rejected duplicate slug. Approvals, kill-switch and fault-injection remain UNVERIFIED. |
| SCR-15 | Playground/API explorer/diagnostics | PARTIAL PASS | CP17 no-model empty Playground disabled Send until input; synthetic missing-model request surfaced 503 and a trace, with $0 settled spend and no hold. Recovery, successful streaming, cancellation and SDK snippets remain open. |
| SCR-16 | Operations/backups/upgrades/privacy | PARTIAL PASS | CP07 authenticated WebKit webhook drawer; CP13 Console backup, receipt, archive verify/restore and restored startup/login. Upgrade, health matrix, privacy and clustered operations remain UNVERIFIED. |

## Business workflows WF-01..13

| ID | Workflow | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| WF-01 | First install/system bootstrap | PARTIAL PASS | CP17 clean SQLite runtime completed owner/org/project bootstrap without provider, then a separate browser session logged in and saw persisted workspace state. Other environments, retries and locale/browser paths remain open. |
| WF-02 | Day-one setup wizard | PARTIAL PASS | CP17 visually inspected all four wizard steps; empty adapter step explicitly directs operator to Marketplace after setup. Initial provider provisioning, validation failures and staged-adapter setup remain open. |
| WF-03 | Add official provider | UNVERIFIED | |
| WF-04 | Install community/external extension | UNVERIFIED | |
| WF-05 | Key and authorized OAuth connection | UNVERIFIED | |
| WF-06 | Unified/native API, stream, timeout, retry, cancel | UNVERIFIED | |
| WF-07 | Capability routing and provider failover | UNVERIFIED | |
| WF-08 | Quota/budget/chargeback without exact usage | PARTIAL PASS | CP11 exact settlement/chargeback/export verified with zero budget ledgers, and CP05 created first project budget. Quota consumption, overage, estimated/unmetered settlement and ledger holds remain UNVERIFIED. |
| WF-09 | MCP tool invocation, side-effect guard, approval | UNVERIFIED | |
| WF-10 | Key rotation and emergency revocation | PARTIAL PASS | CP08 A2A key revoked via Console; UI reported 0 active streams severed and old credential was denied with 401. CP15 revoked during a paused A2A dispatch; request returned 400 `context canceled`, task persisted Failed, and hold released. Whether graceful drain covers this non-stream workflow is UNVERIFIED. Rotation/grace and active SSE revocation remain UNVERIFIED. |
| WF-11 | Extension update, escalation block, rollback | UNVERIFIED | |
| WF-12 | Incident detection, containment, recovery | UNVERIFIED | |
| WF-13 | Data retention, anonymization, deletion | UNVERIFIED | |

## Cross-cutting coverage

| Dimension | QA-002 status | Evidence / open coverage |
| --- | --- | --- |
| A2A dispatch/revocation contract: admitted task recovery, side effects and reservation settlement | UNVERIFIED | CP15 confirmed admitted task cancellation on graceful key revoke; contract gap prevents a pass/fail classification. |
| User roles: first-time owner, developer, administrator, Team Admin, security reviewer, operator, FinOps, extension author | PARTIAL PASS | First-time owner setup/login; CP14 actual Developer+Team Admin login, successful scoped member create, org-role escalation and cross-project denial. Security Reviewer, FinOps Viewer, non-owner Operator and extension author roles remain UNVERIFIED. |
| Tenant and resource authorization; default-deny and cross-tenant boundaries | PARTIAL PASS | CP08 provider-only key was denied A2A discovery/task with 403; revoked key received 401. Cross-tenant and all-resource boundaries remain UNVERIFIED. |
| Provider integrations: Google, Meta, deterministic synthetic provider, adapter failures | PARTIAL PASS | CP10/11 Ollama synthetic success and exact settlement; CP12 isolated HTTP 401 probe and fail-closed route; CP17 actual Google adapter activation and connection probe remained Unverified with `upstream_unreachable`, while same configured credential succeeded through the proxy for one current Google model and Google rejected two old model IDs. Cloud proxy-only egress is separately evidenced; Tanpopo Google inference remains BLOCKED. Meta and more adapters remain open. |
| Platforms/modes: standalone SQLite, Docker, PostgreSQL/Valkey clustered, restart/upgrade | UNVERIFIED | |
| Browsers: Chromium, Firefox, WebKit; real interaction/screenshots | PARTIAL PASS | CP01–06 real Chromium flows/screenshots; CP07 real WebKit 26.5 at 375px. Firefox workflow still UNVERIFIED. |
| Responsive: 320/375/768/1024/1440/1920; loading/empty/error/success states | PARTIAL PASS | CP07 WebKit webhook drawer 375×844; CP03/05 Chromium 1440×1000. Other widths and states UNVERIFIED. |
| Themes: light/dark; 15 locales | UNVERIFIED | |
| Accessibility, keyboard, screen-reader labels, focus/recovery | PARTIAL PASS | CP03 live axe: four Setup steps, 0 violations; CP19 two original Setup accessibility tests pass on the post-engineering baseline. Other screens, keyboard/screen-reader, locales, and browsers remain UNVERIFIED. |
| Security: SSRF, secrets, audit integrity, kill-switch, plugin boundaries | PARTIAL PASS | CP18 standalone organization create was attributed to the acting principal and its per-tenant JCS chain verified intact; duplicate slug had no second row. SSRF, tamper/fault injection, kill-switch and broader plugin boundaries remain UNVERIFIED. |
| Operational reliability: health, backup/restore, restart persistence, diagnostics | PARTIAL PASS | CP13 verified SQLite backup, encrypted restore and fresh-process health/readiness/login/persistence. Restart failure, multi-node, PostgreSQL/Valkey and upgrade recovery remain UNVERIFIED. |

## Update protocol

After each phase, update only rows supported by phase evidence. Keep partial results and residual scenarios explicit; a broad row can remain UNVERIFIED while specific subpaths pass or fail. Record every newly discovered defect with the next stable identifier beginning `DEF-017` in `reports/` and add the same evidence link to a checkpoint.
