# `TP-IMP-0046` Execution & Verification Report — 4-Journey Docs Portal, `check_drift.go` & GitHub Repo Polish

- **Unit ID:** `TP-IMP-0046`
- **Model:** **Gemini 4 Hydrogen**
- **Decision:** `DEC-095` *(delegated under `DEC-092` M5 sequential authorization)*
- **Branch / PR:** `develop` → `main` *(PR [#38](https://github.com/nguywnben/tanpopo/pull/38), merged `main` SHA [`c8a2ea14`](https://github.com/nguywnben/tanpopo/commit/c8a2ea1433d6c8e0feb5dbef2248db80ac5933d7); post-merge CI [`45008483361`](https://github.com/nguywnben/tanpopo/actions/runs/45008483361) [`Stage 0-2`] & [`45008483379`](https://github.com/nguywnben/tanpopo/actions/runs/45008483379) [`Release Dry Run`] `SUCCESS`)*
- **Review Tier:** **`STANDARD_REVIEW`**
- **Verdict:** **`DONE`** *(with explicit honest limitation notes per `COND-PO-01`)*

---

## 1. Delivered Scope & Repository Polish

### A. Root Open-Source Documentation & GitHub Repository Polish
- **Root [`README.md`](../../README.md):** Rewrote completely in English for first-time adopters (`tanpopo-logo.svg`, Apache-2.0 & CI badges, `What Makes Tanpopo Different`, 4-plane isolation, `< 3m` standalone & rootless `UID 65532:65532` Compose quickstart, OpenAI SDK & curl examples using `vk_tp_test_example_replace_me`, 8 bundled adapters + `DEC-004` compliance matrix, deployment topologies, honest `COND-PO-01` empirical benchmarks & transparent limits from [`benchmark-evidence.md`](../quality/benchmark-evidence.md) and [`pentest-v1.0-remediation-report.md`](../security/pentest-v1.0-remediation-report.md), and portal links).
- **GitHub Metadata (`gh repo edit nguywnben/tanpopo`):** Set repository description (`Open-source, self-hosted, extension-first AI Gateway, MCP Hub & A2A Coordinator (Apache-2.0)`) and topics (`a2a`, `ai-gateway`, `golang`, `llm-gateway`, `mcp`, `self-hosted`).
- **Governance, Legal & Release Notes:** Added [`SECURITY.md`](../../SECURITY.md) (`Sev-0..3` disclosure SLAs, Go `1.26.11+`, vault/egress/multi-tenant controls, `DEC-004`), [`CONTRIBUTING.md`](../../CONTRIBUTING.md) (`check.go --all`, `check_drift.go`, `-race`, `web/console` `sync:embed`, DAG & Floor Guard), [`CHANGELOG.md`](../../CHANGELOG.md) (`M0`–`M5`), and [`NOTICE`](../../NOTICE) (Apache-2.0 third-party dependency notices aligned with `go.mod` and `web/console/package.json`).

### B. Four-Journey Documentation Portal (`docs/guides/`)
- **Journey 1 — Quickstart & First Inference:** [`docs/guides/quickstart.md`](../guides/quickstart.md) (`--role all`, Docker Compose, `/setup` bootstrap, `vk_tp_test_example_*`, `/v1/chat/completions`, `/v1/models`, `/native/{dialect}/{upstreamPath}`).
- **Journey 2 — Extension Authoring SDK:** [`docs/guides/plugin-authoring-sdk.md`](../guides/plugin-authoring-sdk.md) (Tier-1 Declarative vs. Pure-Go `wazero` Wasm sandbox, `tanpopo plugin init|validate|test|pack|migrate-check`, `tanpopo upgrade preflight`, `tanpopo config reconcile`).
- **Journey 3 — Production HA, Backups, Incidents & Air-Gap:** [`docs/guides/production-ha-and-airgap.md`](../guides/production-ha-and-airgap.md) (split `control-plane`/`data-plane`, `db migrate-engine`, `admin verify-state`, `.tpop.enc` `backup create|verify|restore`, `vault rotate-kek`, `admin reset-local-owner`, `support-bundle`, `DR-01..04`, `INC-01..07` links, and `--offline` `bundle export|verify|import`).
- **Journey 4 — Security, Vault/KMS & MCP/A2A Governance:** [`docs/guides/security-and-mcp-governance.md`](../guides/security-and-mcp-governance.md) (`Master KEK -> Org DEK -> Project DEK`, Cloud KMS/Vault, `BlindHostCredentialBroker`, OIDC + Four-Eyes, `audit verify`, `tenant shred --org --project --generation`, `MCP 2025-03-26` JCS pinning & HITL, A2A budgets, `DEC-004`).
- **Hub & 15-Language Role Guide Synchronization:** Updated [`docs/guides/README.md`](../guides/README.md), [`docs/README.md`](../README.md), and all 15 `docs/guides/*/user-guide.md` CLI tables (`--role all`, `--output json`, `support-bundle`, `admin reset-local-owner`, `bundle export|verify|import`, `key|route|audit|killswitch`).

### C. Automated Drift Gate (`scripts/docs/check_drift.go` & `.github/workflows/ci.yml`)
- Recursively crawls `tanpopo --help` (`50` registered CLI commands/subcommands) and verifies all `190` CLI invocations across root docs, 4 journeys, and all 15 localized `user-guide.md` files (rejecting stale subcommands, short flags like `-z`, or trailing-pipe flags `NEG-1`).
- Parses `openapi/{dataplane,admin}-v1.yaml` (`98` paths), verifies `deploy/monitoring/alerts.yaml` `INC-01..07` labels & `runbook_ref` anchors across `monitoring-incidents.md` and `production-ha-and-airgap.md` (`NEG-1` fail-closed test), checks all `183` internal Markdown links/headings and `docs/assets/tanpopo-logo.svg`, checks secret & `Apache-2.0` hygiene, and executes a live hermetic `tanpopo serve` Quickstart probe (`/readyz` -> `/admin/v1/bootstrap/complete` -> `/admin/v1/auth/login` with explicit cookies over HTTP loopback -> `/admin/v1/virtual-keys` -> `GET /v1/models` -> `401` on `/admin/v1` -> structured `POST /v1/chat/completions`) in `2.194s`.
- Wired `CGO_ENABLED=0 go run ./scripts/docs/check_drift.go --skip-quickstart` into `.github/workflows/ci.yml` (`Stage 0`), while `go test -v -short ./...` runs the live binary test across Ubuntu, macOS, and Windows (`.exe` binary naming handled cleanly).

---

## 2. Independent Review Sign-Off (`STANDARD_REVIEW`)

| Reviewer | Role | Verdict | Findings & Remediation Summary |
|---|---|---|---|
| `c997d289dccb11b334e589d2460e655a` | `code-reviewer` | **`APPROVED`** *(after remediation)* | Initial review (`CHANGES_REQUIRED`) flagged Critical HTTP-loopback `cookiejar` dropping `Secure` `__Host-tanpopo_admin_session` cookie + PG/Valkey env leakage in `ExecuteQuickstartProbe`, plus `--role worker` in `README.md`, Compose UID `65532:65532` permissions, `Wasmtime` wording in `plugin-authoring-sdk.md`, `execution-baseline.md` stale header, short-flag/trailing-pipe & `CheckMonitoringRunbooks` test gaps, and `all-in-one` in `user-guide.md`. All findings and non-blocking nits (`tenant shred` flags, `/native/{dialect}/{upstreamPath}`, `worker` text across all 15 user guides) were remediated and re-inspected for final `APPROVED`. |

---

## 3. Empirical Verification Ledger

| Check | Outcome | Observed Result |
|---|---|---|
| Drift gate + live Quickstart probe | **PASS** | `CGO_ENABLED=0 go run ./scripts/docs/check_drift.go` -> `PASS check_drift: cli_cmds=50 cli_refs=190 openapi_paths=98 inc_runbooks=7 md_links=183 quickstart=2.194s` |
| Drift test suite (`AC-1`, `AC-2`, `NEG-1` `-race`) | **PASS** | `go test -v -race ./scripts/docs/...` -> `4/4 PASS` (`12.657s`, `0` races) |
| Repo Floor Guard, DAG, license, UI layout & vet | **PASS** | `CGO_ENABLED=0 go run ./scripts/ci/check.go --all && go vet ./...` -> exit `0` |
| Full Go short suite (hermetic) | **PASS** | `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...` -> exit `0` |
| Web Console 15-locale + TS check | **PASS** | `cd web/console && npm run typecheck && npm run check:i18n` -> `i18n check passed (15 locale(s), web + go catalogs + user guides).` |
| Release SBOM license checker unit tests | **PASS** | `python3 -m unittest discover -s scripts/release -p "test_*.py" -v` -> `4/4 OK` (`0.001s`) |

### Explicit Boundaries (`COND-PO-01`)
- Docker Compose live execution (`deploy/compose/docker-compose.standalone.yml --build`) and non-`en` prose localization beyond CLI command-table alignment remain governed within [`TP-IMP-0047`](../planning/units/TP-IMP-0047.md); release tagging remains gated behind human PO sign-off at `GATE-REL-07`.
