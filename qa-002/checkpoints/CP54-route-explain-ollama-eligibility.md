# CP54 — Route Explain versus live Ollama eligibility

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** FAIL DEF-028; live-route positive control PASS; DEF-017 recurrence.  
**Release gate:** `GATE-REL-07 BLOCKED`.

## Journey and evidence

On the existing real two-target Ollama route, the Console Route Explain page was used with `On-prem` residency and no requested capabilities. It returned HTTP 200 and said “Dry-run verified”, but displayed zero eligible and two rejected deployments. Both rejections were `DIALECT_MISMATCH`, with detail that empty deployment native dialect did not match `openai_compatible`. Screenshot was visually inspected. Selecting `tools` left the same dialect errors.

Authenticated dry-run API controls for this same route yielded zero candidates for `openai_compatible` and `openai_chat`, but selected both deployment IDs for `ollama`. These were dry-runs and sent no provider request. In contrast, a real Playground request through the same route returned HTTP 200; the persisted trace selected the first deployment with one attempt. The prior CP52 request also used the same route for a 503→200 multi-target failover. A temporary Playground key from the positive control was revoked through Access & Keys and persisted `REVOKED`.

## Defect disposition

DEF-028 is confirmed: Route Explain contradicts a successful live route, and the UI offers no dialect control or explanation. Source inspection shows the Console unconditionally sends `openai_compatible`; this is a hypothesis for the mismatch, not a claim that live route resolution fails. No additional defect was assigned. DEF-017 recurred as the successful Playground card remained pending.

Standalone handoff: [`reports/DEF-028-route-explain-rejects-working-ollama-route.md`](../reports/DEF-028-route-explain-rejects-working-ollama-route.md).

## Evidence and coverage

- `../evidence/CP54-route-explain-ollama-mismatch.json`
- `../evidence/CP54-route-explain-valid.png` and `CP54-route-explain-tools.png`
- `../evidence/CP54-playground-route-success.png`
- `../evidence/CP54-key-revoked.png`
- CP52 same-route failover: `../evidence/CP52-multi-target-failover-and-route-selector.json` and `CP52-playground-failover-result.png`.

**PASS:** one real standard streaming request on the route returned 200; the dry-run API itself correctly marked its output as dry-run and did not dispatch a live request.  
**FAIL:** DEF-028 false no-candidate result for the normal Ollama chat route.  
**UNVERIFIED:** other adapters/dialects, policy constraints, requested capabilities after dialect filtering, unregistered target models, and browser/role variation.

QA-002 remains in progress. No implementation was changed.
