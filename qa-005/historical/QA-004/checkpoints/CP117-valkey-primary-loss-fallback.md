# CP117 — Valkey primary loss and per-process rate-limit fallback

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (artifact run 45581179274)
**Result:** PASS (scoped) for observed local RPM fallback on the two isolated application processes and graceful key revocation after cache recovery. Automatic Valkey failover/rejoin remains UNVERIFIED. No product defect was assigned.

## Reproduction and observations

The test used the isolated PostgreSQL restore, exact RC6 listeners on `127.0.0.1:18137` and `:18138`, and a loopback-only Valkey 8.1.12 cluster. Before fault injection, the three-primary/no-replica cluster reported all 16,384 slots assigned and healthy. The synthetic organization's RPM key mapped to hash slot 10755, owned by node 2 (slots 5461–10922).

With the cache healthy, a synthetic key named `QA004 CP117 Valkey Primary Loss` had `rpm_limit: 3`. Two requests through listener 18137 each returned HTTP 503 `no_compliant_deployment_available`; there was no model deployment, provider configuration, outbound request, or provider spend.

Node 2 was stopped. Both application listeners continued to report readiness HTTP 200. With the limiter's owning slots unavailable, each application process accepted one local-fallback request (HTTP 503 no-route), then rejected its next request with HTTP 429 `rate_limit_exceeded`. This is consistent with the documented local fallback slice of the global RPM limit divided across three configured nodes: one request per process in this sample. It is not evidence of aggregate global enforcement during a partition, nor does it test an actual replica promotion.

After restart, node 2 came up with a new node identity and did not automatically rejoin the existing cluster or reclaim its prior slots. The test environment had no replicas and no durable Valkey state. The isolated cache topology was repaired manually by admitting the restarted node, forgetting its obsolete identity, and assigning the 5461–10922 range; all three nodes then reported `cluster_state:ok`, 16,384 assigned/healthy slots, and three known nodes. Both RC6 listeners remained ready. This manual repair is an environment recovery step, not a product fix or a successful automatic failover result.

The actual Console Access & Keys view was used to gracefully revoke the synthetic key after recovery. The visible success banner stated that the key was revoked and zero active streams were severed; readback showed `Revoked`. The screenshot was visually inspected. The key state was later confirmed by API as `REVOKED` version 2. No plaintext key was saved in evidence.

## Evidence and limits

Sanitized request, readiness, cache-topology, cleanup, and limitation details are in [results](../evidence/CP117-valkey-primary-loss-fallback-results.json). The visually inspected Console screenshot is [the post-recovery revoke state](../evidence/CP117-valkey-primary-loss-recovered.png).

This is a bounded single-owner-slot outage sample. The QA cluster had three primaries and zero replicas; the container runtime does not support pause/freeze, and restart created a fresh Valkey node identity that required manual cluster repair. Replica promotion, automatic slot migration/rejoin, partition healing, active concurrent traffic, rate-limit key recovery, sustained throughput, and production fault-domain behavior remain UNVERIFIED. No provider route was configured, so no provider egress or spend was possible. No Tanpopo product source was modified. QA continues; **GATE-REL-07 remains BLOCKED.**
