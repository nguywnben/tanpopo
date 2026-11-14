# ADR-0005: Integer `Micro-USD` Two-Phase Budget Ledger, Accuracy Labeling & Strict Commercial API Billing Separation

## Status
**`PROPOSED`** *(Awaiting Product Owner approval at Gate G3 & load verification via `SPIKE-03`)*

## Date
`2026-11-11`

## Context
Existing AI gateways frequently exhibit three severe financial engineering flaws:
1. Checking remaining budget *before* forwarding and debiting *after* completion without locking an in-flight reservation, allowing 50 concurrent streams to slip past admission when only cents remain (`RSK-007`).
2. Storing currency in IEEE-754 floating-point columns (`FLOAT`/`DOUBLE`) causing cumulative rounding drift, or recording bogus `0 USD` entries whenever an SSE stream disconnects mid-flight or an upstream omits token counts (`TP-BR-0703`).
3. Blurring legitimate commercial API key billing with unofficial adapters that scrape or proxy personal consumer subscriptions (`REJ-003`).

## Decision
1. **Strict 64-Bit Integer Monetary Arithmetic (`64-bit Integer Nano/Micro-USD`):** Persist unit token rates in `nano-USD / token` (`10^-9 USD`) and ledger balances/settlements in `micro-USD` (`10^-6 USD`, using conservative ceiling rounding), banning `FLOAT` types across all financial tables.
2. **Two-Phase Budget Hold Protocol (`Two-Phase Hold` — `SM-07`):** Prior to opening any upstream network socket, atomically lock a worst-case ceiling estimate (`RESERVED_HOLD`) across the full `Virtual Key → Env → Project → Org` hierarchy with a bounded lease (`lease_expires_at`). Upon stream completion, release unused hold capacity and append an immutable row to `usage_settlements`; if actual final usage marginally exceeds the remaining ceiling at stream end, record the exact overrun honestly in `negative_carryover_micros` and hard-block subsequent requests (`BA-OQ-002`).
3. **Mandatory 3-Tier Usage Accuracy Labeling (`EXACT` | `ESTIMATED` | `UNMETERED_FLAGGED`):** Segregate accuracy tiers into explicit columns across all `Chargeback` exports and emit response header `X-Tanpopo-Usage-Accuracy`.
4. **Hard Billing Mode Boundary (`billing_mode`):** Accept exclusively `'COMMERCIAL_API'` and `'SELF_HOSTED_INFRA'`, with zero mechanisms to pool, proxy, or convert personal consumer subscription plans into shared API budgets.

## Alternatives Considered

### Option B: Post-Hoc Asynchronous Balance Checks (`Post-Hoc Async Accounting`)
- *Pros:* Zero pre-request reservation overhead.
- *Cons:* Allows parallel request bursts to burn multiples of a hard budget ceiling before asynchronous reconciliation reacts (`UC-08`).

### Option C: Hard Mid-Stream Socket Termination Upon Touching Estimated Ceiling (`Hard Mid-Stream Cut`)
- *Cons:* Truncating a streamed answer or tool-call JSON payload on its final tokens corrupts the entire response for the caller even though the upstream AI provider still bills for every token generated up to that point.

## Rationale
Pre-admission budget holds (`Pre-Request Hold`) stop concurrent overspend storms at the gate, while bounded `Negative Balance Carryover` preserves response integrity for legitimately admitted streams without hiding a single micro-dollar from financial governance.

## Consequences
- **Positive:** Enforceable hard spend ceilings under heavy concurrency; 100% accounting transparency separating exact provider-reported usage from fallback estimates for finance teams.
- **Negative / Trade-offs:** When a caller omits `max_completion_tokens`, Phase 1 temporarily reserves the policy's default output token ceiling (e.g., `4096` tokens), holding slightly more quota than eventual actual usage during the few seconds the stream is in flight.

## Reversibility & Migration Implications
- **Reversibility:** **Hard to reverse** at the `BIGINT micro-USD` ledger schema layer once production financial history accumulates, whereas Phase 1 hold-estimation heuristics remain easily tunable via policy configuration with zero schema migration.
