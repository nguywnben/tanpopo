# QA-005 environment and artifact identity

See [CP00](checkpoints/CP00-baseline-identity.md) and [machine-readable build provenance](evidence/CP00-build-provenance.json). This record is updated as runtimes, databases and fixtures are provisioned.

## Frozen source and branch refs

- Repository: `nguywnben/tanpopo`
- QA-005 branch: `qa/qa-005-baseline-stabilization`, based on `737079ddb847d97dbcf832f610ed0048556cb65b`
- `origin/main` and `origin/develop`: both observed at the exact source commit by read-only `git ls-remote` on 2026-11-26
- Owner signing manifest branch: ref observed at `7503607c2188e7a314d355864e813960244f6c15`; branch was not checked out or modified
- Existing QA refs preserved and unchanged at intake: QA-001 `d994f821…`, QA-002 `1052ecb2…`, QA-003 `a691f248…`, QA-004 `733787cde…`

## Local test build

- OS/architecture: Linux amd64; CGO enabled; actual Go build, not CI test summary
- Toolchain: Go 1.26.11 downloaded from official go.dev archive and checksum verified
- Source: `737079ddb847d97dbcf832f610ed0048556cb65b` (`vcs.modified=false`)
- Build command: `go build -trimpath -o /tmp/qa005-bin/tanpopo ./cmd/tanpopo` with Go 1.26.11, `GOTOOLCHAIN=local`, module cache under `/tmp`
- Binary SHA-256 and embedded VCS provenance: see `evidence/CP00-build-provenance.json`
- CLI version defaults to `0.1.0-dev`, commit `none`, build date `unknown` because this QA build did not use release linker flags. The Go build metadata itself records the exact source commit and time. This is not a release artifact or signed artifact.
- `extensions/official/release-signatures.json` in frozen source contains an empty `signatures` array. No production signature was created or changed.

## Isolated environment

- The new managed workspace is connected; real Chromium automation and screenshot capture work through Playwright. Screenshots were visually opened and inspected. This is browser-rendered headless verification, not desktop/manual testing.
- `QA_META_MODEL_API_KEY` and `QA_GOOGLE_AI_STUDIO_API_KEY` were not used; no paid provider calls are authorized. Real provider interoperability remains BLOCKED/not attempted.
- Node `v24.22.3`, Chromium `156.0.8078.79`, Go `1.26.11`.
- SQLite WAL runtime: `127.0.0.1:18145`, disposable data and KEK under `/tmp/qa005-runtime`.
- PostgreSQL 16 runtime: `127.0.0.1:18146`; Docker service `127.0.0.1:15432`; application role `tpqa005_app` is NOSUPERUSER/NOBYPASSRLS. Temporary DB/role and random credentials were local QA-only. Dispatcher prerequisite `tanpopo_webhook_worker` was provisioned from the repo runbook; it is not a hidden environment failure.
- Valkey 8.1 standalone: Docker service `127.0.0.1:16379`, loopback only, tmpfs storage. Only ping, runtime startup and readiness have been checked; cluster/rate/budget data-path coverage remains UNVERIFIED.
- Deterministic OpenAI-compatible fixture: `127.0.0.1:19090`; all provider calls were loopback-only and used synthetic content.
- OTLP integration follow-up CP62 used OpenTelemetry Collector Contrib `0.130.0` Linux/amd64; the release asset SHA-256 matched the digest published in upstream GitHub release metadata. Receiver bound to loopback `127.0.0.1:4318`; an exact-source SQLite clone at `127.0.0.1:18156` sent trace, metric and log signals. Collector binary/log and the cloned DB remained under `/tmp` and were excluded. This validates a pinned local Collector path only, not a production telemetry backend.
- DEF-065 retest CP63 used an isolated SQLite clone and exact-source runtime on `127.0.0.1:18157` with a separate loopback metrics listener at `127.0.0.1:19157`. `TANPOPO_METRICS_SCRAPE_TOKEN` was synthetic, ephemeral and excluded. A real Chromium 156 Playwright scrape verified rendered Prometheus 0.0.4 exposition; screenshot contains no token.
- DEF-008 retest CP64 used a fresh isolated SQLite install at `127.0.0.1:18158`, created an encrypted backup through the actual Owner Console, then restored the artifact to an absent directory and started the restored service at `127.0.0.1:18159`. Backup archive, databases, bootstrap token, synthetic Owner password and KEK remain under `/tmp/qa005-runtime` and are excluded; only sanitized receipt/audit metadata and screenshots are tracked.
- An ephemeral test-only Ed25519 root under `/tmp/qa005-runtime` was used to exercise Marketplace consent. It is not a production signing key or provenance result.
- All DBs, volume data, generated credentials, bootstrap tokens, test root private key, raw Virtual Keys, invite tokens and encrypted backup archives stay outside the branch. Platform breadth beyond Linux amd64 and Chromium remains UNVERIFIED.


## CP24 A2A runtime variant

A separate SQLite online-backup clone was started on `127.0.0.1:18147` from the QA SQLite test DB. The clone used the same local exact-source RC7 binary and test master-key file, with `TANPOPO_A2A_ENABLED=true`; the baseline process on `127.0.0.1:18145` remained A2A-disabled by default. The local A2A mock was bound only to `127.0.0.1:19092` and allowed via the explicit QA-only `127.0.0.1/32` CIDR. The A2A Virtual Key secret, database copy, and test trust material are outside the repository in `/tmp`.


## CP117 migration test target and current storage limit

The managed overlay had 0 free bytes (100% used), so a new disk-backed PostgreSQL container could not initialize; no existing image, container or QA evidence was removed. To continue independently, CP117 launched a separate PostgreSQL 16 process in the existing QA-only container's 64-MiB `/dev/shm`, on a separate process port and fresh database, with a dedicated `NOSUPERUSER NOBYPASSRLS` migration role. A local-only tunnel exposed that process for the exact-source migration. The process, log and tunnel were stopped/removed; tmpfs returned to 1 MiB used. The existing QA PostgreSQL database was not modified. Detailed identity, rollback and command evidence: [CP117 JSON](evidence/CP117-finops-postgres-migration.json). Disk-backed infrastructure work may remain blocked until managed storage is made available; other tests that fit existing isolated runtimes remain executable.


## CP118 supplemental migration attempt

CP118 used an ephemeral copy under per-command RAM `/dev/shm`, source clone hash `900c0d36e6fda5b33957740bac1bc126707ecd6ed5732669499c1e771b8d830e`; it contained 9 budget rows after exactly three overlength IDs were removed, 48 settlements, 9 deployments and 727 audit events. A second PostgreSQL 16 process ran in the QA-only container RAM tmpfs under a restricted migrator role. The command's local bridge connected, but migration hung with the server session idle in `ClientRead`; the test was terminated and all temporary service/tunnel state removed. This filtered accounting/pricing parity slice is UNVERIFIED. Existing QA PostgreSQL and source DB remain unchanged. CP118 record: [checkpoint](checkpoints/CP118-pricing-migration-attempt.md) and [evidence](evidence/CP118-pricing-migration-attempt.json).

## CP120 local redirect target

The supplemental provider probe used exact RC7 on the existing isolated SQLite Owner runtime and Chromium 156. Two ephemeral Node HTTP listeners bound only to loopback: configured host at 127.0.0.1:19100 and receiver sentinel at 127.0.0.1:19101. The redirect response was deterministic and local. No provider credential was configured. Both listeners were closed after the probe; the temporary connection was deleted and readback showed no row. Evidence: [CP120 JSON](evidence/CP120-provider-redirect-host-port.json).
