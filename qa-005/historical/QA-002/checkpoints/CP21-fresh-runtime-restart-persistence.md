# CP21 — Fresh runtime restart and persisted state

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite, `127.0.0.1:18086`; same binary, data directory and master-key file before and after restart.  
**Implementation changes:** none.  
**Release gate:** GATE-REL-07 remains **BLOCKED**.

## Restart and probes

Stopped the disposable 18086 server cleanly and confirmed the port no longer responded, then restarted the exact QA-002 binary with the same data directory and master-key file. After restart:

- `GET /healthz` returned 200 `{"status":"ok"}`.
- `GET /readyz` returned 200 `{"status":"ready"}`.
- `GET /admin/v1/bootstrap/status` returned 200 and `initialized:true`.
- The source-default version was reported as `0.1.0-dev`. This binary had no release linker version/commit/date injection, as expected; this is not a defect and does not prove the RC4 DevBox artifact.

## Real browser verification

Using a new Chromium session, the synthetic Owner logged in after the restart. The QA organization and project were still present. Overview retained the one failed Playground request and showed 100% error, zero settled spend and zero holds. Traces still displayed the same request ID and `no_compliant_deployment_available`. Screenshots `evidence/CP21-postrestart-overview.png` and `CP21-postrestart-traces.png` were visually inspected.

**Result:** single-node SQLite clean restart persistence **PASS** for this owner/workspace and metadata trace scenario. This does not cover abrupt process/power loss, database corruption, PostgreSQL/Valkey, multi-node consistency, upgrade rollback or restore recovery.

Machine-readable sanitized evidence: `evidence/CP21-restart-persistence.json`. No source code or product dependencies changed. RC4 DevBox artifact verification remains UNVERIFIED.
