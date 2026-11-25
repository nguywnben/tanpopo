# CP53 — Fresh bootstrap audit events

**Status:** Completed for one fresh RC5 SQLite bootstrap path; broader audit coverage remains open.
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** isolated `--role all`, SQLite, `127.0.0.1:18118`; synthetic tenant only.
**Product changes:** none. **GATE-REL-07: BLOCKED.**

The serving process reported frozen commit `3423d4f523ef0ac77469a1201b8b753d1d31c902`, build date `2026-11-25T00:10:06+07:00`, version `0.1.0-dev`. CP54 records binary hashes and independently built metadata. A later attempt to open this database with unrelated `/tmp/qa-tools/tanpopo` (`commit=none`) failed checksum validation before modifying the database; that is not a product result.

## Question

QA-001 recorded no `organization.created`, `project.created`, or `environment.created` entries in a populated tenant (`qa-fresh-install-cp38`), while QA-003 CP02 observed these events on UI-created tenants and CP04 proved rollback when audit insertion was fault-injected. This checkpoint tests the original first-install bootstrap path again and keeps the old result intact.

## Reproduction

1. Start the exact frozen RC5 executable against a new isolated SQLite directory and loopback listener. Use only synthetic credentials and a generated test master key held outside the repository.
2. Open `/setup` in real Chromium. Enter a fresh owner, organization, and project. The First connection step states no provider adapters are enabled and offers setup continuation without a provider. Back up the generated test key locally, acknowledge the key-custody step, and review `Initial connection: Skipped`.
3. Complete setup. The browser receives HTTP 201 and routes to `/overview`; the initial organization, default project, and default `dev` environment persist.
4. Sign in as the synthetic owner, open Security → Audit Log, and inspect the rows. A new isolated database initially contains `organization.created`, `project.created`, `environment.created`, and the owner login event. Targets match the created resource IDs and all three provisioning events have the owner as actor.
5. Use the UI's **Verify hash chain** action. Independently call the verifier using the authenticated session's CSRF token and `POST /admin/v1/audit-events/verify`. The API returned 200, `intact: true`; after subsequent sign-ins, it checked all six persisted events. The earlier exploratory GET was a 405 and is not treated as verifier evidence.

## Result and evidence

- **Scoped result: PASS.** Fresh bootstrap emitted all three expected provisioning events with matching targets and actor. Bootstrap returned 201 and the created organization/project/environment were readable from the authenticated API. No provider connection was created because the environment has no enabled adapters.
- The first post-bootstrap audit read contained four events. Each additional login appended `auth.login_succeeded`; the final evidence snapshot therefore shows six events, including three login events, and a valid chain across all six.
- Visual evidence was inspected directly: [setup owner form](../evidence/CP53-fresh-bootstrap-before.png), [provider step](../evidence/CP53-bootstrap-step2.png), [key custody](../evidence/CP53-bootstrap-step3.png), [review](../evidence/CP53-bootstrap-review.png), [settled overview](../evidence/CP53-fresh-owner-overview.png), and [audit log with verified chain](../evidence/CP53-fresh-bootstrap-audit.png). Structured sanitized readback: [result JSON](../evidence/CP53-fresh-bootstrap-audit-result.json).
- Setup UX observation: when no bundled provider adapter is enabled, the wizard says to finish setup and activate an adapter later; the review clearly says initial connection was skipped. This path is operationally coherent, though the wizard's claim to provision an initial route was not verified as an actual route. Do not infer route provisioning from the screen copy.

## Historical reconciliation and limits

QA-001's `phase48-fresh-bootstrap-audit.json` remains preserved verbatim. It refers to a different already-populated tenant and records 49 existing owner/key audit events but no provisioning events. CP53 uses a genuinely new RC5 database and finds all three events. These results conflict as to whether the historical tenant should have had bootstrap events; the new successful bootstrap does not establish that the historical missing rows were later repaired or that all provisioning code paths are covered. Keep **DEF-007 PARTIAL**.

CP04's isolated SQLite audit-insert failures still show organization/project/environment creates rolling back when their audit append fails. CP18 separately shows crypto-shred can destroy the DEK and leave a tenant in `DELETING` after its final audit append fails, with a later manual retry succeeding. Disk/IO faults, concurrent writes, PostgreSQL/Valkey, other provisioning entry points, route creation, and automated shred recovery remain UNVERIFIED.

No secrets, credentials, databases, or runtime logs are included. Generated master-key material and synthetic login credentials stay in `/tmp` only.
