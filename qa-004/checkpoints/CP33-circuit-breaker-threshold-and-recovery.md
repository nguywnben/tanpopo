# CP33 — Sustained 503 circuit opening and post-cooldown recovery

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for five-failure primary circuit exclusion and recovery after cooldown through three successful primary requests. No new defect ID.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Sustained failure and failover

The primary's isolated local Ollama-compatible fixture at `127.0.0.1:18125` was put in `fail_503` mode; the secondary fixture at `127.0.0.1:18126` remained healthy. A real Team Admin UI-issued PROVIDER Virtual Key called a temporary two-target `PRIORITY_FAILOVER` route with the failing deployment first, max hops 1.

Requests 1 through 5 each returned HTTP 200 from the healthy secondary after one HTTP 503 at the primary. The primary fixture counted exactly five `/v1/chat/completions` requests and the fallback fixture counted five new requests. Request 6 still returned 200; its primary call count stayed at five while the fallback count increased to six. Persistent trace readback identifies the fallback deployment on all six. The real Traces table screenshot shows the six HTTP 200 rows. The app does not expose its internal breaker state in this readback; the fixture counters and selected-deployment traces are the direct evidence that the primary was skipped after the failure streak.

## Cooldown and primary recovery

After more than 30 seconds elapsed from the open-circuit observation, I restored the original successful local provider fixture on port 18125. Three subsequent cookie-free requests through a fresh temporary route all returned HTTP 200 from the primary deployment; the primary fixture count advanced by three and the fallback count did not change. All three persisted traces select the primary. The real Traces table screenshot shows the recovery requests above the six failover requests.

This is consistent with the frozen baseline's circuit behavior for this local route: five consecutive system faults opened the circuit, subsequent traffic bypassed the primary, and after cooldown the primary re-entered routing successfully. The exact transition timestamp and internal `HALF_OPEN_PROBING` state are not exposed via an Admin read endpoint, so those implementation details are inferred from the observable request sequence. Concurrent half-open admission, a failed half-open probe, additional 429/Retry-After handling, and mid-stream errors remain unverified.

The temporary VKEY was revoked through the real UI and persisted `REVOKED` v2. The temporary route was deleted. The original primary fixture was restarted in success mode; the only remaining route is `qa004-loopback`. No external providers or external spend were involved.

## Evidence and status

- Sanitized request-by-request fixture counts, route targets, traces, cooldown recovery, and cleanup: [CP33 result](../evidence/CP33-circuit-breaker-results.json).
- Visually inspected real-browser screenshots: [six-request failover traces](../evidence/CP33-circuit-threshold-traces.png) and [post-cooldown primary recovery traces](../evidence/CP33-circuit-recovery-traces.png).
- **PASS (scoped):** five consecutive synthetic primary 503s, single-hop failover, primary suppression on request six, and successful primary use after cooldown.
- **UNVERIFIED:** exact internal breaker state readback, concurrency behavior in half-open, failed half-open recovery, 429/Retry-After boundaries, mid-stream/no-failover lockout, and other adapters/roles.
- No new defect ID. QA-004 remains ongoing.
