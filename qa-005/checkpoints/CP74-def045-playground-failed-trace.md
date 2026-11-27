# CP74 — DEF-045 failed Playground trace visibility retest

**Source:** frozen unsigned RC7 source commit `737079ddb847d97dbcf832f610ed0048556cb65b`; Linux/amd64 runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; disposable SQLite clone at `127.0.0.1:18162`; Chromium 156; authenticated synthetic Org Owner; QA005 organization/project/environment scope. The same runtime includes a local deterministic route configuration; this test deliberately selected a nonexistent route and made no provider call.

**Disposition:** **PASS (scoped QA-005 retest)** for inline failed-request trace resolution and navigation to persisted trace detail. Historical QA-003 DEF-045 failures remain preserved. This result does not claim all failed-request classes or browser/runtime combinations are fixed.

## Reproduction and result

1. Signed into the real Console as `qa005-owner@example.invalid`, navigated to Playground, and selected custom slug `qa005-no-such-route-cp74`.
2. Sent `QA005 DEF045 deterministic missing-route trace lookup.` The real `POST /v1/chat/completions` returned HTTP 503 `no_compliant_deployment_available`; response body length was 251 bytes and the request had trace ID `trc_01m8dh3hmfe00ajn8snnzytgkf` in the first run. A second independently issued request returned HTTP 503 with trace ID `trc_01m8dh6gyce0081zy70whd0ert`.
3. On both observed screens, Playground rendered an **Execution traces** card with trace ID, `/v1/chat/completions`, HTTP 503, `$0.000000`, duration and an **Inspect trace** action. The former `Trace not available yet` placeholder did not appear.
4. Clicked **Inspect trace** for the second request. The Console navigated to Traces, showed the same ID in Recent requests, and opened its trace waterfall. The detail displayed route, HTTP 503/error, unsettled `$0.000000` usage, 1 ms duration and the Policy stage. An authenticated detail API read during the first run returned HTTP 200 and `no_compliant_deployment_available` metadata.
5. Visually inspected the actual Chromium captures: [Playground failed response with inline trace](../evidence/screenshots/CP74-playground-failed-trace.png) and [Traces list with matching waterfall detail](../evidence/screenshots/CP74-playground-inspect-trace.png). The first capture visibly shows the failed request and inline trace card. The second shows the matching request in the table and the corresponding detail panel.

Sanitized run metadata is in [CP74 evidence JSON](../evidence/CP74-def045-playground-trace.json).

## Classification and limits

This scoped retest passes the persisted pre-dispatch no-deployment 503 case twice, with one full Playground-to-Traces navigation. It is not a proof for upstream/provider errors, timeouts, streaming failures, cancellation, revoked sessions, role/scope changes, alternate database backends, WebKit/Firefox, or other locales. The historical QA-003 double reproduction is not erased. No paid provider was called and no Tanpopo source/specification changed. `GATE-REL-07` remains BLOCKED.
