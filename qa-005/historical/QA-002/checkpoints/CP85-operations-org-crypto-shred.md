# CP85 — Operations organization crypto-shred journey

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; product implementation unchanged.
- **Runtime/browser:** isolated `127.0.0.1:18084`, authenticated Operator + Org Owner, real Chromium at 1440×900.
- **Result:** PASS for a fresh empty-organization create → guarded crypto-shred → persisted audit verification journey. This is one scoped destructive path, not evidence of full erasure guarantees.

## Reproduction and observed result

1. In Operations → Organizations, created a disposable tenant named `QA002 CP85 Shred Probe` with unique slug `qa002-cp85-shred-probe-20261124`. UI reported success, the collection count increased from three to four, and the new row showed its organization ID. The organization was empty: no project, environment, or virtual key was provisioned.
2. Opened that row’s Crypto-shred dialog. The copy states that the operation irreversibly destroys the organization DEK, purges secrets and keys, and cancels active sessions within 60 seconds.
3. Entered a deliberately incorrect confirmation slug. The Crypto-shred button remained disabled. Entered the exact slug and a nonempty justification; the action became enabled. Both states were visually inspected in the captured screenshots.
4. Submitted the irreversible operation against this disposable QA tenant. UI and API returned success (HTTP 200); response reported zero revoked virtual keys. The row disappeared, collection count returned to three, and a fresh API list did not contain the deleted ID or slug.
5. Queried the organization-partition audit after deletion. `organization.crypto_shredded` remained available with sequence 2, expected owner actor and deleted-organization target/partition, exact confirmation slug, justification and zero revoked keys. An authenticated chain verification returned `intact:true`, checked two events (create and shred), and no broken sequence.

## Evidence

- `../evidence/CP85-empty-org-created.png`
- `../evidence/CP85-shred-confirm-mismatch-disabled.png`
- `../evidence/CP85-shred-confirm-exact-slug.png`
- `../evidence/CP85-org-shredded-list.png`
- `../evidence/CP85-org-shred-results-sanitized.json`
- `../evidence/CP85-org-shred-audit-sanitized.json`
- `../evidence/CP85-org-shred-chain-verify.json`

Screenshots were opened and visually inspected. The success screenshot contains the product’s opaque destroyed-DEK handle receipt; no key material or credentials were captured. The text export redacts that handle. Audit hashes and IDs are retained as evidence identifiers.

## Acceptance scope, defect impact, and limits

The tested confirmation guard behaved as expected, the empty synthetic org disappeared from the organization list, and the deletion event survived in its own chain. This supplies fresh success-path evidence for DEF-007 and does **not** close it: failure atomicity, organization updates, organizations containing resources/secrets/active sessions/keys, storage failures, cross-tenant authorization, independent proof of ciphertext/DEK destruction, four-eyes behavior, and cancellation timing were not tested. Do not infer erasure of residual backups from list absence.

An initial audit retrieval used an invalid filter combination and returned HTTP 400; correcting the query to the deleted organization ID/partition exposed the expected event. This was a test-query correction, not a product result. No product source was edited and no retained organization was deleted.

## Consolidated phase status

- **Historical defects:** CP85 changes no DEF-001–006 or DEF-008–033 disposition. DEF-005 remains partial. DEF-007 has a newly verified successful organization-shred audit path; its broad atomicity/security acceptance remains UNVERIFIED.
- **New defects:** none. No new observation was assigned.
- **Coverage completed:** fresh empty-org UI create, exact-string destructive confirmation guard, delete receipt and list persistence, deleted-org audit partition lookup, and intact two-event JCS chain; screenshots visually inspected.
- **Blocked / remaining:** no CP85-specific blocker. Actual upgrade/`ABORTED_SAFE`, webhook delivery, artifact provenance, runtime 18445 A2A, all open defect acceptance scope, and broad role/platform/integration coverage remain as recorded in the matrix. Org crypto-shred with populated resources, injected faults, other roles, and backup implications remain UNVERIFIED.
- **Recommended handoff:** retain DEF-007’s successful and failing historical evidence together; prioritize atomic audit/write behavior and explicit acceptance for populated tenant erasure plus failure recovery.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
