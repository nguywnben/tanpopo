# CP29 — Virtual Key issuance audit event name and chain integrity

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; authenticated synthetic Org Owner  
**Disposition:** DEF-037 discrepancy **reproduced on RC5** from persisted synthetic issue/revoke events; status **PARTIAL** because no fresh issuance transaction was created in this retest. The Console verifier reported the chain intact across 423 events. `GATE-REL-07` remains **BLOCKED**.

## Audit contract retest

1. Authenticated `GET /admin/v1/audit-events?action=virtual_key.issued&limit=100` returned HTTP 200 and zero records.
2. `GET /admin/v1/audit-events?action=virtual_key.created&limit=100` returned HTTP 200 and two records with `virtual_key.created` actions, targets, and actor IDs. `virtual_key.revoked` returned HTTP 200 and two corresponding revoke events. Sanitized API results: [evidence JSON](../evidence/CP29-def037-audit-results.json).
3. In the real Console, opened Security → Audit Log. Entered `virtual_key.issued` in “Filter by action”: the UI displayed count 0 and “No audit events match this filter.” The screenshot was visually inspected: [documented-name filter](../evidence/CP29-audit-filter-issued.png).
4. Replaced it with `virtual_key.created`: the UI showed two records at sequences 261 and 254. Screenshot was visually inspected: [emitted-name filter](../evidence/CP29-audit-filter-created.png).
5. Clicked the Console’s **Verify hash chain** control. It displayed “Hash chain verified” and “Cryptographic RFC 8785 JCS chain intact across 423 events.” Visually inspected evidence: [chain verification](../evidence/CP29-audit-chain-ui-verification.png).

The event records already existed in the RC5 synthetic database from earlier QA actions. This retest independently queried their exact API filters, rendered both UI filter outcomes, and invoked the RC5 chain verifier; it did not issue or revoke a new key. The attempted one-time-key flow was not completed, and no new key or secret was created or captured.

## Expected versus actual and impact

- **Expected:** `virtual_key.issued` is the documented issuance action, and the Console/API filters return issuance events under that name; alternatively, an intentional contract revision is consistently documented and exposed.
- **Actual:** issuance records use `virtual_key.created`; the documented-name filter returns zero while the emitted-name filter finds both records. Revoke records use `virtual_key.revoked`. The chain itself verifies intact.
- **Impact:** a security reviewer or downstream consumer filtering on the documented action can overlook key issuance. No missing audit rows or chain break was observed in these saved events; no plaintext key secret appeared in the audit detail inspected.

## Status and limits

- DEF-037 changes from UNVERIFIED to **PARTIAL — contract mismatch reproduced**. Full acceptance still requires a fresh issue/revoke lifecycle, canonical event naming/documentation reconciliation, actor/target/scope metadata checks, no-secret verification, pagination, and audit failure atomicity.
- FT-12 and SCR-14 remain PARTIAL. This adds the UI/API action-name comparison and one successful Console chain verification.
- No new defect filed; this is the historical DEF-037. No key was created, revoked, or mutated in this phase.
- Historical register totals: 17 VERIFIED FIXED for scoped RC5 scenarios, 7 PARTIAL (DEF-005, DEF-007, DEF-029, DEF-032, DEF-035, DEF-037, DEF-039), and 15 UNVERIFIED.
- QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
