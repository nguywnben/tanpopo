# CP115 — PostgreSQL/Valkey cross-process rate limit and key revocation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)  
**Result:** PASS (scoped) for application RPM enforcement shared between two exact-RC6 processes, followed by graceful revoke and replay denial. No new defect was assigned.

## Isolated runtime

The test used the CP114 post-shred PostgreSQL restore as its database and the isolated three-primary Valkey 8.1.12 cluster on loopback. Exact RC6 was running as a standalone process on `127.0.0.1:18137` and as a separate `data-plane` process on `127.0.0.1:18138`; both `/readyz` responses returned 200.

The model name was a synthetic value with no active deployment or provider connection. A first request therefore terminated locally with HTTP 503 `no_compliant_deployment_available`. No request reached a provider, and no provider spend occurred.

## Rate-limit and revoke journey

Using an authenticated Owner session, the Admin API issued a project/environment-bound PROVIDER Virtual Key with `rpm_limit: 1` and only the synthetic model allowed. Its one-time `secret_key` remained in the test process memory; it was not logged, persisted, included in screenshots, or committed. A second independent key repeated the lifecycle so that revoke replay could be checked while the one-time value was still available.

For each key, the first request through port 18137 returned HTTP 503 `no_compliant_deployment_available`. Within the same minute, the second request through the distinct port 18138 returned HTTP 429 `rate_limit_exceeded` with `Retry-After: 60`. Because no route/deployment existed, this demonstrates shared application-level RPM state across the two RC6 processes through the configured Valkey cluster without upstream dispatch.

The Owner used Access & Keys → Actions → Revoke key → Revoke (graceful drain) in the actual Console. Both UI actions returned HTTP 200 and the page showed “Virtual key revoked. 0 active streams severed.” Authenticated API readback returned `state: REVOKED`, version 2, and `rpm_limit: 1`. For the second key, immediate replay through both ports returned HTTP 401 `invalid_virtual_key`.

The stable active key screenshot, action menu, revoke confirmation and post-revocation state were visually inspected during the browser interactions. An early screenshot taken during list loading was discarded. The exact sanitized responses and key IDs are in [results](../evidence/CP115-valkey-cross-node-ratelimit-results.json).

## Limits

This is one scoped cross-process RPM and revoke test against an isolated local Valkey cluster. It does not cover behavior during Valkey outage/failover, concurrent fairness, TPM accounting, hierarchical policy precedence, long-duration load, real provider dispatch, or multi-host/network-fault behavior. The inspected Console issue form has no `rpm_limit` input although the API supports it; SCR-13 does not define rate-limit configuration in that form, so this is recorded as a scope observation and not assigned a product defect.

No product implementation was modified. QA continues; **GATE-REL-07 remains BLOCKED.**
