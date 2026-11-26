# CP144 — FinOps and Virtual Key probe audit reconciliation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite, `127.0.0.1:18128`  
**Actor:** synthetic Org Owner  
**Product source modified:** no  
**Provider spend:** $0

## Outcome

- **PASS (scoped):** Authenticated audit API readback linked all seven synthetic keys used in CP137–CP140 to both `virtual_key.created` and `virtual_key.revoked`, using exact `target_id` equality. Actor attribution and event timestamps were present.
- **PASS (scoped):** The eight most recent relevant `budget.updated` events on the target project ledger recorded the cap adjustments and restoration performed in CP137–CP140, including the final 100-micro-USD value. Metadata exposed the `hard_limit_micros` and period.
- **PASS (scoped):** The actual Security → Audit Log UI visibly listed the same target-linked key and budget events. Clicking **Verify hash chain** displayed “Cryptographic RFC 8785 JCS chain intact across 853 events.” The screenshot was opened and visually inspected.
- **No new defect:** This only verifies audit linkage and chain integrity for these exercised mutations. It does not close historical DEF-007's provisioning failure/rollback questions or establish audit completeness for other mutation types.

## Reproduction

1. Sign in to the isolated exact-RC6 Console as the synthetic test-root Owner.
2. Query `/admin/v1/audit-events` with authenticated organization scope, filtered separately by `virtual_key.created`, `virtual_key.revoked`, and `budget.updated`.
3. Match the seven target IDs recorded in CP137–CP140 and the project's monthly budget ledger ID. Preserve only event metadata, target IDs, actor IDs, event sequence numbers and timestamps; do not query or store credentials.
4. Open Security → Audit Log and verify the actual event rows and hash-chain result in Chromium.

Sanitized event readback is [CP144 evidence](../evidence/CP144-finops-audit-results.json). The visually inspected screenshot is [CP144 Audit Log](../evidence/CP144-audit-chain-verified.png).

## Limits and status

The first unauthenticated API attempt ran before the post-login route settled and returned 401; it was discarded and not used as product evidence. The authenticated retry succeeded. Exact record matching confirms the targeted seven-key and budget event slices; no claim is made about the remainder of the 853-event chain beyond the UI's cryptographic verification result.

QA-004 remains ongoing. Historical defect results and new defect totals are unchanged. **GATE-REL-07 remains BLOCKED.**
