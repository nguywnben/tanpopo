# QA-OBS-014 — FinOps budget empty state differed across repeated sessions before CP78 writes

- **Classification:** Unresolved test-state observation; not a confirmed product defect
- **First evidence:** CP78

## Observation

In the same QA project `qa-002-a2a-project / dev`, the first real-browser FinOps capture showed `Budget ceilings 0` and “No budgets configured” (`evidence/CP78-finops-initial.png`). A later pre-submit capture in the same exploratory phase showed one Project/MONTHLY budget row under that scope (`evidence/CP78-budget-form-ready.png`). The first capture has no saved API response. The later capture preceded a create-form submission and the post-run API history recorded a version-1 1,000,000-micro-USD row followed by a version-2 row with the same limit. Because no authenticated API snapshot was preserved between those two UI states, this test cannot establish whether the initial view was transient, the row existed before the first capture, or another event occurred.

The 1,000,000-micro ceiling remained in the isolated QA project; no usage was sent. No claim is made about product stability or data loss. The clean new-project path in CP78 used a separate project and has its own evidence.

## Follow-up

Reproduce with request logging from page entry through completed budget load, recording the selected project ID, every `GET /admin/v1/budgets?project_id=...`, and persisted state before any write. Keep this observation separate from DEF-024's effective ancestor-budget visibility defect until repeatable evidence establishes the relationship.
