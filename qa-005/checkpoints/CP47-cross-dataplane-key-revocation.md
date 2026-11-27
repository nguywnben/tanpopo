# CP47 — Cross-Data Plane Virtual Key revocation

**Result:** PASS (scoped orderly revocation propagation)  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source Linux binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`  
**Environment:** isolated PostgreSQL 16; three-node Valkey 8.1 Cluster; exact-source Control Plane `:18151` and Data Planes `:18152`/`:18153`; Chromium synthetic Owner. No provider route or paid service was configured.

## Reproduction and results

1. In the real Access & Keys UI, issue an environment-bound PROVIDER Virtual Key. The one-time plaintext was held in process memory only and not captured in evidence.
2. Send a valid synthetic chat-completions request to Data Plane 18152. Authentication and admission succeeded; dispatch then failed closed with HTTP 503 `no_compliant_deployment_available` because the synthetic model had no route/deployment.
3. Revoke the key through the Control Plane UI using Actions → Revoke key → Revoke (graceful drain). The Admin API returned HTTP 200, state `REVOKED`, version 2, and zero severed streams.
4. Replay the same in-memory credential at independent Data Plane 18153. It returned HTTP 401 `invalid_virtual_key`.

The final Access & Keys table shows the key as **Revoked** and displays the success notice. The screenshot was visually inspected: [revoked inventory](../evidence/screenshots/CP47-vkey-revoked.png). Sanitized request IDs, key metadata, result and limits are recorded in [CP47 evidence](../evidence/CP47-cross-dataplane-virtual-key-revocation.json).

This proves one orderly Control Plane revocation is observed by another Data Plane in this exact-source local PostgreSQL/Valkey setup. It does not verify propagation latency, active stream severance, race conditions, Valkey failure/failover or fallback paths, tenant/role matrices, or production network behavior. No source or specification changes were made.
