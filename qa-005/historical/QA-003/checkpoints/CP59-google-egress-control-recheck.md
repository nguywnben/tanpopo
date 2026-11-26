# CP59 — Google direct-egress control after Cloud network change

**Baseline:** QA-003 remains frozen at RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Scope:** one zero-cost read-only Google AI Studio `GET /v1beta/models` with configured QA credential; direct transport compared with the environment proxy
**Status:** control recheck complete; no Tanpopo adapter request or generation was made; `GATE-REL-07` remains BLOCKED.

## Results

- Current managed environment reports network policy `unrestricted` and enforced, and the Google QA credential binding is ready. This metadata alone is not proof of end-to-end egress.
- The authenticated direct/no-proxy request failed with `URLError` before an HTTP response.
- The same GET through the configured environment proxy returned HTTP 200 and 50 model entries. Only the first three public model identifiers and count were retained; no response body or credential value was saved.
- The check made no generative request, caused no model inference charge, and was not routed through Tanpopo. It is a provider reachability control, not a product probe or proof of adapter recovery.

Sanitized output: [CP59-google-direct-egress-control.json](../evidence/CP59-google-direct-egress-control.json).

## Classification

This repeats the earlier observed transport split under the currently published Cloud network policy: Google is reachable with the bound credential through the proxy, while direct client transport still cannot establish an HTTP response. It corroborates the prior Cloud egress classification and does not support changing Google adapter failure classification. A fresh authenticated Tanpopo probe remains unverified in this checkpoint; do not infer that the prior `upstream_unreachable` state was resolved or that it is a product defect.
