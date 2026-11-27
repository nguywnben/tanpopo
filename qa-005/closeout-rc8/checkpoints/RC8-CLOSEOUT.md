# QA-005 RC8 focused closeout checkpoint

**Date:** 2026-11-27 (Asia/Saigon)
**Frozen baseline:** RC8 `3fb156bb51ab462ec5ae46a8bcb213f1a0714d7c`
**Prior QA-005 handoff:** CP123, QA branch commit `5cfb12896b6cf0d81a1fdd82dd75ba5518f38b44`
**Disposition:** focused closeout only; QA-005 campaign remains incomplete.

## RC8 identity and artifact

Release workflow run `46010590867`, artifact `15204068659`, was retrieved before its recorded expiry (`2026-11-28T14:02:04Z`). SHA256SUMS matched all five cross-platform archives. The Linux/amd64 binary SHA-256 is `30f85f6da82676dc82b27d02e2d409b60e73c99c4aade2a72e8c2af520f019cd`; `tanpopo version --output json` reported `0.0.1-snapshot`, commit `3fb156bb51ab462ec5ae46a8bcb213f1a0714d7c`, build date `2026-11-27T13:59:26Z`. The post-merge CI run `46010590918` succeeded but exposed no downloadable artifact. The release workflow reports successful attestation creation/verification; independent local signature verification is UNVERIFIED because the installed `gh` lacks `gh attestation` and the GitHub attestation API returned 404. The managed `go` executable is a broken shim (`go version` / `go env` fail); source rebuild was not needed because tests used the exact release binary.

## Environments and evidence

- PostgreSQL 16 on an isolated disposable database and non-superuser/non-BYPASSRLS role; Valkey-backed runtime on `127.0.0.1:18160`.
- Exact RC8 binary also started from a full SQLite clone; startup upgraded schema 25→27 and retained 12 budget rows and 43 reservation references.
- Real Chromium/Playwright interactions covered Owner and Team Admin sessions, FinOps, Security, Extensions, Access & Keys and mobile Overview. Screenshots linked from [the closeout report](../REPORT.md) were visually inspected.
- Local loopback A2A and provider fixtures only; no paid provider call.
- Exact full-source SQLite→PostgreSQL migration returned `VERIFIED`: 55 managed tables / 1,272 rows, 727 audit events, all 12 budget ledgers and 43 references, 9 deployments/pricing records, 48 settlements, and all 5 org/project DEK envelopes trial-decrypted. Source and target canonical digests are in [migration evidence](../evidence/full-source-migration.json). The source contained zero secret-credential envelopes, so credential-envelope decryption/use is UNVERIFIED.

## Target outcomes

| Target | RC8 outcome | Key result |
|---|---|---|
| DEF-022 | PASS (scoped) | Role downgrade revoked an active Team Admin browser session; next session request 401; refresh returned to login. |
| DEF-050 | BLOCKED / UNVERIFIED | No eligible local route; native streaming request failed before dispatch. Unified streaming was not reached. |
| DEF-069 | PASS (scoped) | PostgreSQL A2A partial-spend acknowledged cancellation settled 350 micro-USD from 1,000; unsupported cancellation released unused hold; concurrent pair settled independently. |
| DEF-075 | PASS (scoped) | Both rotation credentials worked pre-expiry; after 60-second grace old key 401, current key 200, state `CREATED_ACTIVE` v3, finalization audit present. |
| DEF-080 | UNVERIFIED | No RC8 FinOps Viewer retest. Historical FAIL remains open. |
| DEF-081 | PARTIAL | Existing PostgreSQL adapter inventory matched active installations after restart. No fresh activation/re-enable completed; emergency stream barrier blocked prerequisite quarantine. |
| DEF-082 | BLOCKED | Quarantine failed with 503 before an unquarantine/badge transition could be tested. |
| DEF-083 | BLOCKED | No eligible deployment; native request returned 503 before fixture dispatch; unified trailer path not run. |
| DEF-084 | UNVERIFIED | 320px no-alert screenshot was visually inspected; long-ID alert scenario was absent. |
| DEF-085 | PASS (scoped) | Zero-hold A2A request returned 403 before dispatch; fixture call count remained zero. |
| DEF-086 | PASS (scoped) | Full unfiltered source migration and independent value parity verified; migrated Console and synthetic A2A settlement operated. |

Evidence filenames and per-defect details are in `../evidence/` and `../REPORT.md`.

## Historical evidence and discrepancy handling

CP123's baseline report says 61 scoped PASS, 15 PARTIAL, 3 FAIL (DEF-022/069/075), and zero UNVERIFIED for DEF-001–079. The 15 partial IDs in its contemporaneous full regression ledger are DEF-004, 020, 027, 028, 036, 041, 043, 044, 048, 049, 050, 060, 072, 077 and 078. The 3 failures were independently revisited where Round 5 scope demanded it: DEF-022/069/075 now have scoped RC8 passes; historical failures remain in the source ledger. This focused overlay does not retest the remaining 76 historical defects and does not convert any earlier PASS into proof of complete closure. See [overlay](../regression-ledger-overlay.csv) and the unmodified historical [DEF-001–079 ledger](../../DEFECT-REGRESSION-LEDGER.md).

The CP123 handoff established only filtered migration parity because the original RC7 PostgreSQL width blocked long budget identifiers. Round 5 widens parent and reference columns. RC8 migration now copied the exact full source without filtering or ID rewriting; this resolves the exercised data-preservation case but not secret credential migration or multi-node PostgreSQL/Valkey recovery. The old filtered CP123 evidence remains unchanged.

## Residual risk and verdict

Unresolved/blocked evidence is material: fresh PostgreSQL extension activation and adapter recovery remain incomplete (DEF-081/082); documented native and unified streaming accuracy trailers were not independently observed (DEF-050/083); FinOps Viewer denied-state and long-ID mobile alert require fresh verification (DEF-080/084); secret credential migration/decryption had no source envelope to exercise. A quarantine request failed closed with HTTP 503 `quarantine_kill_switch_failed` and “could not establish its emergency stream barrier”; state remained unchanged. The evidence does not establish whether this is a product defect or a test-topology/configuration limitation, so it is not numbered as a new defect.

**TEEP-00 verdict: BASELINE_REMEDIATION_REQUIRED.** Round 5 closed important A2A accounting and full-source migration cases, but extension lifecycle and streaming acceptance plus multiple high-risk partials remain unverified/blocked. QA-005 is not complete. This verdict authorizes neither production release nor signature integration. `GATE-REL-07` remains BLOCKED.

Cleanup: all RC8-created A2A/native/rotation test keys were revoked; disposable PostgreSQL database and migrator role were dropped; exact RC8 servers and loopback fixtures were stopped. Temporary test credential and key files were removed. Existing historical QA documentation/evidence remains unchanged.
