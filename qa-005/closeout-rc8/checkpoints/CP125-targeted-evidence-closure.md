# CP125 — TEEP-00 targeted evidence closure

**Frozen source:** `3fb156bb51ab462ec5ae46a8bcb213f1a0714d7c` (RC8)
**Evidence branch:** `qa/qa-005-closeout-rc8`
**Prior checkpoint:** CP124 / `dd39f85cbd1affa7a52387fb577a0892a479f8d1`
**Verdict:** **BASELINE_REMEDIATION_REQUIRED**. This is targeted evidence closure only; QA-005 is not declared complete and GATE-REL-07 remains **BLOCKED**.

## Results

- **DEF-081:** Confirmed product failure in the PostgreSQL global extension quarantine path. With a fresh runtime, least-privilege PostgreSQL app role and healthy three-primary Valkey Cluster, both OpenAI-compatible and Ollama quarantine returned HTTP 503 `quarantine_kill_switch_failed` and left state unchanged. A temporary Registry diagnostic surfaced `storage: resource not found`. Read-only RLS visibility checks showed the global instance-operator scope could see its role binding and installation but not its organization-bound principal. The corrected topology, healthy cluster status and restart do not explain this result. The emergency barrier was not bypassed. See [diagnosis](../evidence/targeted-closure/postgres-quarantine-diagnosis.json).
- **DEF-082:** BLOCKED because no quarantine was committed; unquarantine and badge freshness cannot be assessed.
- **DEF-083:** PASS (scoped). Actual HTTP clients consumed native Ollama NDJSON and unified OpenAI-compatible SSE. Exact and estimated usage trailers were observed on successful responses; an interrupted unified stream returned an estimated trailer with the upstream error event. Durable settlement and budget readback were checked. See [stream responses](../evidence/targeted-closure/postgres-stream-results.json) and [readback](../evidence/targeted-closure/readonly-operations.json).
- **DEF-080:** PASS (scoped). Owner saw five existing keys; a pure FinOps Viewer received 403 and the Console explicitly represented the inventory as restricted. Visual screenshots were inspected.
- **DEF-084:** PASS (scoped). A real active long-ID alert remained within 320px and 375px document widths; the ID wrapped and the remediation action remained reachable in inspected screenshots.
- **DEF-050:** The original single-primary warning acceptance passes: the sole target was named, the route persisted, and no fallback mislabel occurred. The separate multi-target generic-copy concern remains PARTIAL. DEF-083 streaming is not folded into DEF-050.
- **Secret credential migration:** PASS for one deterministic encrypted credential envelope. Full fixture migration verified 55 tables, 188 rows, one secret envelope and 83 audit events; authorized PostgreSQL probe and local-fixture inference succeeded. No plaintext credential appears in evidence.
- **DEF-041/072/078:** Bounded read-only checks only. They remain PARTIAL; destructive erasure/recovery, expected-node loss and KEK rewrap, and budget/Chargeback rollup parity were not exercised.

## Historical reconciliation

CP123's historical counts remain 61 scoped PASS, 15 PARTIAL and three FAIL. The exact 15 CP123 PARTIAL IDs are DEF-004, DEF-020, DEF-027, DEF-028, DEF-036, DEF-041, DEF-043, DEF-044, DEF-048, DEF-049, DEF-050, DEF-060, DEF-072 and DEF-077–078 (15 total). This closure gives bounded checks to DEF-041/072/078 and a scoped single-target PASS within DEF-050; all remain PARTIAL overall. Eleven historic partial findings received no new execution. The overlay preserves baseline disposition separately from current targeted status.

No new DEF-087+ or PG-015+ entry is assigned. Existing PG-001–PG-014 and all earlier historical evidence remain preserved. No production implementation/specification changed. No paid provider, production key, signature, tag, release or merge was used.

## Remaining material assurance limits

Successful extension quarantine/unquarantine, adapter activation/re-enable/rollback, multi-node failure/recovery, destructive tenant erasure, KEK rotation, DEF-078 UI/rollup/boundary behavior, broad streaming failures, Windows runtime, native Safari/iOS, remaining partial acceptance criteria and full-product Alpha/Beta coverage remain incomplete. They are recorded as residual assurance gaps unless a failing result is established. The only newly confirmed product behavior in this closure is tracked under existing DEF-081.
