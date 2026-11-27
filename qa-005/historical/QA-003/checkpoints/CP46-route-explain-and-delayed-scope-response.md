# CP46 — Route Explain eligibility and delayed route response

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated QA RC5 service on `18116`, synthetic Owner, Chromium 156/Linux at 1440×1000<br>
**Disposition:** DEF-030 stale route behavior **not reproduced in this scoped race**; remains **PARTIAL**. DEF-028 remains **UNVERIFIED** because the selected provider connection is archived and the dry run never reached dialect eligibility. `GATE-REL-07` remains **BLOCKED**.

## Route Explain — inconclusive for DEF-028

The real Routes & Policies Route Explain UI was set to route `qa003-cp45-identity-probe`, residency `On-prem`, and ingress dialect `ollama`. The page identified itself as a dry run without upstream egress or budget holds. It returned HTTP 200 and `dry_run: true`, matched the requested route, but selected zero eligible candidates. The sole rejection was `HARD_COMPLIANCE_FILTER / CONNECTION_INACTIVE`: its synthetic Ollama connection was already archived. This occurred before any dialect-specific eligibility decision and therefore does not reproduce or disprove QA-002 CP54's working-Ollama versus `DIALECT_MISMATCH` behavior.

No inference or provider dispatch was made. See the sanitized [dry-run response](../evidence/CP46-route-explain-result.json) and visually inspected [Route Explain result](../evidence/CP46-route-explain-ollama-result.png).

The authenticated connections list exposes this same inactive connection as `DISABLED`, while the Route Explain diagnostic says `ARCHIVED`. The OpenAPI mapping intentionally coalesces several canonical inactive states (suspended, revoked, archived) to `DISABLED`; this difference is recorded as a detail-level observation, not a confirmed defect. The disabled listing and explain response identify the same synthetic connection.

## DEF-030 — held route response across project scope

The `default/dev` scope now contained the synthetic persisted route created in CP45. In a fresh Chromium Owner session, Playwright fetched the authenticated HTTP 200 routes list (one row) and held the response before fulfillment. The Owner used the actual workspace switcher to select `QA003 Empty Project DEF035 / No env`. The page showed its no-environment banner, `Configured routes 0`, empty state, and disabled creation control before the held response was released. After release and settling, the page remained at zero routes; the old route did not appear under the sibling project. The actual screenshots were visually inspected.

See [sanitized held-response/state record](../evidence/CP46-def030-scope-race.json), [held-request screenshot](../evidence/CP46-route-response-held.png), [empty scope before release](../evidence/CP46-empty-project-routes-before-release.png), and [after release](../evidence/CP46-empty-project-routes-after-release.png).

DEF-030 moves from **UNVERIFIED** to **PARTIAL**. This is only one delayed-response non-reproduction for an Owner switching environment `default/dev` to an empty sibling project with no environment. Repeated/out-of-order requests, populated cross-project routes, environment-to-environment switching, refresh/back navigation races, other roles and WebKit remain **UNVERIFIED**. No new defect was confirmed.

## Coverage and QA status

- `FT-10` / `SCR-08` gain scoped route persistence, Route Explain failure-classification, and delayed scope-response evidence; remain **PARTIAL**.
- Historical defects: 17 VERIFIED FIXED for scoped RC5 tests; 10 PARTIAL (including DEF-027, DEF-029 and now DEF-030); 12 UNVERIFIED. These are not full acceptance closure.
- DEF-028 remains UNVERIFIED. Healthy upstream behavior and dialect-only candidate evaluation are unavailable in this isolated fixture.
- New defects DEF-040–046 remain open; no new finding was confirmed.
- No product source was modified. `GATE-REL-07` remains **BLOCKED**.
