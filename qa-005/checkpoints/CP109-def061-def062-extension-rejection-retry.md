# CP109 — DEF-061 discard of escalated upgrade and DEF-062 duplicate retry

**Disposition:** Both historical UI findings passed in the tested exact-RC7 Owner/SQLite/Chromium path. Their prior QA-004 failures remain preserved. This is scoped retest evidence, not whole-extension lifecycle closure. `GATE-REL-07` remains **BLOCKED**.

## Baseline and isolated runtime

- Exact source commit: `737079ddb847d97dbcf832f610ed0048556cb65b`.
- Compiled runtime SHA-256: `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
- A point-in-time SQLite online-backup clone was started on loopback `127.0.0.1:18164`; the QA-005 baseline database on port 18145 was not mutated.
- Chromium 156, synthetic QA Owner, and a manifest-only `@qa004/lifecycle-canary` package. The package had no executable code, TLS-only `.invalid` hosts, and private IP access disabled. Its unverified provenance was explicitly acknowledged. No provider calls or paid usage occurred.

## DEF-061 — discard candidate from the Console

1. The existing 0.1.0 package was staged and explicitly approved in the clone. The real UI displayed Unverified/Unsigned and required both the permission and unverified-source acknowledgements.
2. The real Installed → Actions → Upgrade package flow staged 0.1.1 with only `extra.qa004.invalid` added. API returned HTTP 202 and readback showed `UPDATING_PENDING_RECONSENT`, active 0.1.0, candidate 0.1.1.
3. The permission review displayed the exact host delta. Its close button reads **Close (keep pending)**; using it left the candidate pending and active 0.1.0 unchanged.
4. The pending row menu offered **Discard staged upgrade**. Its confirmation said the candidate would be discarded and the prior 0.1.0 returned to `ENABLED_ACTIVE` without granting escalated permissions. Selecting **Discard candidate** caused HTTP 204. Authenticated readback returned `ENABLED_ACTIVE`, active version 0.1.0, no candidate, version 4.
5. Security → Audit Log showed `extension.escalated_upgrade_rejected` at sequence 598 targeting this installation. Authenticated hash-chain verification returned HTTP 200, `intact=true`, checked 601 events.

**Result: PASS (scoped).** The original missing-console-discard failure is not reproduced. The active version and permissions remain intact, candidate is cleared, and an audit decision is recorded. Other roles, stale ETag conflict handling, backends and browsers remain unverified. The old QA-004 failure and its API-only workaround are not erased.

## DEF-062 — retry the identical discarded immutable candidate

After the discard, the same `.1.1` URI and exact digest were submitted again from Installed → Upgrade package. API returned HTTP 409 `resource_already_exists` with a generic immutable-version conflict message. The Console displayed: **“This exact immutable candidate version was already registered or rejected. Publish a new version with a bumped SemVer tag and updated digest to proceed.”** Authenticated readback remained `ENABLED_ACTIVE` version 0.1.0 with no candidate. The error was captured after closing the drawer so the full message was visible and screenshots were visually inspected.

**Result: PASS (scoped).** The old misleading “already installed; open Installed extensions” message is not reproduced. The UI distinguishes the duplicate candidate and gives a usable recovery step while preserving state. Other server conflict forms and browser/role coverage remain open; historical QA-004 FAIL is retained.

## Evidence and boundaries

The machine-readable sequence, response codes, event identity, chain result, limitations and screenshot hashes are in [sanitized evidence](../evidence/CP109-def061-def062-extension-discard-and-retry.json). Screenshots: [initial consent](../evidence/screenshots/CP109-def061-initial-stage-review.png), [escalated permission review](../evidence/screenshots/CP109-def061-escalated-review.png), [pending actions](../evidence/screenshots/CP109-def061-pending-actions.png), [discard confirmation](../evidence/screenshots/CP109-def061-discard-confirm.png), [persisted discard](../evidence/screenshots/CP109-def061-discarded-readback.png), [audit event](../evidence/screenshots/CP109-def061-security-audit.png), and [full duplicate error](../evidence/screenshots/CP109-def062-duplicate-retry-error.png). All seven were visually inspected.

The final candidate-free active state in the disposable clone is `ENABLED_ACTIVE` 0.1.0. No source code/specification changed; no production key, release signature, real provider or external endpoint was used. The clone and package archives remain outside the QA branch and are disposable test data.
