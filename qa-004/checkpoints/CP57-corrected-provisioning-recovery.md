# CP57 — Corrected project/environment submissions recover after conflicts

**Status:** PASS (scoped same-form recovery from duplicate slug conflicts); DEF-055 remains FAIL (scoped). **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; isolated artifact `45581179274` at `127.0.0.1:18132`.
**Product source:** unchanged.

## Recovery journey

As the synthetic Org Owner, I repeated both duplicate-slug failures in real Chromium and corrected the slug in the still-open form without leaving the workflow.

- Environment: duplicate slug returned HTTP 409. Changing only the slug to `qa004-cp57-env-recovery` and resubmitting in the same form returned HTTP 201. The Console selected the created environment. Authenticated collection readback found the new row and ID; it again returned `name` equal to slug, independently showing DEF-055 remains. `environment.created` #108 targets that new record.
- Project: duplicate organization slug returned HTTP 409. Changing only the slug to `qa004-cp57-project-recovery` and resubmitting in the same form returned HTTP 201. The Console selected the recovered project and showed its human name; authenticated project readback preserved the distinct name and slug. `project.created` #109 targets the new project.

The prior failed attempts created no extra project/environment rows or `*.created` records; each corrected submission created exactly one resource and one correctly targeted event. The Owner used the actual Audit Log **Verify hash chain** action; the UI showed RFC 8785 chain integrity across 110 events. All screenshots were visually inspected.

## Evidence

- [Environment after corrected resubmission](../evidence/CP57-environment-corrected-recovery.png)
- [Project after corrected resubmission](../evidence/CP57-project-corrected-recovery.png)
- [Recovered environment create audit event](../evidence/CP57-audit-environment-recovery.png)
- [Recovered project create audit event](../evidence/CP57-audit-project-recovery.png)
- [Verified audit chain](../evidence/CP57-audit-chain-verification.png)
- [Sanitized response, readback, and audit results](../evidence/CP57-provisioning-recovery-results.json)
- [Initial conflict evidence](CP56-duplicate-project-environment-create-rejection.md); [DEF-055 details](../reports/DEF-055-environment-display-name-not-persisted.md).

## Coverage

- **PASS (scoped):** after server-side duplicate conflicts, correcting the slug and retrying in the same form succeeds, persists one resource, switches the active scope, and records a correctly targeted success event.
- **FAIL (scoped):** environment readback/picker still substitutes slug for the submitted display name (DEF-055).
- **UNVERIFIED:** cancel/reopen recovery, transient transport/timeouts, storage failures, concurrent duplicate races, authorization failures, automated retry/double-submit, and other roles/tenants.

This checkpoint does not close provisioning or overall QA coverage.
