# `TP-IMP-0040` Execution & Verification Report — Valkey Cluster Coordination, `< 5s` Revocation, Stale-Node Circuit Breaker, PG16 Runtime Cutover & HA Helm

> **Unit:** `TP-IMP-0040` (`M4` / `EP-08` · `REQUIRES_STRONG_REVIEW`)
> **Branch / Workflow:** `develop` -> PR [#30](https://github.com/nguywnben/tanpopo/pull/30) -> `main` (`b5904476938e3ae517e895d136b6808a1c68876a`, accepted `2026-11-21` under [`DEC-088`](../governance/decisions.md) & [`DEC-089`](../governance/decisions.md))
> **Post-Merge CI:** [`44919349208`](https://github.com/nguywnben/tanpopo/actions/runs/44919349208) (`Stage 0-1` `SUCCESS`) · [`44919349221`](https://github.com/nguywnben/tanpopo/actions/runs/44919349221) (`Release Artifact Dry Run` `SUCCESS`)
> **Author:** `Gemini 4 Hydrogen`
> **Independent Strong Reviewers:**
> - `code-reviewer` (`f577a99eec01dfecc74f08cad0c211b1`): **`APPROVE`** (Critical: 0, Required Changes: 0; optional note on PG PK `23505` vs `404` on cross-org guessed `pol_*`/`bdg_*` PUTs)
> - `security-auditor` (`5f04aa986d3720eeb0c310c2b6d18653`): **`PASS`** (Critical: 0, High: 0, Medium: 0; Low operational note on Helm `preStop` `/bin/sh` vs distroless and Info note on multi-replica CP session affinity)

---

## 1. Delivered Scope

1. **Valkey Cluster Hash-Tag Coordination (`CR-07`):**
   - [`internal/cluster/valkey/`](../../internal/cluster/valkey/): Pure-Go `github.com/valkey-io/valkey-go` cluster client enforcing `tp:v1:{<org_id>}:<subsystem>:<entity>` key formatting, `AssertSameHashTag(keys...)` pre-`EVAL` guard, `DisableCache: true`, TLS 1.2+ enforcement, socket control guardrails (`SSRF`/metadata block), atomic Lua rate limiting (`RPM`/`TPM`/concurrent streams) + idempotency + fast budget holds with `1s` PostgreSQL `valkey_hold_checkpoints` heartbeats, and automatic `Local Fallback Quotient` / PostgreSQL fail-closed fallback.
2. **3-Layer `< 5s` Cluster Revocation & `> 10s` `FMEA-07` Circuit Breaker (`SLO-07`, `INC-07`):**
   - [`internal/config/snapshot/cluster_sync.go`](../../internal/config/snapshot/cluster_sync.go), [`internal/config/snapshot/manager.go`](../../internal/config/snapshot/manager.go), [`cmd/tanpopo/serve.go`](../../cmd/tanpopo/serve.go): Synchronizes Control Plane mutations (`s.publishChange`) and revocations across `Valkey Pub/Sub` (Layer 1), PostgreSQL `LISTEN/NOTIFY` with `pgx.Identifier.Sanitize()` (Layer 2), and `2s` `config_changelog` polling (Layer 3). Data Plane nodes cut over to live PG16 + Valkey and transition `/readyz -> 503` (failing closed on traffic) when both PostgreSQL and Valkey connectivity lapse for `> 10s`.
3. **Full PostgreSQL 16 + `FORCE RLS` Runtime Parity Across Control/Data Plane:**
   - Remediated dialect & transaction-scoped PG16 `FORCE ROW LEVEL SECURITY` (`port.SetOrgRLSTx`, `port.SetOperatorRLSTx`, `port.Rebind`, `port.FlexBool`/`BoolVal`/`BoolExpr`, `to_regclass`, SQLSTATE `23505`/`23503`/`23514`, `seq_no DESC`, `MAX(config_epoch)`, `audit.VerifyAuditChainTx`, HSTS on TLS, and `decodeJSONAndCheckOrgSpoofing`) across `internal/admin/http/*` (`auth.go`, `bootstrap.go`, `connections.go`, `extensions.go`, `finops.go`, `mcp.go`, `mcp_approvals.go`, `models.go`, `oauth.go`, `ops.go`, `ops_lifecycle.go`, `playground.go`, `policies.go`, `rbac.go`, `routes.go`, `secrets.go`, `server.go`, `tenancy.go`, `traces.go`, `vkeys.go`, `webhooks.go`), `internal/webui/embed.go`, `internal/security/{killswitch,oauth}`, `internal/extension/{installer,registry}`, `internal/provider/catalog`, `internal/ingress/mcp/{approval,hub}`, `internal/finops/rollup`, `internal/iam/{auth,bootstrap,vkey}`, `internal/policy/engine`, `internal/ingress/idempotency`, `internal/ratelimit`, and `internal/telemetry/{otel,ttl}`.
4. **DR-02 `tanpopo admin verify-state` & 15-Locale Sync:**
   - [`internal/cli/admin/verify_state.go`](../../internal/cli/admin/verify_state.go), [`internal/cli/admin/reset_owner.go`](../../internal/cli/admin/reset_owner.go), [`internal/cli/admin/verify_state_test.go`](../../internal/cli/admin/verify_state_test.go), [`internal/pkg/i18n/locales/*.json`](../../internal/pkg/i18n/locales/): Added `--check-audit-chain`, `--reconcile-orphaned-holds`, and `--rebuild-valkey-state` across SQLite and PG16 + Valkey with all 15 CLI locales synchronized.
5. **Role-Split Production Helm Chart & Graceful SSE Drain (`OPS-AC-06`):**
   - [`deploy/helm/tanpopo/`](../../deploy/helm/tanpopo/), [`internal/ops/upgrade/helm_chart_test.go`](../../internal/ops/upgrade/helm_chart_test.go), [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml): Dedicated `control-plane` and `data-plane` Deployments, HPA, PDBs, default-deny `NetworkPolicy`, ServiceMonitor, non-root `65532` read-only rootfs defaults, and Valkey Cluster Stage 2 CI job.

---

## 2. Independent Verification & Test Evidence

- **Static Quality Gates (`CGO_ENABLED=0 go run ./scripts/ci/check.go --all`):** **`PASS`** (`0` errors).
- **Go Vet (`go vet ./...`):** **`PASS`** (`0` issues).
- **Full Hermetic Short Suite (`CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...`):** **`PASS`** across all packages.
- **Live PostgreSQL 16 (`FORCE RLS` / `tanpopo_app` `NOBYPASSRLS`) + 3-Shard Valkey Cluster (`127.0.0.1:7001..7003`) `-race` Suite:** **`PASS`** with `-race -count=1` across all `./internal/...` and `./cmd/...` packages:
  - `cmd/tanpopo` (`52.601s`): Includes `TestTPIMP0040GracefulDrainReadyzAndSSE`, `TestTPIMP0040MultiNodeClusterPG16AndValkeyAC1ToAC4` (4-node CP+3DP cluster `< 5s` revocation, `FMEA-07` partition circuit breaker, 3-shard `CROSSSLOT` = 0 + `FLUSHALL` rebuild, 100-SSE rolling restart `0` drops), and `TestTPIMP0040PG16ForceRLSEndToEnd` (`/admin/v1/*`, `/mcp`, `rollup`, `killswitch.EnsureActiveAbortBarrier`, `catalog.ValidateConnection`, `installer`, `oauth`).
  - `internal/admin/http` (`94.757s`), `internal/ingress/http` (`346.141s`), `internal/storage/postgres` (`24.585s`), `internal/cluster/valkey` (`2.417s`), `internal/config/snapshot` (`26.924s`), `internal/extension/installer` (`28.305s`), `internal/finops/ledger` (`25.018s`), `internal/finops/rollup` (`2.315s`), `internal/provider/catalog` (`30.569s`), `internal/security/audit` (`46.770s`), `internal/security/vault` (`46.198s`), `internal/security/oauth` (`16.014s`), `internal/security/killswitch` (`12.839s`), and all remaining packages.
- **Note on local Helm CLI:** `helm` binary is not preinstalled in this VM (`helm` lint/template/kubeconform checks execute in `.github/workflows/ci.yml`; Go chart structure tests pass via `internal/ops/upgrade/helm_chart_test.go`).
