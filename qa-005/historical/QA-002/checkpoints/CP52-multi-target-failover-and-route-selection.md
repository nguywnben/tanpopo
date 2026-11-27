# CP52 — Multi-target failover and route target identification

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** scoped priority failover PASS; DEF-027 FAIL; DEF-017 recurrence.  
**Release gate:** `GATE-REL-07 BLOCKED`.

## Journey

Using real Chromium and the isolated QA runtime, an owner created and probed a second loopback Ollama provider connection, added a deployment for the same logical/upstream model as an existing connection, and created a two-target `PRIORITY_FAILOVER` route. The first target's fixture returned HTTP 503; the second target returned an HTTP 200 streaming response. The Playground displayed the backup's synthetic completion. Fixture call order, route API target IDs, and the persisted request trace's two `RouteAttempt` waterfall stages agree: first deployment 503, second deployment success. Trace reports HTTP 200 and `failover_attempts=2`. The successful Playground trace card remained `Trace metadata pending…` (DEF-017 recurrence).

## Defect

DEF-027 was confirmed during route setup. The environment had three distinct healthy deployments with the same upstream model `qa002:latest`, same region `ON_PREM`, and same health. The target dropdown presented all three with identical visible and accessible text, omitting connection and deployment identity. Ordered target rows and the configured route fallback chain also showed only model and health. API/persistence confirms the selections represent distinct deployment IDs and different provider connections; controlled failure then established the first and second endpoint identities. This leaves a route author unable to choose a known provider or verify priority through the Console.

Standalone report: [`reports/DEF-027-route-target-options-hide-deployment-identity.md`](../reports/DEF-027-route-target-options-hide-deployment-identity.md).

## Evidence

- `../evidence/CP52-multi-target-failover-and-route-selector.json`
- `../evidence/CP52-route-deployment-options.png` (visual inspection confirms duplicate choices)
- `../evidence/CP52-route-two-targets-selected.png`
- `../evidence/CP52-route-failover-created.png`
- `../evidence/CP52-playground-failover-result.png` (visual inspection confirms backup completion and DEF-017 recurrence)
- Provider/deployment setup and temporary key revocation screenshots are listed in the evidence JSON.

## Coverage disposition

- **PASS:** one controlled two-target streaming route failed over from an upstream 503 to a successful backup; ordered deployment IDs and trace waterfall matched.
- **FAIL:** DEF-027 prevents route authors from distinguishing deployments sharing model, region, and health.
- **FAIL recurrence:** DEF-017 trace metadata remains pending after success.
- **Cleanup:** the temporary Playground key was revoked via Access & Keys; the isolated database shows `REVOKED` and zero active streams were severed.
- **UNVERIFIED:** more than two targets, max hops, repeated failures/all-target failure, timeouts, cancellation, non-streaming failover, route editing/reordering after save, policy/region incompatibility, role restrictions, provider-specific 429 and connection identity on other screens.

QA-002 remains in progress. No implementation was changed.
