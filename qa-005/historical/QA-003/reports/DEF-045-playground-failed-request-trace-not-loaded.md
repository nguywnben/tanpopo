# DEF-045 — Playground failed requests do not load their persisted trace

**Severity:** Low  
**Status:** Confirmed on frozen RC5, reproduced twice in isolated SQLite runtime 18116  
**Affected:** Playground failure troubleshooting; failed `/v1/chat/completions` requests and their linked execution trace  
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Reproduction

1. Sign in as the synthetic Org Owner, select `Default Organization / default / dev`, and open Playground.
2. Set Model or route slug to a nonexistent synthetic value (for example `qa003-no-such-route-cp35`), enter a synthetic prompt, and click Send. The Playground mints its documented short-lived, budget-capped ephemeral session and calls the data plane.
3. The request returns HTTP 503 with `no_compliant_deployment_available` and `X-Tanpopo-Trace-Id`. The transcript shows the request error and retains the synthetic user turn.
4. Inspect Execution traces. It shows the trace ID followed by **“Trace not available yet.”**
5. Open Traces in the same `default/dev` scope. The failed request is in Recent requests. Open its row: the waterfall loads the same trace with HTTP 503, `no_compliant_deployment_available`, exact `$0.000000` cost, and the Policy stage.
6. Independently GET `/admin/v1/traces/{trace-id}` with the authenticated session. It returns HTTP 200 and metadata. The observed detail keys contain no prompt, completion, message, or content fields.

The full interaction was repeated with a second synthetic model and prompt. In the second run the data-plane response returned in 87 ms; the Playground still showed “Trace not available yet” after 1.1 seconds, while a direct no-store trace detail read returned HTTP 200. Reproduction screens and sanitized results: [Playground failure](../evidence/CP35-playground-error.png), [repeat timing](../evidence/CP35-playground-trace-timing.png), [Traces list](../evidence/CP35-traces-list.png), [loaded trace detail](../evidence/CP35-trace-detail.png), [first run result](../evidence/CP35-playground-error-result.json), [repeat trace timing](../evidence/CP35-trace-timing-result.json), [trace view](../evidence/CP35-trace-view-result.json).

## Expected versus actual

- **Expected:** when a failed data-plane response includes a trace ID, Playground should retrieve and show the persisted trace metadata, or communicate a bounded pending state and a usable route to Traces if retrieval genuinely fails.
- **Actual:** on the error branch the Playground displays the trace ID but labels it “Trace not available yet”; meanwhile the same record is persisted and available through the Traces page/API. No direct link or retry affordance is provided in the inline row.

## Impact

This hides useful failure metadata exactly when users need to diagnose an unsuccessful request. It adds a confusing second “trace unavailable” status after the error has already been recorded, and forces users to navigate to Traces and locate the ID manually. The tested error cost was zero and no real provider was contacted. No authorization bypass, payload exposure, or accounting error was observed.

## Facts and root-cause hypothesis

**Observed facts:** both synthetic missing-route requests returned 503 with a trace ID; the same IDs were persisted and readable from authenticated trace detail; the Traces screen displayed their metadata; the Playground inline status remained “Trace not available yet.” The captured failed-request trace response did not include prompt/completion fields.

**Hypothesis (not proven as root cause):** the Playground error handler records the returned request ID and marks trace loading complete without fetching trace detail, whereas the success branch calls the trace polling helper. The frozen UI source contains those distinct branches; this is consistent with the observed behavior, but backend timing or other causes have not been ruled out.

## Acceptance criteria

1. For an error response with `X-Tanpopo-Trace-Id` or `X-Tanpopo-Request-Id`, Playground fetches the associated trace and displays status, error code, duration, cost, and usage accuracy when available.
2. If the trace is temporarily absent, the UI clearly indicates pending state, retries within a bounded interval, then provides a working trace lookup/navigation action and does not assert permanent unavailability without evidence.
3. Failed-request traces remain metadata-only; prompts, completions, secrets, and credentials do not appear in the UI or trace response.
4. Verify 4xx/5xx errors, budget denial, cancellation, rapid subsequent sends, and scope switching; confirm trace IDs never point to another request and no stale result appears.

## QA scope limits

Only two synthetic no-route failures were exercised in this issue. Success streaming, cancellation, other data-plane errors, retries, other roles, and other browsers are not covered by this report. QA-003 coverage remains incomplete.
