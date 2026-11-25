# CP73 — DEF-041 exact-RC6 audit-failure state and recovery

**Baseline:** exact frozen RC6 2eb7f6f6e5ba703d44fbd273f0245ba441de74e1  
**Runtime:** disposable SQLite clone on 127.0.0.1:18133, copied through SQLite backup from the existing isolated RC6 runtime  
**Role/browser:** authenticated Org Owner/Operator in real Chromium, 1440×1000  
**Product source modified:** no

## Result

**DEF-041: PARTIAL on exact RC6.** A SQLite BEFORE INSERT trigger blocked only the organization.crypto_shredded audit event. The real Console's Crypto-shred request returned HTTP 500 after the organization and project DEKs were already gone. The project, environment, and one synthetic Virtual Key remained, and no terminal receipt existed yet.

RC6 no longer showed the historical unlabeled state. The Console row displayed **Deleting (pending erasure)**, a banner said key destruction finished but deletion finalization was incomplete, and the row exposed **Retry finalization**. After removing only the trigger from the disposable clone, I clicked that real Console action. It returned HTTP 200, removed the organization and its child rows, reported one revoked Virtual Key, and emitted organization.crypto_shredded. Targeted audit-event readback returned one matching sequence-5 receipt; audit verification returned HTTP 200, intact, five checked events.

Residual UX coverage remains: the banner does not enumerate which metadata rows can remain, and the original Crypto-shred confirmation dialog stayed open with its **Crypto-shred** action after the failure. The automated recovery, restart, concurrent retry, disk/I/O, and PostgreSQL/Valkey paths remain UNVERIFIED. No new defect is assigned from this scoped reproduction.

## Reproduction

1. Make a SQLite online backup of the isolated RC6 test-root database into a separate disposable data directory. Start the exact RC6 artifact against that clone on a dedicated loopback port; do not edit product files.
2. Install a test-only SQLite BEFORE INSERT trigger that raises an error only when audit_events.event_type='organization.crypto_shredded'.
3. Using real Chromium, create synthetic organization qa004-cp73-def041-20261125, project cp73-failure-project, environment cp73-failure-env, and an environment-scoped PROVIDER Virtual Key. The one-time key was not used against a provider or retained in the evidence package.
4. In Operations → Organizations, open Crypto-shred, provide the matching slug and synthetic justification, then confirm. The exact-target DELETE returns HTTP 500 internal_error / failed recording shred audit event.
5. Inspect the row in the Console and read the clone database read-only. After failure the organization is DELETING; Org and project DEK counts are zero; one project, environment, and Virtual Key remain; there are four create audit events and no crypto-shred receipt; one vault_shred_requests row is COMPLETED.
6. Remove only the injected trigger. In the real Console, click **Retry finalization**. It returns HTTP 200. Read back zero organization, DEK, project, environment, and Virtual Key rows, one terminal audit event at sequence 5, and an intact five-event tenant chain.

Sanitized counts and responses are in [CP73 results](../evidence/CP73-def041-fault-injection-results.json). Visually inspected screenshots: [created canary](../evidence/CP73-org-created.png), [project scope](../evidence/CP73-project-created.png), [environment form](../evidence/CP73-environment-create-form.png), [key metadata](../evidence/CP73-populated-vkey-metadata.png), [confirmation](../evidence/CP73-shred-confirmation-filled.png), [failure state and retry action](../evidence/CP73-shred-audit-failure-ui.png), and [completed retry receipt](../evidence/CP73-shred-retry-complete.png). The screenshot showing the one-time synthetic key was deleted locally before evidence staging and is excluded.

## Expected versus actual

- **Historical RC5 expectation:** do not leave a destructive intermediate state unlabeled or imply the organization is active; clearly explain the irreversible key-destruction phase and provide a safe recovery path.
- **Exact RC6 actual:** the Console marks the row pending erasure, explains key destruction finished and finalization is incomplete, blocks new/switch operations globally, and offers a dedicated retry-finalization action. A manual retry completed and emitted the terminal receipt.
- **Remaining gap:** the notice does not name the retained metadata categories, and the failed confirmation dialog itself remains open with a Crypto-shred CTA. This behavior warrants keeping DEF-041 PARTIAL until its fuller UX/recovery contract and other failure modes are checked.

## Facts and hypothesis

**Observed facts:** the injected insert fails only for the terminal crypto-shred event; the DELETE response is HTTP 500; after that failure the organization is DELETING, both DEK tables have zero target rows, the project/environment/key metadata rows remain, and no tombstone event exists. Removing the trigger and using the Console retry removes those rows, revokes the key, and creates an intact receipt chain.

**Hypothesis:** the operation is intentionally phased across irreversible DEK destruction and final mutable-row purge/audit append; the UI now reflects that phase, while its still-open original confirmation action and non-itemized residual metadata disclosure are legacy-state UX details. This test did not inspect or infer further transaction ordering, cross-process behavior, or failure compensation.

## Acceptance still open

- Verify the pending status and retry contract for failures before key destruction, after key destruction, during purge, and after receipt append.
- State which resource metadata can remain while explaining that the DEKs are already destroyed; make the pending modal/action unambiguous after a failed attempt.
- Verify safe retry after process restart, concurrent retry, disk-full/I/O failure, and on supported PostgreSQL/Valkey deployments.
- Verify automatic retry or an operational alert, and preserve the terminal verifiable audit chain.
- Verify erasure timing and cache/backup boundary separately; this test does not claim the <60s target.

The test-only clone was cleaned up after evidence capture; the original runtime and product source were not changed. QA continues; GATE-REL-07 remains **BLOCKED**.
