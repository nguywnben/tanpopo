# CP96 — Idempotency trace and chargeback reconciliation

**Baseline:** exact frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Outcome:** PASS (scoped) for no duplicate settlement on replay; FAIL (existing DEF-051/052 confirmed and extended) for trace/chargeback consistency. No new defect ID assigned.
**Gate:** GATE-REL-07 remains BLOCKED. Product source unchanged.

## Persistent-state reconciliation

Using the two CP94 default/dev keys, I read back the persisted trace rows by Virtual Key and grouped chargeback rows by key after completing the request lifecycle. The two actual first-dispatch requests each created one estimated usage charge of 1 micro-USD in FinOps. Replaying the idempotent request did not add another settled request; the changed-payload 422 also had no charge. This agrees with the two actual upstream dispatches recorded in CP94.

The Traces screen contains five records for the five accepted/replayed/mismatch HTTP attempts: three key-A successes, one key-A 422 `idempotency_key_reused_mismatch`, and one key-B success. Every row is labeled `Exact`; costs are `$0` in trace readback. The two key dispatch traces show `qa004-mock`, while cache-replay/error rows show `/native/{dialect}`. FinOps groups both keys as 0 Exact / 1 Estimated request at 1 micro-USD each. Thus idempotency did not double-charge, but trace metering and route projection remain inconsistent with chargeback, extending DEF-051/052. The 422 was zero-settlement as expected.

I visually inspected the real [Traces screen](../evidence/CP96-idempotency-traces-ui.png) and [FinOps → Chargeback screen grouped by Virtual Key](../evidence/CP96-idempotency-finops-ui.png). Sanitized persisted trace and chargeback readback is in [results](../evidence/CP96-idempotency-usage-reconciliation.json); the original call/replay/upstream evidence is in [CP94](CP94-native-idempotency-per-key-replay.md).

## Disposition and limits

**PASS (scoped):** one successful first dispatch per VKEY, no duplicate upstream dispatch or FinOps charge for a replay, mismatched payload rejected with no charge, and post-revoke replay denied. **FAIL (existing defects):** trace accuracy/cost does not reconcile with the estimated usage settlement; route labels also differ between dispatch and replay trace rows. This is additional evidence for DEF-051 and DEF-052, not a new defect.

This verifies only one native synchronous route on the isolated SQLite runtime. Concurrency, restart, distributed storage, usage counter fidelity beyond this synthetic response, exact cost models, and other dialects remain UNVERIFIED. QA continues; **GATE-REL-07 remains BLOCKED**.
