# CP52 — PostgreSQL + Valkey partition fail-closed behavior and environment recovery

**Campaign:** QA-005 / TEEP-00 Baseline Stabilization
**Frozen source:** `737079ddb847d97dbcf832f610ed0048556cb65b`
**Runtime:** exact-source local Linux/amd64 build, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; Control Plane `127.0.0.1:18151`, Data Planes `:18152` and `:18153`.
**Disposition:** dual-dependency stale-snapshot fail-closed behavior **PASS (scoped)**; database replacement durability and restore **UNVERIFIED**. No product defect assigned.

## Acceptance basis and reproduction

Reviewed `docs/operations/monitoring-incidents.md` (FMEA-07), `docs/architecture/deployment-topology.md`, `docs/planning/units/TP-IMP-0040.md`, and its execution review. These require readiness to fail and authenticated Virtual Key traffic to be rejected after the runtime has been partitioned from both PostgreSQL and Valkey for more than 10 seconds; liveness remains available.

In Chromium, created the synthetic key `CP52 stale-snapshot breaker probe` (last four `57dF`). Before the partition, a request to each Data Plane authenticated but returned HTTP 503 `no_compliant_deployment_available`, as the test organization had no matching route. No provider request was sent. Stopped all three Valkey primaries and PostgreSQL, waited 13 seconds, then queried health, readiness, and authenticated Data Plane traffic. `/healthz` remained HTTP 200 `ok` on all three processes; `/readyz` returned HTTP 503 `not_ready` on all three. The valid key received HTTP 503 `snapshot_not_ready` at both Data Planes. This is a scoped PASS for the documented FMEA-07 fail-closed contract.

## Recovery limitation and fresh-install validation

The PostgreSQL container was disposable and had no persistent volume. Stopping it removed its writable layer and the synthetic database/key. The original DB therefore could not be restarted or restored; the key stopped being valid because its database was lost. This is an environment durability/setup limitation, not evidence that Tanpopo lost durable production data. PostgreSQL backup/restore and database replacement durability remain UNVERIFIED.

To restore QA execution, created a replacement isolated PostgreSQL 16 instance, a dedicated `NOSUPERUSER NOBYPASSRLS` application role, and the documented least-privilege `tanpopo_webhook_worker` role/grants. Recovered the three-node Valkey cluster (16,384 slots, `cluster_state:ok`) and restarted the same exact-source binary in the three roles. Chromium completed a fresh real `/setup` journey with a synthetic owner, organization and project; the provider step was skipped, key-custody acknowledgment was selected, review was inspected, and setup navigated to Overview. The Operations screenshot showed `postgres`, Ready, epoch 1, and all three process roles healthy. All setup, review and recovered Operations screenshots were visually inspected. Runtime `/readyz` returned HTTP 200 for all three roles.

## Result

- Dual PostgreSQL + Valkey partition after 13 seconds: **PASS (scoped)** against the listed accepted FMEA-07 behavior.
- Liveness while partitioned: **PASS (scoped)**.
- Database persistence through container removal/replacement: **UNVERIFIED**; the test container had no persistent volume and no backup was taken before the deliberate outage.
- Recovery by fresh isolated database + documented role provisioning + real setup: **PASS (environment restoration only)**.
- Historical defect status counts: unchanged. No new product defect was inferred.

Evidence is in [CP52 JSON](../evidence/CP52-database-and-valkey-partition.json). The screenshots capture the setup form, wizard review, recovered Overview and Operations state; no password, token or database content was committed. QA continues and **GATE-REL-07 remains BLOCKED**.
