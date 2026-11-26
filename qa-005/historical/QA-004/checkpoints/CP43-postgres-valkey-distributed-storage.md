# CP43 — PostgreSQL, Valkey, and distributed propagation

**Status:** PARTIAL; the listed isolated database, cache, and multi-node race runs PASS. Broad production-operation coverage remains UNVERIFIED.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged and mounted read-only during test execution.

## Environment

The host `go` command resolves to an unrelated board-game executable. The cached `golang:1.26.11` image supplies the baseline-compatible compiler and ran Go tests against a read-only source mount. Go module downloads initially failed TLS validation inside the image because the managed environment’s CA was missing; mounting the host CA bundle read-only restored dependency resolution without disabling certificate validation.

Created isolated ephemeral services outside the repository:

- PostgreSQL 16.16 in container `qa004-postgres16`, data on container tmpfs, published only to `127.0.0.1:25432`. The separate application role was confirmed `NOSUPERUSER` and `NOBYPASSRLS`; a distinct admin role is used only by test setup.
- Valkey 8.1.12 as a three-primary cluster on loopback ports `16379`–`16381`. All three nodes reported `cluster_state:ok`, 16,384/16,384 slots healthy, and three known nodes.
- Generated database credentials, environment files, module/build caches, and data stayed under `/tmp` or ephemeral containers and are not in QA evidence or Git.

## Execution results

All runs used `-race -count=1`, the PostgreSQL-required and Valkey-cluster-required flags, and an isolated test DSN. Evidence records exact commands and package/scenario results in `evidence/CP43-postgres-valkey-race-results.json`.

- `internal/storage/postgres` and `internal/security/audit`: PASS.
- `internal/cluster/valkey`, `internal/finops/ledger`, and `internal/ops/migrate`: PASS. The Valkey test exercised cluster rate limiting across shard hash tags and PostgreSQL-backed DR rebuild after cache clearing.
- `cmd/tanpopo` PostgreSQL FORCE RLS end-to-end and control-plane / three-data-plane PG16+Valkey propagation cases: PASS. The multi-node test passed again in an independent repeat.

These results exercise selected executable storage/distribution contracts, not a live-production fault-domain deployment. They are not a substitute for user-journey validation or acceptance of the whole product.

## Remaining coverage

PostgreSQL upgrades across supported versions, storage-engine migration of populated production-like data, process crash and restart, real node/network partitions, split-brain, Valkey failover under active traffic, encrypted backup from PostgreSQL, multi-node kill-switch timing under long-lived SSE, load/latency, OTel collector export, other runtime roles, and other supported deployment modes remain UNVERIFIED. No new defect was identified by these passing scoped tests; QA continues and **GATE-REL-07 remains BLOCKED**.
