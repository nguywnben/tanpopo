# CP84 — Operations incompatible upgrade preflight

- **Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`; product source unchanged.
- **Runtime/browser:** isolated `127.0.0.1:18084`, Operator + Org Owner, real Chromium at 1440×900.
- **Result:** PASS for the tested fail-closed compatibility response and blocker display. The actual upgrade/ABORTED_SAFE lifecycle remains UNVERIFIED.

## Procedure and outcome

1. Opened Operations → Upgrades and entered the valid far-future target `v999.0.0` into the real preflight form. No upgrade action was available or invoked.
2. Clicked **Run preflight**. Tanpopo returned HTTP 200 with `compatible:false`, state `BLOCKED_INCOMPATIBLE`, and two blocking extensions. Both entries identified installed version `0.1.0`, range `>=0.1.0 <0.3.0`, and reason `target core version is outside the extension compatibility range`.
3. The visible Console presented the red **Upgrade blocked** state and listed both blockers with IDs, versions, compatibility ranges, and reason. The browser screenshot was visually inspected.
4. The SYSTEM audit API returned sequence 6 `ops.upgrade_preflight_ran`, attributed to the active Operator, target `v999.0.0`, metadata `compatible:false`, `BLOCKED_INCOMPATIBLE`, and blocker count 2. CP83 sequence 5 remains the prior `v0.2.0` compatible control.
5. No binary, lockfile, installation, or provider state was changed. No upgrade was started.

Evidence:

- `../evidence/CP84-upgrade-preflight-v999.png`
- `../evidence/CP84-upgrade-preflight-v999.json`

## Acceptance scope and limits

This verifies a real incompatible-target preflight with explicit fail-closed UI feedback and persistent audit evidence. The documented full acceptance scenario also expects an upgrade workflow to exit `ABORTED_SAFE` without DB or binary corruption. The Console exposes only compatibility preflight in this path; there is no Apply/Abort control or upgrade execution operation to exercise. Therefore the `ABORTED_SAFE` transition, corruption-free exit from an actual upgrade attempt, and non-Operator denial remain **UNVERIFIED**. A high semantic version is a compatibility probe, not a release target recommendation.

## Consolidated phase status

- **Historical defects:** no DEF-001–015 disposition changed; DEF-005 remains PARTIAL. No QA-001 PASS is inherited.
- **Open findings:** DEF-016–033 remain confirmed open; QA-OBS-001–014 remain preserved. No new defect or observation ID assigned.
- **Coverage completed:** paired compatible (`v0.2.0`) and incompatible (`v999.0.0`) Operations preflight paths, visible blocker details, HTTP responses, and SYSTEM audit attribution/metadata.
- **Coverage blocked / remaining:** actual upgrade/ABORTED_SAFE workflow and cross-role denial remain UNVERIFIED. A2A runtime 18445 remains configuration-disabled; webhook delivery lacks usable Operator/receiver/signing prerequisites; RC4 artifact remains UNVERIFIED. Other integration, browser, platform and product coverage remains incomplete.
- **Next work:** continue unaffected workflows and historical defect retests; preserve safe-abort execution as an explicit engineering/QA requirement if an upgrade runner becomes available.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
