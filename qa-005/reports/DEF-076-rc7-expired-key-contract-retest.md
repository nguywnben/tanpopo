# DEF-076 — exact-RC7 expired-key contract retest

**QA-005 result:** PASS (scoped to the frozen RC7 contract and live 300-second boundary). Historical QA-004 report retained unchanged.

## Contract reconciliation

The QA-004 RC6 report states that TP-IMP-0009 expected `virtual_key_expired`, AUTH-05 expected `key_expired`, and runtime returned `invalid_virtual_key`. The exact frozen RC7 documents inspected for this campaign agree with one another: TP-IMP-0009 AC-2 requires a uniform HTTP 401 `invalid_virtual_key`; AUTH-05 requires the same anti-enumeration response; the API compatibility table uses that code for unknown, expired or revoked keys. This campaign records the frozen RC7 facts and does not revise specifications or infer intent for earlier baselines.

## Reproduction

Using the exact-source binary (`737079ddb847d97dbcf832f610ed0048556cb65b`, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`) on an isolated SQLite clone at `127.0.0.1:18164`, Chromium 156.0.8078.79 and a deterministic loopback route:

1. Create a scoped PROVIDER key and verify its pre-rotation request returns HTTP 200.
2. Rotate it through the real Console with `grace_seconds=300`; verify the old digest is the persisted previous digest and successor digest is active.
3. At approximately T+270.016 seconds, both old and current keys return HTTP 200.
4. At T+301.086 seconds, old key returns HTTP 401 `invalid_virtual_key`; successor returns HTTP 200.
5. Revoke the temporary key and confirm no active CP113 test keys remain.

**Expected:** old key is unusable after grace with uniform `invalid_virtual_key`; current key remains usable.  
**Actual:** matches on the tested SQLite/Owner/provider route.

The same run observed a separate DEF-075 lifecycle failure: post-expiry state remained `ROTATING_DUAL_VALID` v2 with its deadline and no finalization audit event. That state defect is tracked separately and prevents treating key rotation as fully healthy. Exact-source finalization conflicts remain recorded under DEF-075.

See [CP113](../checkpoints/CP113-def059-def075-def076-regression.md), [sanitized evidence](../evidence/CP113-def059-def075-def076.json), and the historical [DEF-076 record](../historical/QA-004/reports/DEF-076-expired-key-error-contract-conflict.md). No secrets, credentials or product changes are included.
