# QA-OBS-005 — Project budget created after spend starts at zero

**Classification:** Business-contract question; not a confirmed defect.  
**Severity:** Not assigned.  
**Baseline/runtime:** exact QA-002 source baseline; isolated SQLite at `127.0.0.1:18086`.  
**Evidence:** CP26 budget creation, synthetic inference, and persisted state.

## Facts

Before this project budget existed, two synthetic provider settlements for the project totaled 2 micro-USD during the same runtime/month. The owner created a monthly project budget with a zero limit, then raised it to 1 micro-USD. At creation/update, the new project ledger reported zero settled. A subsequent one-micro synthetic Playground request returned 200, reached the loopback provider fixture once, and settled exactly 1 micro-USD. A second call at the exact cap was rejected; after raising the cap to 2 micro, a second synthetic request succeeded. At the end, project-wide exact usage was 4 micro-USD while the budget ledger showed 2 micro settled against a 2-micro cap. No active hold remained. Persistent evidence: `../evidence/CP26-budget-enforcement-recovery-state.json`; screenshots include `CP26-finops-after-budget-test.png`.

## Expected versus actual depends on contract

If a monthly hard limit covers all usage in the current calendar month, the expected starting ledger would include prior same-month settlements and the 1-micro request should be rejected. Actual behavior starts tracking that scope from ledger creation and accepts 1 micro. If budgets are explicitly prospective from their creation time, the behavior may be intentional, but the Console currently communicates only `MONTHLY` and does not state a budget activation time/window.

## Follow-up

Clarify whether scope limits apply retroactively to the current period or only from configuration time. If prospective, surface the effective start timestamp and make remaining-cap calculations unambiguous. If period-wide, reconcile existing settlements when creating a scope ledger. Re-test concurrent holds and all four scope tiers. This observation does not claim a defect while the business contract remains unspecified.
