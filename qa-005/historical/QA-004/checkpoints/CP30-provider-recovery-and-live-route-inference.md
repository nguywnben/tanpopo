# CP30 — Provider recovery, live routed inference, key revocation, and FinOps reconciliation

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for the synthetic Ollama 401 → recovered connection → explicit deployment activation → real routed inference and key revocation journey. DEF-002 is further verified for this adapter and path. The same request confirms the existing FAIL findings DEF-052 and DEF-053. QA-004 remains ongoing.  
**Product source:** unchanged.

## Recovery path

The local synthetic connection from CP09 began `CREATED_UNVERIFIED`; its deployment was `PROVISIONED`. I replaced only the local 401 test fixture with an isolated success fixture. Its first version answered `/api/tags` but not the adapter's observed `GET /v1/models` probe path. Tanpopo's probe returned the fixture's 404 and correctly left the connection `CREATED_UNVERIFIED`; this was a test-fixture mismatch, not a product defect. After the fixture served an Ollama-compatible `/v1/models` response, the real Providers → Probe connection action succeeded in 6 ms and persisted connection `ACTIVE` at version 7.

The deployment remained `PROVISIONED` at version 1 after connection recovery; the Models UI offered Activate. Using the real Team Admin action changed it to `ACTIVE_HEALTHY`, connection `ACTIVE`, version 2. The explicit activation step is visible and persisted; connection recovery alone did not silently promote its deployment.

## Real routed inference and key boundary

The Team Admin Routes screen remained affected by DEF-054, so I created one temporary project route through its authorized API, targeting the now-active synthetic deployment. I issued a temporary PROVIDER key through the real Access & Keys form. Its first allowlist value was the route slug; a real cookie-free request received HTTP 403 `scope_or_plane_denied` because the key restricts request paths. The provider fixture saw zero requests. The UI hint describes path-prefix grants, so I updated the key through the real Edit plane & route grants dialog to `/v1/*`.

The next cookie-free `POST /v1/chat/completions` returned HTTP 200 and the fixture's synthetic response. The fixture observed exactly one `/v1/chat/completions` request. The returned usage was exact 7 input / 4 output tokens. The persisted trace appeared at the top of the real Traces table and its waterfall opened from the row. The UI shows HTTP 200, Exact, 7 ms, and `$0.000000`; screenshots were visually inspected.

The real Access & Keys UI revoked the test key. Its persistent record is `REVOKED` version 3. Replaying the same request returned HTTP 401 `invalid_virtual_key`, and the provider request counter remained at one. The temporary route was deleted; a fresh route read returned only the original `qa004-loopback` route. The one-time secret existed only in a mode-0600 `/tmp` file during the request and was removed after cleanup.

## Audit and billing reconciliation

The Owner's real Audit Log shows `virtual_key.created` (#186), `virtual_key.updated` (#187), and `virtual_key.revoked` (#188) for the same key target; route create/delete are #185/#189. The Owner Console verified an intact RFC 8785 JCS chain across 191 events. Team Admin audit API read was denied 403, consistent with the audit-read role boundary; the denial is not a product defect.

For the same request, the persisted Traces row says `usage_accuracy=EXACT`, `total_cost_micros=0`; the FinOps `group_by=key` summary says one exact request and `exact_micro_usd=1`. This is another independent reproduction of DEF-052. The configured card is 1,000 / 2,000 nano-USD, `self_hosted_zero_rate=false`; the returned exact 7/4 counters imply 15 micro-USD under the displayed per-token labels, while FinOps records 1 micro-USD. This reproduces DEF-053's public-unit/settlement mismatch. No new defect ID is assigned.

## Evidence and status

- Sanitized API/provider/FinOps/audit results: [CP30 provider recovery results](../evidence/CP30-provider-recovery-results.json).
- Visually inspected screenshots cover successful probe, deployment before/after activation, key grant form/update/revocation, Owner audit create/revoke and chain verification, the real trace list/waterfall, and FinOps group-by-key and Chargeback. In particular, the `QA004 provider recovery probe` row displays `Exact $0.000001` and `1 / 0` in [CP30-finops-key-row.png](../evidence/CP30-finops-key-row.png), independently inspected after capture. The trace screenshot for the same request displays `$0.000000`; the discrepancy is visible across both product screens.
- **PASS (scoped):** Ollama connection recovery, manual deployment activation, authorized live request, persistent trace, VKEY grant boundary and revocation.
- **FAIL (existing):** DEF-052 trace cost disagrees with FinOps; DEF-053 pricing unit contract disagrees with settlement.
- **UNVERIFIED:** post-activation failure, other adapter types, alternate roles, streaming cancellation, retries/failover and broader recovery.
- **No new defect ID.** `GATE-REL-07` remains BLOCKED; QA-004 continues.
