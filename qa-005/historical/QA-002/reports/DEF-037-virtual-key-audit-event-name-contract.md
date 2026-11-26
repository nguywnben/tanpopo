# DEF-037 — Virtual Key issuance audit action differs from documented contract

**Severity:** Low  
**Status:** Confirmed  
**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Checkpoint:** [CP105](../checkpoints/CP105-audit-contract-virtual-key-event-name.md)

## Summary

The documented audit action for Virtual Key issuance is `virtual_key.issued`, but an actual Console-issued Virtual Key is recorded as `virtual_key.created`. The Security Audit Log action filter follows the emitted name: filtering for the documented action returns zero records, while filtering for `virtual_key.created` returns the new key event. This can cause consumers that use the documented action name to miss issuance events. The key issue event itself exists, its actor/target and scope metadata are present, its corresponding revoke event is recorded, and the hash chain verified intact. This is an event-vocabulary contract discrepancy, not a missing-event or chain-integrity finding.

## Reproduction

1. On isolated runtime 18084 at the frozen baseline, use the authorized Org Owner Console to issue a least-privilege A2A-only Virtual Key in the selected A2A project/environment. Do not persist or capture the one-time secret.
2. Open Security → Audit Log and filter by `virtual_key.issued`. The UI reports zero matching events. `GET /admin/v1/audit-events?action=virtual_key.issued&limit=100` returns HTTP 200 with zero matching records.
3. Filter by `virtual_key.created`. The UI shows 12 records, including the newly issued key at sequence 499. `GET /admin/v1/audit-events?action=virtual_key.created&limit=100` returns HTTP 200 and includes the same key target.
4. Revoke that key through Access & Keys. The log contains `virtual_key.revoked` at sequence 505.
5. Verify the chain. The UI and API report an intact chain across 511 events.

## Expected and actual

- **Expected:** issuance uses the published `virtual_key.issued` action, or the documented contract is intentionally revised and all supported API/UI consumers use the chosen canonical action consistently.
- **Actual:** the docs specify `virtual_key.issued`; the implementation emits and filters `virtual_key.created`. The documented action filter yields no records although the issuance record is available under the emitted action.

## Impact and affected workflows

Audit reviewers, event-filter users and downstream integrations built against the published action name can overlook key issuance during a security review or fail to trigger issuance-specific monitoring. The evidence does not show data loss: the event is retained and chain-valid under the alternate name. No authorization bypass, secret disclosure, missing revoke event or chain break was observed.

## Evidence

- `../evidence/CP105-audit-name-contract.json` — sanitized exact-action API filter results and chain verification.
- `../evidence/CP105-audit-filter-documented-issued.png` — visually inspected Security Log showing zero results for the documented action.
- `../evidence/CP105-audit-filter-actual-created.png` — visually inspected results showing sequence 499 for the issued key.
- `../evidence/CP105-security-audit-key-events.png` — issue/revoke events visible together.
- `../evidence/CP105-audit-chain-after-key-lifecycle.png` — visually inspected intact chain across 511 events.
- Frozen source documentation names `virtual_key.issued` in `docs/ux/screen-specifications.md` (audit contracts); the implementation emits `virtual_key.created` in `internal/iam/vkey/service.go`. Source inspection supports the observed mismatch; no source files were modified.

## Root-cause hypotheses (not established)

- Documentation may predate an intentional naming change from `issued` to `created`.
- Or the issuance service and audit API/UI may have diverged from a still-current published event vocabulary.

Neither hypothesis is established by this test. Engineering should select and document one canonical event name.

## Acceptance criteria

1. A single canonical issuance action name is documented and emitted by the service; API filtering and Console filtering return the same matching event.
2. The canonical issue event identifies actor, Virtual Key target and applicable organization/project/environment scope, without storing the plaintext key secret.
3. Revocation remains represented by the documented canonical revoke action.
4. Issue/revoke event creation remains atomic with the corresponding state transition, and chain verification remains intact.
5. Regression coverage verifies exact action filtering, pagination, UI display, actor/target metadata, no-secret audit metadata, and chain verification for issue and revoke.
6. If `virtual_key.created` is retained as canonical, update the published contract and consumers; if `virtual_key.issued` remains canonical, migrate or compatibly alias existing consumers without silently dropping records.

## Scope limits

This retest does not exercise audit append/storage failures, rollback behavior, populated organization erasure, cross-tenant event isolation, or consumer integrations. Those remain in DEF-007's open coverage.
