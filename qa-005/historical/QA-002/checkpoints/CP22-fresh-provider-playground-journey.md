# CP22 — Fresh runtime provider-to-Playground journey

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite at `127.0.0.1:18086`; disposable Ollama-compatible HTTP fixture at `127.0.0.1:19106`; real Chromium interactions at 1440×1000.  
**Implementation changes:** none.  
**Release gate:** GATE-REL-07 remains **BLOCKED**.

## Marketplace, connection, model, route, inference

On the fresh-install runtime from CP17/21, the Installed extensions page initially had no package. The real Marketplace flow staged official-origin `@tanpopo/ollama`; the package publisher was unsigned. I inspected the complete permission delta: loopback hosts, `api_key` secret slot, provider hooks, declarative execution tier, private-IP access enabled, TLS enforcement disabled, request/response body inspection disabled, and default resource limits. After checking the explicit consent box, the package became Active 0.1.0. The review, checked-consent, and activated states are in `evidence/CP22-ollama-review-consent.png`, `CP22-ollama-consent-checked.png`, and `CP22-ollama-activated.png` and were visually inspected.

Through Providers, I created `QA002 CP22 Synthetic Ollama` at the isolated loopback fixture, selected SELF_HOSTED_INFRA / ON_PREM and explicitly allowed `127.0.0.1/32`. The connection persisted as Unverified until the real Console Probe action; the probe returned HTTP 200, displayed success in 5 ms, and changed state to Active. Screenshots were visually inspected. The extension/connection and state evidence is sanitized in `evidence/CP22-synthetic-provider-persistence.json`.

Through Models, I registered `qa002-cp22-ollama` with no declared capabilities, then created its deployment at 1,000 nano-USD per input/output token; the deployment activated as ACTIVE_HEALTHY. A real Playground request against that route returned HTTP 400 `capability_not_supported` before selecting a deployment. The persisted trace corroborates the rejection. This is a negative capability-gating result, not a defect: the next model explicitly declared `streaming`.

I registered `qa002-cp22-streaming`, created another healthy deployment and a route `qa002-cp22-streamroute`. A first synthetic streaming attempt returned HTTP 502 `upstream_malformed_response` because the fixture incorrectly returned a JSON object for a streaming request. I corrected only the disposable fixture to return valid SSE frames matching the published Ollama cassette contract, then retried through the real Playground. The UI showed the synthetic completion and HTTP 200; read-only SQLite independently confirmed the selected deployment, 13 input + 10 output tokens, EXACT usage, and a 1-micro-USD settlement. The valid SSE screenshots were visually inspected. This 502 was fixture-induced; it is not classified as a product defect.

One earlier route-create attempt showed an optimistic-concurrency conflict and duplicate-slug error while a row with that slug appeared in the list. Its request/response sequence was not captured, so I did not classify it as a defect. A clean single-submit retry using a new unique slug returned one POST HTTP 201 followed by GET HTTP 200 and exactly one persisted row; `evidence/CP22-route-create-repro.json` and the visually inspected `CP22-route-create-repro-result.png` show that control result. The original conflict remains **UNVERIFIED** pending a controlled reproduction with request capture.

## DEF-017 — Playground trace metadata identifier mismatch

After the successful inference I waited six seconds and visually inspected the result card. It still showed `Trace metadata pending…` for `req_01m856b4yye009p484h39hr8ea`. Browser evidence captured five calls to `/admin/v1/traces/{req_…}`, all HTTP 404. Authenticated lookup for that `req_…` ID also returned 404, while lookup for the persisted `trc_01m856b4yze00960j2ygdcaha2` returned HTTP 200. Traces listed the successful `trc_…` row. This is a confirmed moderate UI/data-link defect, DEF-017; full report and acceptance criteria are in `reports/DEF-017-playground-trace-metadata-id-mismatch.md`.

The same trace-detail response reports `total_cost_micros: 0` while a separate usage settlement row records 1 micro-USD. The storage tables use separately generated request identifiers and have no foreign key between them; without a documented trace-cost contract or stable correlation, I record this as an **unresolved observation**, not another defect. Check `CP22-playground-trace-id-correlation.json` and `CP22-synthetic-provider-persistence.json`.

## Scope and limitations

- **PASS, scoped:** real Chromium fresh-runtime Marketplace activation, synthetic connection probe and persistence, model/deployment/route creation, streaming synthetic inference, exact usage persistence, and visibility of the successful trace in the Traces screen.
- **FAIL, confirmed:** DEF-017 Playground trace-card lookup/pending behavior.
- **Expected negative:** model with no declared streaming capability is rejected as `capability_not_supported` before upstream dispatch.
- **Fixture correction:** malformed first SSE fixture response was classified `upstream_malformed_response`; corrected SSE fixture later succeeded.
- **BLOCKED:** real Google inference remains blocked by the Cloud proxy-only egress/upstream boundary recorded in CP17.
- **UNVERIFIED:** production Ollama compatibility, other providers/auth modes, failover/cancellation, route capability UX beyond this case, comparison mode, and cross-browser behavior.
- **UNVERIFIED:** RC4 artifact on Hydrogen’s separate DevBox. The isolated binary is source-default `0.1.0-dev`; no release linker provenance is claimed.
- No QA-001 finding was altered or marked passed from this checkpoint. No release decision was made.

Screenshots and request/SQLite evidence are listed in the DEF-017 report and the CP22 evidence directory. The fixture and server contain only synthetic data. No secrets or prompt content were persisted in trace evidence.
