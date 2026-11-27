# CP119 — Provider probe redirect boundary and failure feedback

**Source:** frozen RC7 `737079ddb847d97dbcf832f610ed0048556cb65b`; compiled Linux/amd64 runtime SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.  
**Runtime:** isolated QA SQLite Owner Console at `127.0.0.1:18145`; Chromium 156 with Playwright. No provider credentials or paid services.  
**Disposition:** SSRF redirect test PARTIAL; UX result visibility recorded as PG-013 (contract gap, not defect). No change to the private-egress or metadata policy.

## Procedure and result

1. Started a disposable HTTP fixture bound only to `127.0.0.1:19100`. Every GET returned HTTP 302 with `Location: http://169.254.169.254/latest/meta-data/`; it logged method/path and whether Authorization was present.
2. In the real Owner Console → Providers form, created `QA005 SSRF Redirect Synthetic` for `http://127.0.0.1:19100`, using the explicit `127.0.0.1/32` private-egress scope. The UI showed the created connection as Unverified; API returned HTTP 201. Screenshot: [created connection](../evidence/screenshots/CP119-redirect-connection-created.png).
3. Invoked “Probe connection” twice from the row menu. Both Admin API calls returned HTTP 200 with `valid:false`, `failure_code=upstream_unreachable`, sanitized message `upstream connection could not be reached`; latency was 9 ms. The fixture logged exactly two unauthenticated GET `/v1/models` requests, each answered with the metadata redirect. It observed no follow-up request.
4. Immediately after the second failed response, visually inspected a real screenshot. The row still read “Unverified”; no failure detail, toast, or `role=alert/status` feedback appeared. See [probe result screenshot](../evidence/screenshots/CP119-probe-result-unverified.png).
5. Deleted the temporary provider connection through its real row action; HTTP 204; the row was absent from the next rendered list. Stopped the fixture. Port 19100 is closed.

## Classification and limits

**SSRF boundary — PARTIAL:** the explicitly allowed local endpoint was reached and returned a redirect to the documented hard-blocked metadata address; Tanpopo returned an unreachable failure, and the receiver saw no follow-up. This is consistent with redirect rejection. However, the environment provided no socket-level egress trace or metadata-side telemetry, so the destination rejection is not independently proven and is not marked PASS. No external provider was contacted.

**PG-013 — probe failure state visibility:** the API distinguishes a failed probe from success, but the real UI left the same Unverified row and exposed no result when the response arrived. Users cannot distinguish never-probed from a just-failed probe from this screen. The accepted SCR-04 text specifies success latency feedback but does not state a failed-probe rendering contract. Therefore this is a product-gap observation pending a UX contract, not a defect.

## Evidence and cleanup

Sanitized API/runtime facts: [CP119 evidence](../evidence/CP119-provider-probe-redirect.json). The two screenshots contain only synthetic account/connection data. The test connection was deleted (204), local fixture stopped, and no credentials, secrets, source or specification were changed. Audit-chain verification and alternate role/backend/browser behavior were not part of this slice.
