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
| FT-08 | A2A discovery, authorization, delegation, tasks/lifecycle | PARTIAL PASS | CP08 authorized discovery, task submit/input-required/resume/complete/cancel, terminal transition rejection, and same-ID persistence against isolated synthetic endpoint; task draining, budgets, delegation depth, other roles remain UNVERIFIED. |
| FT-09 | Org/project/environment, six-role RBAC, key lifecycle | PARTIAL PASS | CP06 provisioning audit; CP08 A2A-only/provider-only issuance, authorization, persistence and revocation; rotation, other role/tenant boundaries remain UNVERIFIED. |
| FT-10 | Capability routing, health, failover, route explain | PARTIAL PASS | CP11 single-target synthetic route served; CP12 route to unverified connection remained Provisioned and failed closed. Multi-target failover, weights, policy explain, health recovery remain UNVERIFIED. |
| FT-11 | Runtime policy, rate limit, SSRF/security boundaries | UNVERIFIED | |
| FT-12 | Audit chain and emergency kill-switch | PARTIAL PASS | CP06 provisioning events had correct actor/scope and audit verification intact across 10 events; CP08 agent/key/task events and chain integrity also checked. Emergency kill-switch, tamper detection, other mutation/error paths remain UNVERIFIED. |
| FT-13 | Budgets, reservations, settlement and chargeback | PARTIAL PASS | CP05 created first project budget from empty Console state; CP11 settled one exact provider request without any budget ledger and reconciled the FinOps headline, project/model chargeback rows, CSV and JSON exports. Enforcement, estimates, multi-budget scopes and larger reconciliations remain UNVERIFIED. |
| FT-14 | OTel export, trace privacy and explorer | UNVERIFIED | |
| FT-15 | Console, CLI, Admin API, Data Plane API | PARTIAL PASS | CP04 built CLI config show/help/reconcile command routing with local synthetic API fixture; real Admin/Data Plane API paths remain UNVERIFIED. |
| FT-16 | Setup/startup, GitOps, encrypted backup, restore, upgrade | UNVERIFIED | |

**CP00–02 test capability note:** Local Go 1.26.11 linux/amd64 binary is built from the exact baseline. `go test ./...` and `go test -race ./...` both pass all 86 packages after applying environment-specific `GOFLAGS=-buildvcs=false` and `umask 0022`; `go run ./scripts/ci/check.go --all`, `go vet ./...`, `go run ./scripts/docs/check_drift.go`, Console typecheck, all 45 Console tests, and production build pass. These are build/automated quality results, not product-feature PASS results. RC4 DevBox artifact remains UNVERIFIED.

## Canonical screens SCR-01..16

| ID | Screen | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| SCR-01 | First-run setup/login | PARTIAL PASS | CP01 login and CP03 complete bootstrap in separate clean runtimes; other states/locales/browsers remain open. |
| SCR-02 | Overview/system posture | UNVERIFIED | |
| SCR-03 | Provider adapters | PARTIAL PASS | CP10 Ollama catalog review and activation; other adapters, upgrade, rollback and permission paths remain UNVERIFIED. |
| SCR-04 | Connections/credentials | PARTIAL PASS | CP10 Ollama defaults, connection persistence and successful live synthetic probe; secret binding and other auth methods remain UNVERIFIED. |
| SCR-05 | Models/deployments | UNVERIFIED | |
| SCR-06 | Extension marketplace | UNVERIFIED | |
| SCR-07 | Installed plugins/lockfile | UNVERIFIED | |
| SCR-08 | Capability routes/route explain | UNVERIFIED | |
| SCR-09 | Policies/guardrails | UNVERIFIED | |
| SCR-10 | MCP/A2A registry and tasks | PARTIAL PASS | CP08 visually inspected actual agent registration, A2A-only key grants, input-required tasks and completion; CP09 MCP server registration/persistence visually inspected. MCP tool sync/invocation, error recovery and concurrent drain remain UNVERIFIED. |
| SCR-11 | Request traces | UNVERIFIED | |
| SCR-12 | FinOps budgets/chargeback | PARTIAL PASS | CP05 visually reviewed empty budgets, create drawer and persisted budget row; CP11 visually reviewed no-budget chargeback headline/table and CSV export success; export contents matched selected Model grouping. Other dimensions and budget enforcement remain UNVERIFIED. |
| SCR-13 | Keys, projects, members, roles | PARTIAL PASS | CP06 project/environment provisioning; CP08 A2A-only and provider-only key creation, grant visibility and revocation screenshots inspected. Member/role journeys and rotation remain UNVERIFIED. |
| SCR-14 | Approvals/audit/kill-switch | PARTIAL PASS | CP06 live audit-log screen visually reviewed; project/environment event attribution and chain verification passed. Approvals and kill-switch remain UNVERIFIED. |
| SCR-15 | Playground/API explorer/diagnostics | UNVERIFIED | |
| SCR-16 | Operations/backups/upgrades/privacy | PARTIAL PASS | CP07 authenticated WebKit Operations→Webhooks at 375px; responsive drawer visible. Backup, upgrade, health, and privacy flows remain UNVERIFIED. |

