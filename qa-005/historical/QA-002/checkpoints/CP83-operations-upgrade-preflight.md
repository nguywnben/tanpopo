# CP83 — Operations upgrade compatibility preflight

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; no product source changes.
- **Runtime/browser:** isolated `127.0.0.1:18084`, authenticated Operator + Org Owner, real Chromium at 1440×900.
- **Result:** PASS for one compatible-target preflight and its SYSTEM audit record. No upgrade or package mutation was performed.

## Procedure and outcome

1. Opened Operations → Upgrades in the real Console. The visible form described an extension ABI compatibility preflight, accepted a target Core version, and displayed the current `tanpopo.lock` pins.
2. Entered `v0.2.0` and clicked **Run preflight**. The browser showed **Compatible / State: READY_TO_UPGRADE**. The actual `POST /admin/v1/ops/upgrades/preflight` returned HTTP 200 with `compatible: true`, `blocking_extensions: []`, and the same state.
3. Queried the SYSTEM audit partition through the authenticated API. It returned the persisted `ops.upgrade_preflight_ran` event at sequence 5 with the expected Operator actor, target `v0.2.0`, and metadata `{compatible:true,state:READY_TO_UPGRADE,blocking_extension_count:0}`.
4. Visually inspected the preflight result and extension lockfile view. No Core upgrade, extension change, or inference occurred.

Evidence:

- `../evidence/CP83-upgrade-preflight-v020.png`
- `../evidence/CP83-upgrade-preflight-results.json`
- `../evidence/CP83-upgrade-preflight-system-audit.json`

The first harness attempt to POST the audit-chain verification endpoint lacked the valid CSRF token and received HTTP 403 `csrf_validation_failed`. This was a harness-authentication mistake, not evidence of a product failure; the successful preflight and persisted audit record were independently confirmed. No subsequent chain-verification result is claimed for CP83.

## Acceptance scope and remaining work

The reviewed acceptance criterion requires a deliberately incompatible active extension to produce `BLOCKED_INCOMPATIBLE` and allow a clean `ABORTED_SAFE` exit without corruption. CP83 tested only a compatible target where the currently active extension set produced no blockers. Incompatible preflight, abort/recovery behavior, target-version validation, and cross-role denial remain **UNVERIFIED**. The SCR-16 overall result therefore stays PARTIAL PASS.

## Consolidated phase status

- **Historical defects:** CP83 does not change any DEF-001–015 disposition; DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Open findings:** DEF-016–033 remain confirmed open. QA-OBS-001–014 remain preserved. No new defect or observation ID was assigned.
- **Coverage completed:** Operations → Upgrades compatible path, result copy, API response, and persisted SYSTEM audit attribution/metadata.
- **Coverage blocked / remaining:** incompatible-extension fixture and safe-abort lifecycle; additional Operator/Owner role combinations; webhook delivery prerequisite; clustered PostgreSQL/Valkey operations; RC4 artifact verification remains UNVERIFIED. Broader product and platform coverage is incomplete.
- **Next work:** continue independent defect retests and unexplored user workflows. Preserve an isolated incompatible extension fixture before testing the required block/abort path.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
