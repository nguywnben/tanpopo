# CP55 — Project/environment provisioning, display-name retest, and audit events

**Status:** PASS (scoped project create/readback and successful provisioning audit events); FAIL (scoped environment display-name readback, reproduces DEF-055); UNVERIFIED for failure atomicity and broader role/platform paths. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; isolated artifact `45581179274` at `127.0.0.1:18132`.
**Product source:** unchanged.

## Real workspace provisioning journey

As an isolated synthetic Org Owner, I opened the actual scope picker, inspected the project/environment choices, and created project `QA004 CP55 Ops Billing` with slug `qa004-cp55-ops-billing` through **New project**. The created project became active in the picker and showed its human name. `GET /admin/v1/projects?limit=100` returned HTTP 200 with the same name and slug. The Security → Audit Log exact `project.created` filter showed event #95 targeting the new project ID.

In that project I created an environment with name `QA004 CP55 Staging West` and slug `qa004-cp55-staging-west`. The real POST returned HTTP 201 and echoed the distinct name and slug. On the next GET of the selected project's environment collection, the row's `name` was the slug, not the submitted display name. After a hard refresh, the scope picker continued to show `qa004-cp55-staging-west`. This is a fresh independent reproduction of DEF-055 on the frozen RC6 artifact; no new defect ID is warranted.

The exact `environment.created` audit filter showed event #102 targeting the new environment ID. The Owner then used **Verify hash chain**; the UI reported “Cryptographic RFC 8785 JCS chain intact across 104 events.” Filtered audit screens and the chain result were visually inspected.

## Findings and classification

- **PASS (scoped):** project display name and slug survive creation and list readback; its `project.created` event targets that resource.
- **FAIL (scoped, DEF-055):** environment POST initially echoes the human label, but collection readback and refreshed workspace scope selection replace it with the slug.
- **PASS (scoped, DEF-007 success path):** project and environment creates each emit a visible, correctly targeted audit event; current chain verifies intact. This corroborates existing successful-path results and does not test failed/partial transaction audit semantics.
- **UNVERIFIED:** duplicate/invalid inputs, concurrent creation, failure atomicity, rollback behavior, rename/delete, other roles/tenants, second browser engines, and populated environment-bound resources.

## Evidence

- [Scope picker before provisioning](../evidence/CP55-project-scope-picker.png), [project create form](../evidence/CP55-project-create-form.png), [project active after create](../evidence/CP55-project-created.png).
- [Environment create form](../evidence/CP55-environment-create-form.png), [immediate post-create overview](../evidence/CP55-environment-created.png), [scope picker after hard refresh](../evidence/CP55-environment-scope-after-refresh.png).
- [Project create audit filter](../evidence/CP55-audit-project-created.png), [environment create audit filter](../evidence/CP55-audit-environment-created.png), [verified chain](../evidence/CP55-audit-chain-verification.png).
- [Sanitized request/readback/audit results](../evidence/CP55-provisioning-results.json).
- DEF-055’s first independent proof remains in [CP19](CP19-environment-name-persistence.md) and its [standalone report](../reports/DEF-055-environment-display-name-not-persisted.md).

All captured screens above were visually inspected during browser interaction. No external provider, credential, or production tenant was used. The synthetic project/environment remain in the isolated database for repeatability.
