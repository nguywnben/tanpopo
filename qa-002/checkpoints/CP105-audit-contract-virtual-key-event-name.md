# CP105 — Virtual Key issuance audit-event contract

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`<br>
**Runtime:** isolated QA server `18084`<br>
**Role/browser:** authorized Org Owner, real Chromium at 1440 × 1000<br>
**Product implementation:** unchanged

## Result

Confirmed **DEF-037 Low**: the published action name `virtual_key.issued` does not match the actual emitted/filterable action `virtual_key.created`. This caused a documented-name filter to return zero, while the actual-name filter returned the real issuance event. The event was not missing: it retained actor, key target and scope metadata; the matching revoke event existed; the hash chain remained intact across 511 events.

## Reproduction and evidence

1. The authorized Console issued a fresh A2A-only Virtual Key in the selected project/environment. Its one-time secret stayed in memory and was not written to evidence.
2. On Security → Audit Log, filtering `virtual_key.issued` showed zero events. The corresponding authenticated API filter returned HTTP 200 with zero matches.
3. Filtering `virtual_key.created` showed 12 events and included the test key at sequence 499. The event carried the owner actor, Virtual Key target, `allowed_planes:A2A`, and environment metadata. The action-filter API returned the same test key.
4. The key was revoked through the UI. `virtual_key.revoked` was recorded at sequence 505. The Audit Log showed the issue and revoke entries.
5. The UI and API verified the RFC 8785 JCS chain intact across 511 events.

The four evidence screenshots listed below were visually inspected. The first shows “0” and “No audit events match this filter”; the second shows the actual action and sequence 499; the chain screenshot shows the successful integrity banner and the issue/revoke rows; the overview screenshot shows both lifecycle actions together. The API evidence is sanitized and contains no key secret.

## DEF-007 reconciliation

CP105 verifies that the successful Virtual Key provisioning path produced a durable, chain-valid audit event and that revoke also produced a durable event. It therefore does not reproduce a missing event for this success path. It does reveal that the emitted issue action conflicts with the published `virtual_key.issued` contract. DEF-007 remains open/partial: event append failures, rollback/atomicity, populated organization erasure and storage-fault cases remain UNVERIFIED. See [DEF-037](../reports/DEF-037-virtual-key-audit-event-name-contract.md).

## Evidence inventory

- `../evidence/CP105-audit-name-contract.json` — exact-action API filters, UI-observed actions and chain result.
- `../evidence/CP105-audit-key-events.json` — sanitized audit UI screen text.
- `../evidence/CP105-audit-filter-documented-issued.png` — zero results for documented action; visually inspected.
- `../evidence/CP105-audit-filter-actual-created.png` — matching issued key under actual action; visually inspected.
- `../evidence/CP105-security-audit-key-events.png` — issue and revoke event inventory; visually inspected.
- `../evidence/CP105-audit-chain-after-key-lifecycle.png` — intact 511-event chain and lifecycle rows; visually inspected.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain only their scoped QA-002 outcomes; CP104 expanded DEF-011. DEF-005 remains PARTIAL. QA-001 PASS results are not inherited.
- **Historical defects still open or unverified:** DEF-007 remains partial with failure atomicity/populated erasure UNVERIFIED. DEF-016–036 remain open as recorded; no disposition changed in CP105. DEF-023 remains the latest independent workspace-scope reconfirmation (CP101).
- **New defects:** DEF-037 Low, confirmed event-name mismatch. No evidence of a missing record, secret exposure or chain failure.
- **Coverage completed:** UI and API action filtering, issue/revoke success-path event presence and metadata, separate exact documented/emitted name checks, and chain verification; real screenshots visually inspected.
- **Coverage blocked / remaining:** audit append fault injection, state-transition atomicity, populated-org erasure, cross-tenant audit isolation, downstream event consumers and non-owner access remain open. Full role/browser/platform and product coverage remains incomplete.
- **Recommended engineering handoffs:** reconcile DEF-037 against the published audit vocabulary; retain DEF-007 handoff for append failure atomicity and populated erasure. Continue DEF-005 without weakening SSRF or authorization controls.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
