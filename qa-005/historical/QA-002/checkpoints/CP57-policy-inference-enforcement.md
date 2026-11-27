# CP57 — Residency policy enforcement on live synthetic inference

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Result:** PASS for one healthy ON_PREM route under EU-only organization policy, fail-closed rejection before egress, and recovery after policy restore. No new defect classified.
**Release gate:** `GATE-REL-07 BLOCKED`.

## Journey and outcome

Created one temporary PROVIDER key scoped to the CP43 secondary project/environment. From a clean browser context with only its bearer credential, sent a non-streaming request through the real `qa002-cp52-failover-route` to the isolated loopback provider. Under the restored baseline policy (EU, US, APAC, ON_PREM), the request returned HTTP 200 and the fixture call count advanced 4 → 5. Its persisted trace selected the primary deployment in `ON_PREM`.

Changed the Organization residency policy to EU through the real Policies UI. Effective-policy API reads returned EU at Organization, Project, and Environment tiers. A second call to the same route returned HTTP 503 `no_compliant_deployment_available`; the fixture count stayed at 5. The persisted failure trace had no deployment or data region, showing the request was rejected before provider dispatch. The trace list labels this failure with `/v1/chat/completions` rather than the route slug; this is recorded as evidence, not a new defect because the canonical error and no-egress behavior are consistent.

Restored all four organization regions through the UI and re-read effective state at all three tiers. The same key and route returned HTTP 200 and the fixture count advanced 5 → 6. Revoked the key through Access & Keys and verified authenticated Admin API state `REVOKED`, version 2. Screenshots were opened and visually inspected. No live Google/provider credential was used; the only upstream was the synthetic loopback fixture.

## Evidence

- [`../evidence/CP57-policy-enforcement.json`](../evidence/CP57-policy-enforcement.json) — request outcomes, scope resolution, fixture deltas, metadata-only trace rows, policy writes and final key state.
- [`../evidence/CP57-policy-key-created.png`](../evidence/CP57-policy-key-created.png) — scoped temporary key in the real Console.
- [`../evidence/CP57-policy-enforced-eu.png`](../evidence/CP57-policy-enforced-eu.png) — visually inspected effective EU policy before the rejected request.
- [`../evidence/CP57-policy-restored.png`](../evidence/CP57-policy-restored.png) — visually inspected restored policy and effective value.
- [`../evidence/CP57-policy-key-revoked.png`](../evidence/CP57-policy-key-revoked.png) — visually inspected final key revocation.

## Consolidated status and remaining coverage

- **Historical DEF-001–015:** no CP57 retest; retain the current QA-002 status: DEF-001–004 and DEF-006–015 verified fixed only for their scoped scenarios; DEF-005 partial with redirect, DNS rebinding, alternate-runtime and IPv4-mapped-address contract gaps.
- **Current new defects:** DEF-016–028 remain confirmed open; no new finding was added in CP57. QA-OBS-006 recurred: Data Plane `req_…` IDs differ from trace list `trc_…` IDs; cross-link expectations remain unresolved.
- **Coverage completed:** real Data Plane positive control, org-to-environment residency inheritance, pre-egress policy rejection with persisted error trace, recovery after policy restore, and key revocation with persistent-state verification.
- **Blocked:** none in CP57; CP53 webhook registration/delivery remains blocked by unavailable Operator credentials.
- **Remaining:** alternate data regions/route targets, project/environment-specific policy changes, simultaneous constraints, concurrent requests, other roles/browsers, live provider integrations, and the wider QA-002 matrix remain unverified. See [`../STATUS-SUMMARY.md`](../STATUS-SUMMARY.md) and [`../COVERAGE-MATRIX.md`](../COVERAGE-MATRIX.md).

No Tanpopo implementation was changed. QA-002 remains in progress.
