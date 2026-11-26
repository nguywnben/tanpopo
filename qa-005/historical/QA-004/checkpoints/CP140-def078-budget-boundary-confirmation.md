# CP140 — DEF-078 budget-boundary confirmation

**Date:** 2026-11-26  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated SQLite at `127.0.0.1:18128`  
**Outcome:** confirms DEF-078; no additional finding assigned.

## One-micro headroom boundary

Before the probe, the default monthly project budget was 100 micro-USD hard limit, exact 2,043, estimated 14 (total settled 2,057). The same project's Chargeback summary was exact 2,044, estimated 15 (total 2,059). Temporarily raised the project limit through the real FinOps UI to 2,058 micro-USD, leaving exactly one micro-USD headroom according to the budget ledger.

Created a disposable route-scoped Provider VKEY through Access & Keys and sent one cookie-free chat request to `qa004-loopback`. The request returned HTTP 200. Budget readback after settlement was exact 2,044 / estimated 14, totaling exactly the 2,058-micro limit; Chargeback readback was exact 2,045 / estimated 15, totaling 2,060. Thus the system accepted the one-micro request at ledger headroom, while displayed project usage ended two micro-USD above the configured hard limit. Screenshots were visually inspected.

Revoked the temporary key and restored the original project limit of 100 micro-USD. Final authenticated readback confirms the restored limit, zero active holds, exact 2,044, estimated 14, and the newly confirmed 2,045 / 15 Chargeback result. No external provider spend occurred.

This makes DEF-078's user impact concrete: a user can reach a budget boundary where the request is admitted based on ledger amounts but the displayed Chargeback total exceeds the configured ceiling. This does not quantify the long-term accumulation rate or identify the missing source settlement.

## Evidence

- [Before, cap, after, and cleanup API readbacks](../evidence/CP140-budget-headroom-results.json)
- [Budget UI at one micro-USD headroom](../evidence/CP140-budget-one-micro-headroom.png)
- [Chargeback UI after the accepted request](../evidence/CP140-chargeback-after-headroom.png)
- [Access & Keys UI showing the temporary key revoked](../evidence/CP140-key-revoked.png)
- [DEF-078 report](../reports/DEF-078-finops-summary-exceeds-monthly-budget-ledger.md)

Project limit remains 100 micro-USD. The disposable key was revoked and no secret was retained. DEF-078 remains Moderate FAIL pending engineering investigation and fresh-period regression. All other defect and historical counts are unchanged. QA-004 remains incomplete. **GATE-REL-07 remains BLOCKED.**
