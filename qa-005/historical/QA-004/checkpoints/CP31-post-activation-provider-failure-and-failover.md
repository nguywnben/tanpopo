# CP31 — Post-activation provider failure, priority failover, and recovery

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for first-fault dispatch protection, two-target priority failover, Virtual Key cleanup, and probe recovery using two isolated Ollama-compatible fixtures. No new defect ID is assigned in this checkpoint.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Test setup and starting state

The real Providers page showed `QA004 synthetic Ollama 401` ACTIVE at connection version 7. Its deployment was `ACTIVE_HEALTHY` v2 with `connection_state=ACTIVE`. A temporary file-controlled fixture on `127.0.0.1:18126` was configured to return synthetic HTTP 503 for provider requests. A separate already-active local fixture on `127.0.0.1:18125` supplied the healthy fallback. No external provider was used.

## Failure after activation

From the real Providers UI, Probe against the now-failing fixture displayed `synthetic upstream unavailable`. Persistent readback changed the connection to `DEGRADED` v8; the deployment stayed `ACTIVE_HEALTHY` v2 and exposed `connection_state=DEGRADED`. The failure-state screenshot was visually inspected.

A temporary single-target route and a UI-issued PROVIDER Virtual Key then sent a cookie-free `POST /v1/chat/completions`. Tanpopo dispatched exactly once to the failed local fixture and returned HTTP 502 `upstream_unavailable` (`retryable=true`). The persisted trace is `trc_01m89jmk2je009gm7t4wm4hpdh`, status 502, error `upstream_unavailable`, and cost 0 micro-USD. This is a controlled first failure; it did not establish circuit-breaker threshold behavior.

## Priority failover with a degraded primary

A second temporary route used `PRIORITY_FAILOVER`, max one hop, the degraded deployment first and a healthy local deployment second. One cookie-free request returned HTTP 200. The failed fixture observed one `/v1/chat/completions` attempt; the healthy fixture observed one authenticated-upstream request. The persisted trace `trc_01m89jr4cbe008s9v9jpreg3x8` selects the healthy secondary deployment `dpl_01m8969kjme00br1mjja9gam7q`, status 200, Exact metering, 10 ms, and 0 micro-USD. The actual Traces table screenshot was visually inspected; it shows the 200 failover trace beside the earlier 502 trace. This verifies the one-hop pre-response failover slice only. Circuit opening after sustained errors, retry budgets, SSE cancellation, and mid-stream failure remain UNVERIFIED.

The test key was revoked through the real Access & Keys UI and persisted as `REVOKED` v2. A cookie-free replay returned 401 `invalid_virtual_key`; both fixtures' request counts were unchanged. Both temporary routes were deleted and the route list returned to the sole original `qa004-loopback` entry.

## Recovery and an unclassified Route Explain observation

The controlled fixture was restored to success. A real Providers UI Probe succeeded in 9 ms and persisted the connection as `ACTIVE` v9. The attached deployment remained `ACTIVE_HEALTHY` v2 with `connection_state=ACTIVE`. The restored state and success banner were visually inspected.

During the degraded interval, Route Explain was submitted for the temporary route with `dialect=openai_compatible`. It returned HTTP 200 but selected no candidates, rejecting the deployment as `DIALECT_MISMATCH` because the deployment's `native_dialect` was empty. The same deployment did receive a `/v1/chat/completions` request and returned a provider error, and the two-target `/v1` inference succeeded through its fallback. This may be a request-dialect input issue or a Route Explain/runtime discrepancy; it is **unclassified**, not assigned a defect ID. A controlled Console comparison using its `Normalized` versus `openai_compatible` dialect options is the next investigation.

## Evidence and status

- Sanitized state, request, trace, failover, cleanup, and recovery observations: [CP31 results](../evidence/CP31-provider-failure-failover-results.json).
- Visually inspected real-browser screenshots: [degraded provider](../evidence/CP31-failure-after-activation-probe.png), [two-request Traces table](../evidence/CP31-failover-trace-list.png), and [successful restored provider](../evidence/CP31-provider-restored.png).
- **PASS (scoped):** post-activation 503 propagation, one-hop failover to a distinct healthy local provider, key revocation boundary, cleanup, and recovery probe.
- **UNVERIFIED:** sustained-error circuit threshold/open-circuit exclusion, cooldown and half-open recovery, other adapters/roles, retry/timeout/cancellation, streaming failover and external provider interoperability.
- **No new defect ID.** Existing historical/new registers remain in force; QA-004 continues.