## Business workflows WF-01..13

| ID | Workflow | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| WF-01 | First install/system bootstrap | UNVERIFIED | |
| WF-02 | Day-one setup wizard | UNVERIFIED | |
| WF-03 | Add official provider | UNVERIFIED | |
| WF-04 | Install community/external extension | UNVERIFIED | |
| WF-05 | Key and authorized OAuth connection | UNVERIFIED | |
| WF-06 | Unified/native API, stream, timeout, retry, cancel | UNVERIFIED | |
| WF-07 | Capability routing and provider failover | UNVERIFIED | |
| WF-08 | Quota/budget/chargeback without exact usage | PARTIAL PASS | CP11 exact settlement/chargeback/export verified with zero budget ledgers, and CP05 created first project budget. Quota consumption, overage, estimated/unmetered settlement and ledger holds remain UNVERIFIED. |
| WF-09 | MCP tool invocation, side-effect guard, approval | UNVERIFIED | |
| WF-10 | Key rotation and emergency revocation | PARTIAL PASS | CP08 A2A key revoked via Console; UI reported 0 active streams severed and old credential was denied with 401. Rotation/grace and revocation with an actual concurrent active A2A task remain UNVERIFIED. |
| WF-11 | Extension update, escalation block, rollback | UNVERIFIED | |
| WF-12 | Incident detection, containment, recovery | UNVERIFIED | |
| WF-13 | Data retention, anonymization, deletion | UNVERIFIED | |

## Cross-cutting coverage

| Dimension | QA-002 status | Evidence / open coverage |
| --- | --- | --- |
| User roles: first-time owner, developer, administrator, Team Admin, security reviewer, operator, FinOps, extension author | PARTIAL PASS | First-time owner setup on 18084 (CP03) and login on 18083 (CP01); remaining roles UNVERIFIED. |
| Tenant and resource authorization; default-deny and cross-tenant boundaries | PARTIAL PASS | CP08 provider-only key was denied A2A discovery/task with 403; revoked key received 401. Cross-tenant and all-resource boundaries remain UNVERIFIED. |
| Provider integrations: Google, Meta, deterministic synthetic provider, adapter failures | PARTIAL PASS | CP10/11 Ollama synthetic success, exact settlement; CP12 isolated HTTP 401 probe and fail-closed route. Google provider remains blocked/UNVERIFIED for Cloud egress vs adapter separation; Meta and more adapters remain open. |
| Platforms/modes: standalone SQLite, Docker, PostgreSQL/Valkey clustered, restart/upgrade | UNVERIFIED | |
| Browsers: Chromium, Firefox, WebKit; real interaction/screenshots | PARTIAL PASS | CP01–06 real Chromium flows/screenshots; CP07 real WebKit 26.5 at 375px. Firefox workflow still UNVERIFIED. |
| Responsive: 320/375/768/1024/1440/1920; loading/empty/error/success states | PARTIAL PASS | CP07 WebKit webhook drawer 375×844; CP03/05 Chromium 1440×1000. Other widths and states UNVERIFIED. |
| Themes: light/dark; 15 locales | UNVERIFIED | |
| Accessibility, keyboard, screen-reader labels, focus/recovery | PARTIAL PASS | CP03 live axe: four setup steps, 0 violations; other screens, keyboard/screen-reader, locales, and browsers UNVERIFIED. |
| Security: SSRF, secrets, audit integrity, kill-switch, plugin boundaries | UNVERIFIED | |
| Operational reliability: health, backup/restore, restart persistence, diagnostics | UNVERIFIED | |

## Update protocol

After each phase, update only rows supported by phase evidence. Keep partial results and residual scenarios explicit; a broad row can remain UNVERIFIED while specific subpaths pass or fail. Record every newly discovered defect with the next stable identifier beginning `DEF-016` in `reports/` and add the same evidence link to a checkpoint.
