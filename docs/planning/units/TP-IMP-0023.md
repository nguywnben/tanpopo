# `TP-IMP-0023` — Immutable Per-`Org` `JCS` Hash-Chained Audit Log, 4-Level Emergency Kill-Switch (`< 1s` Stream Sever) & `Blast-Radius` Query

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0023` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-04` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0009`, `TP-IMP-0011`, `TP-IMP-0022` — `GAP-01, 05`)* |
| **Size** | `L` (`internal/security/audit/*`, `internal/security/killswitch/*`, `db/migrations/sqlite/0010_audit_killswitch.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Tamper-evident forensic integrity `TM-13` & emergency security incident containment)* |
| **Technical Owner** | `Principal Security & Incident Response Engineer` |

---

## 1. Intent
Build the two ultimate defensive and forensic governance capabilities of the Control Plane (`TP-FR-0704`, `TP-FR-0705`):
1. **Immutable Per-`Organization` + `SYSTEM` Hash-Chained Audit Log (`Per-Org + SYSTEM RFC 8785 JCS Hash-Chained Audit Log` — `CR-06`):** Every administrative mutation, key issuance/revocation, plugin install/quarantine, policy update, and security violation is deterministically canonicalized via `RFC 8785 (JCS)` and chained via `entry_hash = SHA-256(chain_key || seq_no || prev_hash || jcs_payload)` (where `chain_key = org_id` for tenant events and `chain_key = "SYSTEM"` for instance-scoped events with `org_id IS NULL` such as `Bootstrap` / `Break-Glass`), protected by DB triggers forbidding `UPDATE`/`DELETE` and verified by a standalone `VerifyAuditChain` inspector.
2. **4-Level Emergency Kill-Switch & `Blast-Radius` Forensics:** Enables instant containment at (1) `VirtualKey`, (2) `ProviderConnection`, (3) `ExtensionInstallation` (`QUARANTINED`), or (4) full `Project / Organization Traffic Freeze` granularity, severing every matching in-flight SSE stream in `< 1 second` on single-node deployments (`SLO-07`, `OPS-AC-03`, `OPS-AC-04`, `INC-02`, `INC-03`) and enumerating every project, virtual key, and request trace that touched a compromised plugin or credential within an incident window.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/audit/jcs.go` (deterministic `RFC 8785` JSON canonicalizer across map keys, Unicode strings, and integer representations).
  - `internal/security/audit/logger.go` (gapless monotonic `seq_no` hash-chain writer partitioned by `chain_key` (`org_id` or `"SYSTEM"`), scrubbed through `privacy.Redactor`, enforcing `Fail-Closed` semantics on administrative mutations).
  - `internal/security/audit/verifier.go` (full hash-chain verifier across per-`org_id` and `"SYSTEM"` chains, raising `ALT-AUDIT-TAMPER` immediately upon detecting a single flipped bit or deleted row — `CT-30`, `OPS-AC-08`).
  - `internal/security/killswitch/registry.go` (`InFlightStream` registry indexed by `(org_id, project_id, vkey_id, connection_id, plugin_install_id)` invoking bulk `CancelFunc` callbacks in `< 1s` with audited reason codes when any kill-switch level fires).
  - `internal/security/audit/blastradius.go` (forensic correlation query joining `request_traces_meta` + `audit_events` by `plugin_install_id` / `connection_id` / `vkey_id` across `[from, to]`).
  - `db/migrations/sqlite/0010_audit_killswitch.sql` (`audit_events` guarded by `BEFORE UPDATE / BEFORE DELETE RAISE(ABORT, 'audit_events_immutable')` triggers, `emergency_kill_switches`).
- **Out-of-Scope:** Multi-node `< 5s` cluster-wide kill-switch fan-out over `Valkey` (owned by `TP-IMP-0040`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0704`, `TP-FR-0705`, `TP-NFR-0404`, `TP-NFR-0501`, `SLO-07`, `TP-BR-0803`, `CT-30`, `AUTH-06`, `OPS-AC-03`, `OPS-AC-04`, `OPS-AC-08`, `INC-02`, `INC-03`.
- **Dependencies:** `TP-IMP-0009`, `TP-IMP-0011`, `TP-IMP-0022`.
- **Design Refs:** [`threat-model.md`](../../security/threat-model.md) (`TM-13`), [`monitoring-incidents.md`](../../operations/monitoring-incidents.md) (`INC-02`, `INC-03`, `ALT-AUDIT-TAMPER`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-06`).

## 4. API, Wire & Data Contracts
- **Transactional Audit Atomicity (`Transactional Audit Atomicity`):** Every administrative state mutation (creating/revoking keys, approving plugins, updating policies, toggling kill-switches) must append its `audit_events` row **inside the exact same database transaction (`Tx`)** as the domain mutation — guaranteeing no configuration change can ever commit without its forensic audit trail.
- **Per-`Org` & `SYSTEM` Chain Genesis (`Genesis Hash`):** Entry `seq_no = 1` of each chain binds `prev_hash = SHA-256("tanpopo:audit:genesis:v1:" + chain_key)` where `chain_key = org_id` (or `"SYSTEM"` when `org_id IS NULL`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-30` & `OPS-AC-08` Tamper Detection):** Given 500 interleaved audit rows across `Org A`, `Org B`, and `"SYSTEM"`, When running `VerifyAuditChain` across all 3 chains, Then `100%` validate cleanly; when triggers are temporarily bypassed in a test harness to mutate 1 JSON character or delete `seq_no = 250` directly in the raw SQLite file, `VerifyAuditChain` pinpoints the exact tampered sequence (`seq_no = 250`) and fires `ALT-AUDIT-TAMPER`.
- **`AC-2` (`OPS-AC-03, 04` 4-Level Kill-Switch `< 1s`):** Given 8 active SSE streams across 2 Plugins and 2 Projects, When triggering an `ExtensionInstallation` Kill-Switch targeting `Plugin 1`, Then all 4 streams using `Plugin 1` terminate within `< 1 second` while all 4 streams using `Plugin 2` continue streaming uninterrupted.
- **`NEG-1`:** Executing `DELETE FROM audit_events` or `UPDATE audit_events SET actor_id = 'x'` over the application DB connection → immediately aborted by the DB immutability trigger.
- **`NEG-2` (`Transactional Audit Rollback`):** Injecting a simulated `audit_events` write failure → rolls back the enclosing `Virtual Key` creation or `Plugin` activation transaction completely with zero silent state drift.

## 6. Test Commands & Required Evidence
- Pure-Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/security/audit/... ./internal/security/killswitch/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/security/audit/... ./internal/security/killswitch/...`)*
- Data-race (`-race`): `go test -v -race ./internal/security/audit/... ./internal/security/killswitch/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** No role (`ORG_OWNER`, `OPERATOR`, `SECURITY_REVIEWER`) can delete rows from or disable `audit_events`; lifting an active `Kill-Switch` requires appending a new audited event with a non-empty mandatory `reason`.
- **Rollback:** Additive schema `0010_audit_killswitch.sql`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (`CR-06` per-`Org` `JCS` hash-chain lock), `TP-IMP-0009`, `TP-IMP-0011`, and `TP-IMP-0022` are complete.
- **DoD:** `100%` green across `CT-30`, `AUTH-06`, and `OPS-AC-03, 04, 08` under `-race`; passes `REQUIRES_STRONG_REVIEW`.
