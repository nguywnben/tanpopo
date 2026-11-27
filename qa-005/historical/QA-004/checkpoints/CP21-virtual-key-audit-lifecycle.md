# CP21 — Virtual Key audit lifecycle

**Outcome:** PASS for one synthetic key create/revoke audit lifecycle and the observed audit-chain verification; broader audit-contract coverage remains PARTIAL. GATE-REL-07 remains BLOCKED.

## Real-user reproduction

CP20 created an environment-bound Virtual Key using the real Owner Access & Keys flow, then revoked it using **Revoke key → Graceful drain**. The Security → Audit Log screen was opened in the same authenticated Owner session. Its exact action filter showed `virtual_key.created` and `virtual_key.revoked` entries targeting the same synthetic key ID. The revoke event was opened with **Inspect**; it displayed sequence 156, action, actor, target, timestamp, event hash, and metadata. The revoke metadata was `mode: STANDARD`, `reason: Graceful drain revocation`. The frozen RC6 handler maps request mode `GRACEFUL_DRAIN` to internal `STANDARD` before audit emission, so this is consistent with the selected UI action, not a mode mismatch.

The audit screen's **Verify hash chain** action reported: “Cryptographic RFC 8785 JCS chain intact across 156 events.” The scoped action filter returned 13 creation events and 4 revocation events overall at test time, with the CP20 target visible in each. The complete target linkage and sanitized event summary are in `evidence/CP21-audit-vkey-events.json`. Visually inspected UI evidence is `CP21-audit-created-filter.png`, `CP21-audit-revoked-filter.png`, `CP21-audit-event-inspect.png`, and `CP21-audit-vkey-verified.png`.

## Classification

No new defect is confirmed. This extends the existing Virtual Key lifecycle coverage: the key-state transition persisted, creation and revocation were both linked to its target in audit, and the hash-chain verification succeeded for the then-current 156-event chain. It does not prove every audit action is present or correctly attributed, nor tamper detection or failure atomicity. DEF-007's previously scoped successful organization/project/environment creation paths remain the only historical DEF-007 pass; other provisioning and fault paths are not inferred from this test.

## Remaining coverage

Other resource mutation actions, role attribution across all roles, denied-action audit expectations, atomicity when audit append fails, hash-tamper detection, concurrent writes, restart verification, retention/export, and audit filter behavior for all canonical action names remain UNVERIFIED. CP20's scoped Developer membership/revoke denials and Owner key revoke/readback are documented in [CP20](CP20-access-key-role-boundaries.md).
