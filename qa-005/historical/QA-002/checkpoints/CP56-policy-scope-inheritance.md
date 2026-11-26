# CP56 — Hierarchical policy scope and residency inheritance

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for organization → project → environment residency inheritance; child editors kept the restriction fail-closed. No new defect classified.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Actions and observed state

Using the synthetic Org Owner in real Chromium at `QA002 CP43 Secondary Project / qa002-cp43-env2`, opened Routes & Policies → Policies and visually inspected the effective-policy table and editor. The editor allowed choosing Organization, Project, and Environment. Independent authenticated `GET /admin/v1/policies/effective` requests for organization, project, and environment initially returned all four regions (EU, US, APAC, ON_PREM); versions were organization 2, project 1, and environment 1.

Through the UI, temporarily restricted the organization policy to EU. The UI showed “Saved Organization policy.” All three effective-policy API reads then returned only EU. Switching the editor to Project and Environment showed one EU checkbox each; US, APAC, and On-prem were absent from those child editors. No child write was attempted because the UI did not expose any broader-region choices. This is consistent with the documented monotonic child-policy restriction and did not weaken the security boundary.

Restored the organization policy through the same UI. The final organization, project, and environment effective-policy reads again returned EU, US, APAC, and ON_PREM. Organization policy version advanced from 2 → 3 → 4; project/environment versions remained 1. Captured UI responses showed exactly two successful writes to the organization policy ID and no project/environment writes. No inference or provider request was sent during this policy-only test.

## Evidence

- [`../evidence/CP56-policy-inheritance.json`](../evidence/CP56-policy-inheritance.json) — before/restricted/restored effective API results, child-editor options, versions, and policy-write responses.
- [`../evidence/CP56-policy-effective.png`](../evidence/CP56-policy-effective.png) — initial effective-policy table and editor, visually inspected.
- [`../evidence/CP56-policy-scope-organization.png`](../evidence/CP56-policy-scope-organization.png), [`../evidence/CP56-policy-scope-project.png`](../evidence/CP56-policy-scope-project.png), and [`../evidence/CP56-policy-scope-environment.png`](../evidence/CP56-policy-scope-environment.png) — scope selection and initial values.
- [`../evidence/CP56-policy-org-restriction-saved.png`](../evidence/CP56-policy-org-restriction-saved.png) — saved EU-only organization policy and effective EU value.
- [`../evidence/CP56-policy-project-inherited.png`](../evidence/CP56-policy-project-inherited.png) and [`../evidence/CP56-policy-environment-inherited.png`](../evidence/CP56-policy-environment-inherited.png) — inherited EU-only state in both child editors.
- [`../evidence/CP56-policy-child-relaxation-result.png`](../evidence/CP56-policy-child-relaxation-result.png) — Environment editor showed only EU as available; no widening write was sent.
- [`../evidence/CP56-policy-org-restored.png`](../evidence/CP56-policy-org-restored.png) — restored four-region organization policy, visually inspected.

## Coverage and limits

**PASS:** real UI edits at Organization scope, effective state resolved at all three tiers, restrictive inheritance reflected in both child editors, no child policy mutation, and persisted organization restoration.
**UNVERIFIED:** policy enforcement against a live inference route, model allowlist and plugin-trust inheritance, RPM/TPM/concurrency policy enforcement, guardrail attach/confirmation, optimistic-lock conflict recovery, other roles, and Routes & Policies behavior in organization-only workspace context. CP43 previously observed the route/policy screen's org-only guard; CP56 confirms an owner can choose Organization scope while a project/environment workspace is active. The org-only workflow contract remains unresolved.

No product implementation was changed. QA-002 remains in progress.
