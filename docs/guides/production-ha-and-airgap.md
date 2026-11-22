# Journey 3: Production HA, Backups, Incidents & Air-Gap

Operate Tanpopo in multi-node **Mode B/C** topologies backed by PostgreSQL 16+ (range partitioning + `FORCE ROW LEVEL SECURITY`), Valkey Cluster (`ACL` + TLS), encrypted `.tpop.enc` disaster recovery, and disconnected `--offline` `.tpbundle` deployments.

---

## 1. Split Control/Data Plane & PostgreSQL 16+ HA

In production clusters, separate admin mutation traffic from hot-path inference:

```bash
# Control Plane listener (internal mTLS / VPN / private ingress)
tanpopo serve --role control-plane --listen 10.0.1.10:9090

# Data Plane nodes (horizontally scaled, ephemeral read-only snapshot workers)
tanpopo serve --role data-plane --listen 0.0.0.0:8080
```

Helm chart templates live under [deploy/helm/tanpopo/Chart.yaml](../../deploy/helm/tanpopo/Chart.yaml).

### Migrating SQLite WAL to PostgreSQL 16+ (`TP-IMP-0039`)

Promote a single-node SQLite installation to partitioned PostgreSQL 16+ with transactional micro-USD balance, JCS audit hash-chain, and Vault envelope verification:

```bash
tanpopo db migrate-engine \
  --from ./data/tanpopo.db \
  --to "postgres://tanpopo_app@pg-primary.internal:5432/tanpopo?sslmode=verify-full" \
  --verify
```

### Valkey Cluster Reconciliation & Control Commands (`TP-IMP-0040`)

Verify audit chains, reclaim expired reservation holds, and rebuild Valkey budget/revocation state after a cache failover:

```bash
tanpopo admin verify-state \
  --pg-dsn "postgres://tanpopo_app@pg-primary.internal:5432/tanpopo?sslmode=verify-full" \
  --valkey-addrs "valkey-0.internal:6379,valkey-1.internal:6379" \
  --valkey-username tanpopo \
  --valkey-require-tls \
  --check-audit-chain \
  --reconcile-orphaned-holds \
  --rebuild-valkey-state
```

---

## 2. Encrypted Backups, Recovery & Support Bundles

| Operation | Command | Reference |
| --- | --- | --- |
| Create encrypted `.tpop.enc` backup | `tanpopo backup create --config data/tanpopo.yaml --lockfile data/tanpopo.lock --artifact-dir data/plugins --output-dir data/backups` | [backups-recovery.md](../operations/backups-recovery.md) |
| Verify `.tpop.enc` integrity offline | `tanpopo backup verify --archive data/backups/latest.tpop.enc` | [backups-recovery.md](../operations/backups-recovery.md) |
| Atomic restore with DEK trial-unwrap | `tanpopo backup restore --archive data/backups/latest.tpop.enc --destination ./data-restored --verify-secrets-decrypt` | [backups-recovery.md](../operations/backups-recovery.md) |
| Rotate Master KEK envelopes | `tanpopo vault rotate-kek --new-kek-id kek-2026-q4 --new-key-env TANPOPO_MASTER_KEY_NEXT` | [backups-recovery.md](../operations/backups-recovery.md) |
| Recover local `ORG_OWNER` password | `tanpopo admin reset-local-owner --db ./data/tanpopo.db --email owner@example.com` | [user-guide.md](./en/user-guide.md) |
| Export redacted diagnostic bundle | `tanpopo support-bundle -o ./tanpopo-diag.tar.gz --root ./data` | [installer-upgrades.md](../operations/installer-upgrades.md) |

> **Disaster Drills:** Verified procedures cover **DR-01** (backup & restore), **DR-02** (KEK rotation & DB failover), **DR-03** (extension quarantine), and **DR-04** (`< 60s` canary rollback with live SSE streams).

---

## 3. Incident Runbook Matrix (`INC-01`..`INC-07`)

Every Prometheus rule in [deploy/monitoring/alerts.yaml](../../deploy/monitoring/alerts.yaml) cross-links directly to [docs/operations/monitoring-incidents.md](../operations/monitoring-incidents.md):

| Incident | Trigger & Mitigation Runbook |
| --- | --- |
| `INC-01` | Budget exhaustion, soft/hard hold loops, missing rate card → [`monitoring-incidents.md#inc-01`](../operations/monitoring-incidents.md#inc-01) |
| `INC-02` | Leaked Virtual Key (`graceful` vs `emergency` revoke) or audit chain tamper → [`monitoring-incidents.md#inc-02`](../operations/monitoring-incidents.md#inc-02) |
| `INC-03` | Extension sandbox traps (`> 20 / 60s`) & auto-quarantine → [`monitoring-incidents.md#inc-03`](../operations/monitoring-incidents.md#inc-03) |
| `INC-04` | Upstream provider outage, 5xx burn rate & fail-closed data residency fence → [`monitoring-incidents.md#inc-04`](../operations/monitoring-incidents.md#inc-04) |
| `INC-05` | Expired/revoked OAuth 2.1 grant (`AUTH_EXPIRED_REAUTH_REQUIRED`) → [`monitoring-incidents.md#inc-05`](../operations/monitoring-incidents.md#inc-05) |
| `INC-06` | Upstream MCP tool schema mutation (rug-pull) or dropped OTLP telemetry → [`monitoring-incidents.md#inc-06`](../operations/monitoring-incidents.md#inc-06) |
| `INC-07` | Valkey cluster outage, snapshot staleness (`> 5s` warn, `> 10s` `/readyz = 503`) → [`monitoring-incidents.md#inc-07`](../operations/monitoring-incidents.md#inc-07) |

---

## 4. Disconnected Air-Gap Workflow (`TP-IMP-0045`)

Package verified `.tpkg` extensions, feeds, SBOMs, and Ed25519 trust signatures on a connected build host, transfer cleanly, and boot offline with zero network sockets:

```bash
# 1. Export signed .tpbundle on build workstation
tanpopo bundle export \
  --lockfile ./tanpopo.lock \
  --packages-dir ./plugins \
  --signing-key ./keys/release-ed25519.pem \
  --public-key ./keys/release-ed25519.pub \
  --include-revocation-feed \
  -o ./dist/site-bundle.tpbundle

# 2. Verify signatures, SHA-256 digests & revocation expiry on air-gapped host
tanpopo bundle verify --input ./site-bundle.tpbundle --public-key /etc/tanpopo/trust-root.pub

# 3. Atomically import verified bundle into offline state (and optional SQLite store)
tanpopo bundle import \
  --input ./site-bundle.tpbundle \
  --public-key /etc/tanpopo/trust-root.pub \
  --dest-dir ./data/airgap \
  --db ./data/tanpopo.db

# 4. Run Tanpopo strictly offline (rejects all outbound marketplace/extension fetches)
tanpopo serve --offline --role all --listen 127.0.0.1:8080
```
