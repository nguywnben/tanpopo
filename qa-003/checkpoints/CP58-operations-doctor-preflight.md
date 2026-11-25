# CP58 — Operations doctor preflight

**Baseline:** RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`; exact-source CLI `/tmp/tanpopo-qa003`
**Runtime:** isolated `/tmp` SQLite clone, synthetic KEK, loopback ephemeral listener, empty valid lockfile
**Status:** scoped operational preflight passed; QA-003 remains in progress and `GATE-REL-07` remains BLOCKED.

## Procedure and results

1. Copied the synthetic CP55 SQLite test database into a separate CP58 directory; the CP55 source database was not modified.
2. Created a valid empty lockfile in the CP58 clone. Ran `tanpopo --output json doctor --root <isolated-root>` against that clone with a synthetic 256-bit KEK in the process environment, an ephemeral loopback listener address, and an expected clock timestamp sampled at launch. No secret value was saved or emitted.
3. The exact RC5 CLI exited 0. `clock-skew`, `ports-available`, `master-kek-ready`, `storage-fs-lock`, `bundle-lockfile-integrity`, and `egress-dns-ssrf` were PASS. `cluster-cache` was SKIPPED (HA not configured); `tls-posture` was WARN because the listener was loopback-only. That is the expected posture for this local test and does not establish non-loopback TLS readiness.
4. Repeated the bundle-integrity check using the valid non-empty synthetic CP57 lockfile and its local synthetic archive. Doctor exited 0 with bundle integrity PASS. Appending bytes to the archive changed that check to FAIL and caused exit code 2. Restoring the test archive restored the passing condition; the test did not modify the RC5 source or cloned database.

Sanitized machine result: [CP58-doctor-readiness-result.json](../evidence/CP58-doctor-readiness-result.json). No Tanpopo implementation changed. The isolated database clone and synthetic files remain in `/tmp` and are not staged.

## Limits

This does not verify PostgreSQL/Valkey, multi-node cache readiness, a production TLS terminator/trusted-proxy setup, non-loopback port conflicts, clock drift, disk exhaustion, compromised permissions, or live service health endpoint behavior. The doctor report intentionally returns status-only checks, so this run confirms the named statuses and process exit result, not detailed diagnostics for every failing condition.
