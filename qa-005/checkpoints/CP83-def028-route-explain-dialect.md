# CP83 — DEF-028 Route Explain ingress-dialect retest

**Status:** PARTIAL (bounded dry-run regression only)  
**Source:** exact frozen unsigned RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime:** locally compiled binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite at `127.0.0.1:18162`  
**Browser:** Chromium 156  
**Scope:** Owner in `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev`; route `qa005-playground-low`; synthetic loopback deployment `dpl_01m8d7p2t3e00b7sh8ny9trtct`.

## Reproduction and observation

1. Open Routes & Policies → Route Explain in the actual Console.
2. Select `qa005-playground-low` and submit the default `Unified normalized (/v1/*)` dialect.
3. Repeat with explicit `ollama` dialect.
4. Inspect the dry-run decision, persisted route/deployment state, and isolated provider request counter.

Both real UI submissions returned HTTP 200 and `dry_run=true`. Each selected the same one healthy deployment with reason `MATCHED_PRIMARY_HEALTHY_PRIORITY_FAILOVER`; neither reported a dialect rejection. The screenshots show the Ingress dialect selector and one eligible candidate. No upstream request or budget hold was generated. Screenshots were visually inspected: [normalized](../evidence/screenshots/CP83-def028-normalized.png), [Ollama](../evidence/screenshots/CP83-def028-ollama.png). Sanitized response evidence: [CP83 JSON](../evidence/CP83-def028-route-explain.json).

## Disposition and limitation

This does not reproduce the historical `DIALECT_MISMATCH` result for this existing healthy route and shows the Console exposes explicit dialect selection. However, this is only dry-run evidence. Attempts to establish a live positive control did not produce a successful correlated request: a synthetic Virtual Key attempt returned 401 `invalid_credential_plane`, and a follow-up did not yield a matching live trace. The two temporary synthetic keys were revoked; no provider was contacted and no paid usage occurred. Therefore, the full live routing acceptance criterion is not independently established, and DEF-028 remains **PARTIAL**, not closed.

Reproduce on another adapter and role, prove a successful same-route live request with matching ingress dialect, prove the mismatched dialect is rejected as specified, and reconcile resulting trace/upstream counters before considering closure. Preserve the prior QA-002 evidence as historical intake.
