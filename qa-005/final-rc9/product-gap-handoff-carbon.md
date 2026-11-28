# Product and technology intelligence handoff — Gemini 4 Carbon

This handoff preserves, without reclassification, the existing [PG-001–PG-013 register](../PRODUCT-GAP-REGISTER.md) and [PG-014 observation](../closeout-rc8/product-gap-handoff.md). None are newly classified as implementation defects by this bounded Round 6 verification.

## Architecture research observation ARCH-OBS-01 — organization-wide principal and audit lookups

**Observation:** Round 6 authorization and quarantine resolution crosses a global instance-operator identity with organization-bound installation, principal, role-binding, audit and containment state. The fix is verified under PostgreSQL FORCE RLS, but organization-wide principal and audit lookups are a potential scale and operational-cost concern as tenant count and audit volume increase.

**Research questions for TEEP-01:**

- What are the query plans and index requirements as organization, principal, installation, kill-switch and audit-chain counts grow?
- Which lookups must span organizations, and which can be resolved through an explicitly scoped system identity without weakening tenant RLS?
- How do audit verification, global emergency inventory and operator resolution behave under large tenant cardinality and partitioned audit history?
- What are the latency and failure boundaries when containment depends on organization and system audit partitions at once?

**Classification:** architecture research observation, not a confirmed defect and not a specification change.

## Setup friction observed during QA

An unverified bundled adapter was explicitly approved in project scope, but project-scoped provider connection creation correctly returned `org_trust_policy_denied` until the ORG_OWNER explicitly enabled the organization's trust policy. QA configured that policy through the authorized organization policy API, then connection creation/probes passed. Preserve this as a TEEP-01 usability and governance workflow question; this mission did not establish that the UI lacks an approved configuration path and does not assign a PG or DEF number.

## Evidence and scope

The RC9 provider, extension lifecycle and active switch screenshots are under [evidence/screenshots](../evidence/screenshots/). Earlier product observations and their original evidence remain in the historical QA tree and the product gap register.
