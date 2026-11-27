# CP100 — DEF-027 route target identity

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite through TLS proxy `https://127.0.0.1:19444`; Chromium 156 at 1440×1000; `ORG_OWNER` (`qa005-owner@example.invalid`); QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev.

## Result

**PARTIAL (fresh exact-RC7 route-target identity retest).** The actual Routes & Policies route picker visibly presented each candidate as provider-connection display name, model slug, short deployment ID, residency region and health. Two same-model `qa005-native:latest` deployments under the visible QA005 budgetless Ollama connection had different short IDs (`t0t22t` and `s434yd`); selecting the former rendered the same identity in the ordered target card. Screenshots were visually inspected. The route form was canceled without a route write; no inference/provider call occurred and no browser errors were recorded.

## Reproduction

1. Sign in to the exact-source Owner Console and open Routes & Policies under the populated QA005 project/environment.
2. Capture the route list, open Create route and open Select deployment.
3. Compare same-model choices. Select `QA005 budgetless Ollama / qa005-native:latest · t0t22t (ON_PREM · Healthy)` and click Add target.
4. Verify the selected target card continues to identify `qa005-native:latest · t0t22t`, region and health. Cancel the drawer; do not click Create route.
5. Confirm network capture contains no route write.

Evidence screenshots: [route list](../evidence/screenshots/CP100-def027-route-list.png), [empty route form](../evidence/screenshots/CP100-def027-route-create-targets.png), [candidate options](../evidence/screenshots/CP100-def027-route-target-options.png), and [selected target card](../evidence/screenshots/CP100-def027-selected-target-identity.png). Sanitized evidence: [CP100 JSON](../evidence/CP100-def027-route-target-identity.json).

## Limits and reconciliation

This run shows that the current picker and unsaved target card distinguish the two same-model deployments in this QA workspace. The historical QA-002 CP52 case had three healthy identical-model targets from different provider connections, a saved multi-target route summary, and a controlled 503→200 failover; that acceptance is not repeated here. Connection labels, route-edit/reorder flows, the saved summary under multiple targets, actual failover and other roles/backends/browsers remain UNVERIFIED. The old finding is therefore PARTIAL rather than closed. `GATE-REL-07` remains BLOCKED.
