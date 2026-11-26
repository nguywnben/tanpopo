# CP13–15 — Runtime recovery, PostgreSQL/Valkey and key lifecycle

**Campaign:** QA-005 / TEEP-00 baseline stabilization  
**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime artifact:** local exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; not the official RC7 artifact.  
**Disposition:** PARTIAL campaign coverage. DEF-081 is a new Major FAIL; DEF-075 is PARTIAL. No release verdict.

## CP13 — SQLite restart, operator state and Operations Console

Gracefully restarted the exact-source SQLite service on `127.0.0.1:18145` with the same isolated data and ephemeral KEK. `/healthz` and `/readyz` returned 200; bootstrap remained initialized. Read-only DB checks after restart reported integrity `ok`, WAL mode, 1 organization, 1 project, 1 environment, 1 provider connection, 4 then-active key records, 3 settlements and 91 audit events after local owner password recovery. The owner completed `tanpopo admin reset-local-owner` on this disposable QA database; generated recovery material stayed mode 0600 under `/tmp` and is excluded from evidence.

In Chromium, inspected the Operations Health & Cluster, Backups, Upgrades, Organizations and Webhooks screens. Health showed Ready, `sqlite_wal`, epoch 18 and the single `all` node. Backups showed zero API receipts because the independent CLI backup receipt was not created through the Console; do not treat that state as proof of backup API failure. Upgrade preflight for `0.2.0` returned `READY_TO_UPGRADE` while the test lockfile contained no extension pins; this is a smoke pass only, not extension compatibility evidence. No crypto-shred, webhook registration/delivery or upgrade was triggered. `tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds=false` passed with zero orphaned holds. Screenshots: `CP13-*` and `CP13-upgrade-preflight.png`.

## CP14 — Fresh PostgreSQL 16 + Valkey 8.1 runtime

Started disposable Docker PostgreSQL 16 and Valkey 8.1 containers with tmpfs data and loopback-only published ports. Credentials were random synthetic test material in `/tmp`, never committed. The PostgreSQL login `tpqa005_app` was verified `NOSUPERUSER` and `NOBYPASSRLS`. Initial Tanpopo start failed because the documented `tanpopo_webhook_worker` role/membership prerequisite was absent. Applied the repository runbook's test-only role, membership, schema usage and worker grants; then the runtime started and returned `/healthz` and `/readyz` 200. The real setup wizard initialized `QA005 PostgreSQL / PG Project / dev`; Operations visibly reported storage engine `postgres`, Ready and one healthy `all` node.

PostgreSQL migration readback found 67 public tables, 55 with both row security enabled and forced, and 58 policies. As the app login, a tenant-table query without `app.current_org_id` returned 0 organizations; after setting the explicitly selected synthetic test organization scope it returned 1. This is a scoped SQL RLS check, not proof of all tenant-facing API paths or non-forgeability of application context. Valkey was reachable (`PONG`), and runtime startup/readiness succeeded with it configured, but cluster mode/rate-limit/budget/rebuild behavior remains unverified; the fixture was standalone Valkey (its `CLUSTER INFO` command correctly reports cluster support disabled).

Activated the bundled `@tanpopo/ollama` extension through Chromium after viewing the permission diff and explicitly consenting to loopback/private-IP access. The package used test-only ephemeral publisher root/signatures; this is not production signing. Approval returned a visible `adapter_registration_failed` error. Readback showed the package `ENABLED_ACTIVE` while `/admin/v1/provider-adapters` remained empty. After a graceful service restart it remained active, and the provider-adapter API still returned an empty list. The same package and test signature material registered successfully in the SQLite runtime at CP03. This independent cross-engine failure is **DEF-081 (Major, FAIL)**; exact root cause is not proven. Evidence: `CP14-postgres-*` screenshots and `evidence/CP14-postgres-provider-registration.json`.

## CP15 — DEF-075 rotation expiry and stale Playground inventory

On the SQLite instance, used the real owner Console rotation action on `QA005 loopback provider v2`. A test-only Playwright network interceptor changed the UI's 86,400-second request to the API's documented 60-second minimum. The API accepted the rotation and returned state `ROTATING_DUAL_VALID`, version 2, and a 60-second grace deadline. After expiry, independent database readback found state `CREATED_ACTIVE`, version 3, null prior-key digest and grace deadline; one `virtual_key.rotation_finalized` audit event existed. Thus lifecycle finalization now differs from the historical RC6 failure. The one-time old/current pair was not retained/correlated reliably for auth tests; an attempted expired-key request using a nonmatching harness secret is excluded. Therefore **DEF-075 remains PARTIAL**, not PASS: before/after authorization, old token rejection and current token validity still require a fresh trustworthy pair.

Access & Keys visually showed two Playground keys as `Active` even though their persisted 15-minute `expires_at_ms` values were more than 30 minutes past. This is recorded as **PG-002**, because the accepted contract says ephemeral 15-minute keys but does not state the expected inventory label after their expiry. Their specific show-once secrets were unavailable, so data-plane rejection was not re-tested. Screenshot: `CP15-access-owner-key-inventory.png`.

Temporary credentials, bootstrap tokens, test signing private root, PostgreSQL data, Valkey data, backup archives, restored DBs, raw Virtual Keys and invitation material are excluded from QA evidence. Screenshots and JSON contain only synthetic identifiers and no credentials.

## Remaining work and current classifications

- DEF-080 Low FAIL: unauthorized key inventory renders 403 alongside zero/empty result.
- DEF-081 Major FAIL: PostgreSQL extension activation leaves an active package without its provider adapter.
- DEF-075 PARTIAL: expiry cleanup and audit event pass; actual old/new key auth acceptance unverified.
- DEF-001–074, 076–079 remain UNVERIFIED on QA-005. No prior QA PASS is inherited.
- Official RC7 artifact identity remains UNVERIFIED; production signature integration remains BLOCKED; real-provider interoperability was not attempted.
- PostgreSQL routed provider traffic is blocked by DEF-081. Valkey data-path behavior, MCP/A2A, full FinOps migration and concurrency, tenant/API isolation, security role matrix, telemetry, cluster and platform/browser/a11y coverage remain UNVERIFIED or PARTIAL as listed in the full coverage matrix.
- The QA-005 campaign is ongoing. TEEP-01 readiness is not yet determined. GATE-REL-07 remains BLOCKED.
