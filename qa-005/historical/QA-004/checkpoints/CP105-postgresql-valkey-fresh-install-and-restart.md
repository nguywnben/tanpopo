# CP105 — PostgreSQL/Valkey fresh install and cold restart

**Frozen baseline:** RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Coverage result:** fresh PostgreSQL install and persistence PASS (scoped); Valkey service health PASS; application rate-limit behavior and HA UNVERIFIED; Console PostgreSQL encrypted backup BLOCKED by documented backend limitation; backup/restore UNVERIFIED.

## Environment and setup diagnosis

Started the exact frozen Linux amd64 artifact on isolated loopback port 18133, with a newly created PostgreSQL 16.16 database, private data directory and synthetic Org Owner. A pre-existing three-node Valkey Cluster was independently checked on ports 16379–16381: all nodes answered PING and reported `cluster_state:ok`, 16,384 slots OK, and zero failed or failing slots.

The first startup attempt correctly failed closed because the environment’s supplied PostgreSQL login was a superuser with `BYPASSRLS`. This is an infrastructure credential mismatch, not a product defect. A dedicated runtime role was created with `NOSUPERUSER`, `NOBYPASSRLS`, `NOCREATEDB`, and `NOCREATEROLE`. Startup then exposed the documented webhook-worker prerequisite: the login must be a member of a dedicated `NOLOGIN` `tanpopo_webhook_worker` role with narrow queue grants. That role and only the documented grants were provisioned for this isolated database, after which the same artifact became ready. Privileged setup credentials were never used by the Tanpopo runtime, and no credentials or raw server logs are included in this handoff.

## Real browser flow and results

In Chromium at 1440×1000, completed `/setup` with a synthetic owner, new organization, default project and dev environment. The wizard completed with HTTP 201 and navigated to Overview. Screenshots of the initial setup, review, completion, Overview and Operations view were visually inspected. Authenticated API readback confirmed exactly one organization, one project and one dev environment. The UI’s Audit Log showed organization/project/environment creation events and verified an intact RFC 8785 chain across 6 events.

After cleanly stopping and restarting the same artifact against the same PostgreSQL database and key, `/readyz` again returned 200. The owner logged in; organization, project, dev environment and audit records were still present. The Console’s Audit Log UI verified the chain intact across 8 events. This is a scoped cold-process persistence pass; it does not establish power-loss, storage-loss, or multi-node HA durability.

Operations → Health & Cluster reported `storage_engine: postgres`, ready, epoch 1, and one healthy `primary` node. This was a singleton runtime, so HA and Valkey-backed application behavior were not established. Operations → Backups explicitly reported that Console snapshots support SQLite only and PostgreSQL deployments must use PostgreSQL backup tooling. The UI did not offer a PostgreSQL backup action. This limitation is presented as expected/documented; no product defect was assigned. PostgreSQL backup and restore remain unverified because external backup tooling was not provisioned.

One exploratory `GET /admin/v1/environments` returned 404; source route inspection showed that environments are listed at `/admin/v1/projects/{projectId}/environments`. The corrected authenticated read returned 200 with the expected `dev` row. This was a harness path error, not a product defect. A direct POST to audit verification returned CSRF 403; the real Console button verified the chain successfully, so the 403 is expected protection.

## Evidence

- [Sanitized setup journey](../evidence/CP105-postgres-valkey-journey.json); screenshots: [setup initial](../evidence/CP105-setup-initial.png), [review](../evidence/CP105-setup-review.png), [completion](../evidence/CP105-setup-complete.png), [Overview](../evidence/CP105-owner-overview.png), and [Operations](../evidence/CP105-postgres-operations-ui.png).
- [Initial persisted-state and Ops readback](../evidence/CP105-postgres-persistence-and-ops.json); [correct project-scoped environment and audit UI readback](../evidence/CP105-postgres-audit-backup.json).
- [Cold-restart persistence and Ops readback](../evidence/CP105-cold-restart-readback.json) and visually inspected [post-restart audit chain screenshot](../evidence/CP105-audit-chain-after-restart.png).
- Visually inspected [PostgreSQL backup limitation screen](../evidence/CP105-postgres-backup-limitation-ui.png).
- [Valkey cluster setup response](../evidence/CP105-postgres-valkey-journey.json) captures only product UI/API output; direct node-health observations are summarized above.

## Coverage

- Fresh PostgreSQL 16.16 setup and owner login: **PASS (scoped)**.
- Organization/project/environment and audit persistence across browser refresh and process restart: **PASS (scoped)**.
- `/readyz`, authenticated Operations storage identity, and audit-chain UI verification: **PASS (scoped)**.
- Valkey three-node service cluster health: **PASS (infrastructure only)**.
- Application-specific Valkey rate-limit behavior, shared state, and cross-node consistency: **UNVERIFIED**.
- PostgreSQL Console encrypted backup operation: **BLOCKED** by the documented SQLite-only Console snapshot implementation; external PostgreSQL backup workflow not provisioned.
- PostgreSQL backup/restore, failover, node loss, multi-node runtime, and power-loss recovery: **UNVERIFIED**.

No product implementation was modified and no new defect was assigned. QA continues; **GATE-REL-07 remains BLOCKED.**
