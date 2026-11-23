# QA-002 full-product coverage matrix

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`. This is a new regression campaign. QA-001 coverage is reference only and does not count as QA-002 PASS.

Status definitions: **PASS** = the named user journey and observable postconditions succeeded; **FAIL** = behavior contradicted the expected contract; **BLOCKED** = a prerequisite prevented the test; **UNVERIFIED** = not yet executed or inconclusive. Each PASS must link to current evidence.

## Product features FT-01..16

| ID | Coverage scope | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| FT-01 | Unified inference, multimodal, streaming, cancellation, errors/recovery | UNVERIFIED | |
| FT-02 | Native dialect passthrough and lossless fields | UNVERIFIED | |
| FT-03 | Adapter → connection → model → deployment lifecycle | UNVERIFIED | |
| FT-04 | Secret vault, authorized OAuth, federated identity | UNVERIFIED | |
| FT-05 | Extension isolation and blind credential broker | UNVERIFIED | |
| FT-06 | Registry, package install, lockfile, contract harness | UNVERIFIED | |
| FT-07 | MCP discovery, grants, schema pinning, approvals | UNVERIFIED | |
| FT-08 | A2A discovery, authorization, delegation, tasks/lifecycle | UNVERIFIED | |
| FT-09 | Org/project/environment, six-role RBAC, key lifecycle | UNVERIFIED | |
| FT-10 | Capability routing, health, failover, route explain | UNVERIFIED | |
| FT-11 | Runtime policy, rate limit, SSRF/security boundaries | UNVERIFIED | |
| FT-12 | Audit chain and emergency kill-switch | UNVERIFIED | |
| FT-13 | Budgets, reservations, settlement and chargeback | UNVERIFIED | |
| FT-14 | OTel export, trace privacy and explorer | UNVERIFIED | |
| FT-15 | Console, CLI, Admin API, Data Plane API | UNVERIFIED | |
| FT-16 | Setup/startup, GitOps, encrypted backup, restore, upgrade | UNVERIFIED | |

**CP00–02 test capability note:** Local Go 1.26.11 linux/amd64 binary is built from the exact baseline. `go test ./...` and `go test -race ./...` both pass all 86 packages after applying environment-specific `GOFLAGS=-buildvcs=false` and `umask 0022`; `go run ./scripts/ci/check.go --all`, `go vet ./...`, `go run ./scripts/docs/check_drift.go`, Console typecheck, all 45 Console tests, and production build pass. These are build/automated quality results, not product-feature PASS results. RC4 DevBox artifact remains UNVERIFIED.

## Canonical screens SCR-01..16

| ID | Screen | QA-002 status | Evidence / open coverage |
| --- | --- | --- | --- |
| SCR-01 | First-run setup/login | UNVERIFIED | |
| SCR-02 | Overview/system posture | UNVERIFIED | |
| SCR-03 | Provider adapters | UNVERIFIED | |
| SCR-04 | Connections/credentials | UNVERIFIED | |
| SCR-05 | Models/deployments | UNVERIFIED | |
| SCR-06 | Extension marketplace | UNVERIFIED | |
| SCR-07 | Installed plugins/lockfile | UNVERIFIED | |
| SCR-08 | Capability routes/route explain | UNVERIFIED | |
| SCR-09 | Policies/guardrails | UNVERIFIED | |
| SCR-10 | MCP/A2A registry and tasks | UNVERIFIED | |
| SCR-11 | Request traces | UNVERIFIED | |
| SCR-12 | FinOps budgets/chargeback | UNVERIFIED | |
| SCR-13 | Keys, projects, members, roles | UNVERIFIED | |
| SCR-14 | Approvals/audit/kill-switch | UNVERIFIED | |
| SCR-15 | Playground/API explorer/diagnostics | UNVERIFIED | |
| SCR-16 | Operations/backups/upgrades/privacy | UNVERIFIED | |

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
| WF-08 | Quota/budget/chargeback without exact usage | UNVERIFIED | |
| WF-09 | MCP tool invocation, side-effect guard, approval | UNVERIFIED | |
| WF-10 | Key rotation and emergency revocation | UNVERIFIED | |
| WF-11 | Extension update, escalation block, rollback | UNVERIFIED | |
| WF-12 | Incident detection, containment, recovery | UNVERIFIED | |
| WF-13 | Data retention, anonymization, deletion | UNVERIFIED | |

## Cross-cutting coverage

| Dimension | QA-002 status | Evidence / open coverage |
| --- | --- | --- |
| User roles: first-time owner, developer, administrator, Team Admin, security reviewer, operator, FinOps, extension author | UNVERIFIED | |
| Tenant and resource authorization; default-deny and cross-tenant boundaries | UNVERIFIED | |
| Provider integrations: Google, Meta, deterministic synthetic provider, adapter failures | UNVERIFIED | |
| Platforms/modes: standalone SQLite, Docker, PostgreSQL/Valkey clustered, restart/upgrade | UNVERIFIED | |
| Browsers: Chromium, Firefox, WebKit; real interaction/screenshots | UNVERIFIED | |
| Responsive: 320/375/768/1024/1440/1920; loading/empty/error/success states | UNVERIFIED | |
| Themes: light/dark; 15 locales | UNVERIFIED | |
| Accessibility, keyboard, screen-reader labels, focus/recovery | UNVERIFIED | |
| Security: SSRF, secrets, audit integrity, kill-switch, plugin boundaries | UNVERIFIED | |
| Operational reliability: health, backup/restore, restart persistence, diagnostics | UNVERIFIED | |

## Update protocol

After each phase, update only rows supported by phase evidence. Keep partial results and residual scenarios explicit; a broad row can remain UNVERIFIED while specific subpaths pass or fail. Record every newly discovered defect with the next stable identifier beginning `DEF-016` in `reports/` and add the same evidence link to a checkpoint.
