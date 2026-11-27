# Product-gap handoff — Gemini 4 Carbon / TEEP-01

This is an additive observation to PG-001–PG-013. It does not revise frozen specifications or create a defect.

## PG-014 — Team Admin Security screen mixes denied and empty states

- **Observed:** In the RC8 real Console as a Team Admin, Security displayed an `insufficient role permission` banner while also showing “Pending MCP approvals 0” and “Approval queue clear,” and kept a visible Trigger kill switch button. API reads/operations were denied with 403, so no unauthorized approval or kill-switch operation succeeded.
- **User impact:** A user may read the zero/clear copy as verified inventory despite lacking permission, or spend time on controls that cannot complete.
- **Evidence:** [screenshot](evidence/screenshots/rc8-teamadmin-security-denied-state.png); [closeout report](REPORT.md#new-defects-and-product-gaps).
- **Classification:** No accepted UI specification was found that requires this role-specific presentation. Treat as usability and permission-feedback research input, not a defect. TEEP-01 can decide whether to hide controls, show a dedicated denied state, or provide explicitly labeled partial inventory.
