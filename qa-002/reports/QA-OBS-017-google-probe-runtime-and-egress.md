# QA-OBS-017 — Google probe failures separate runtime acquisition from Cloud egress

**Classification:** environment/configuration observation; no product defect assigned.  
**QA-002 baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Current browser probes:** Chromium, Org Owner, isolated runtimes 18084 and 18086.

## Current RC4 evidence

### Runtime 18084 — configured Google connection

- Providers → Connections showed two `QA002 Google Gemini` rows with identical visible endpoint, adapter, US/commercial region, Unverified state, and API-key fingerprint suffix. They remain distinct stored records (the existing DEF-032 duplicate-identity ambiguity); the probe targeted the first visible row only.
- The real UI action **Probe connection** sent `POST /admin/v1/connections/con_01m8530p3we00828f2x8mp6vs2/test`.
- Admin API returned HTTP 200 with `valid=false`, `failure_code=upstream_unreachable`, `sanitized_message="upstream connection could not be reached"`, and about 2,088 ms latency. The UI showed the same safe message; the connection remained Unverified. This independently repeats the RC4 CP17 result.

### Runtime 18086 — no Google connection to test

- A real Owner session showed one configured synthetic Ollama connection and zero Google/Gemini rows. No Google probe was sent; this runtime lacks the needed configured connection and bound Google credential.
- 18083 and 18085 were not probed for Google in this phase. Their available account/connection/credential state was not established. The live three-runtime comparison is therefore **INCOMPLETE**.

## Historical comparison, kept separate from QA-002 results

QA-001 CP26 records a different baseline (`a771311107c4e4c2c7d9b53a3f5a0926f114b799`) and three Cloud listeners: 8080 and restored 8082 returned HTTP 200 with `valid=false/upstream_unreachable`; listener 8081 returned HTTP 503 `probe_runtime_unavailable`. A shell request through the configured Cloud HTTPS proxy succeeded, while direct no-proxy egress timed out. That historical run supports two distinct environmental hypotheses, but it is not inherited as an RC4 product PASS or a current three-runtime retest.

QA-002 CP17 on RC4 independently reports the app probe `upstream_unreachable` while a shell Google request through the Cloud proxy succeeded and direct socket egress was unavailable. CP86’s managed Cloud diagnostics also found proxy-mediated HTTPS worked while direct no-proxy DNS failed. The provider transport intentionally avoids ambient HTTP proxies to prevent proxy behavior from bypassing endpoint/SSRF protections.

## Classification

- **`upstream_unreachable`:** current RC4 reproduction plus proxy/direct-path evidence makes managed Cloud direct-egress incompatibility the supported explanation. This is not classified as a product defect. No successful Tanpopo-to-Google inference is claimed.
- **`probe_runtime_unavailable`:** historical QA-001 listener-specific runtime acquisition/configuration failure, not reproduced on current 18084; its exact factory cause was not exposed. It is distinct from upstream reachability and remains **UNVERIFIED on RC4**. No product defect is assigned without a current reproducible failure under supported runtime configuration.
- **Provider/adapter correctness:** Google model-list/probe via a reachable app transport and app-mediated inference remain unverified. The historical proxy shell test validates Google account/model access over that proxy, not Tanpopo’s guarded transport.
- **Duplicate rows:** DEF-032 remains a separate UI identity issue. The attempted probe used a specific API ID and does not resolve how users should choose between visually identical records.

## Evidence

- `evidence/CP93-provider-inventory.png` — current 18084 duplicate rows, visually inspected.
- `evidence/CP93-google-probe-first-duplicate.png` — real probe result and unchanged Unverified rows, visually inspected.
- `evidence/CP93-google-probe-first.json` — sanitized response status/code/message and selected row index.
- `evidence/CP93-google-runtime-18086-inventory.png` — no Google connection in 18086, visually inspected.
- `evidence/CP93-google-runtime-18086-inventory.json` — sanitized inventory and no-probe status.
- `evidence/CP93-google-connections-api-sanitized.json` — current connection IDs/state/fingerprint suffixes only; no secret values.
- Historical QA-001 references: `qa/evidence/phase26-provider-runtime-comparison.json`, `qa/PRODUCT-COVERAGE-MATRIX.md` CP25/26, and QA-002 `checkpoints/CP17-fresh-setup-google-boundary.md`. QA-001 baseline and interpretation remain preserved separately.

## Limits / next reproducible work

- A current three-runtime differential requires the same supported Google adapter, authorized test credential, and matching connection configuration in each runtime, especially a repeat of the former 8081 runtime-acquisition case. Those prerequisites were not present/established for 18086/18083/18085 in this phase.
- To resolve the current Cloud block, run the exact RC4 app probe and a minimal Google request in an environment where guarded direct egress is supported, while retaining explicit target validation. Do not route around SSRF checks or infer product failure from the Cloud proxy/direct split.

QA-OBS-017 remains an environment observation. GATE-REL-07 remains BLOCKED.
