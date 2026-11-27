# CP93 — Google provider probe and runtime evidence reconciliation

**QA-002 baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Current runtime probes:** 18084 and 18086, authenticated real Chromium Console  
**Gate:** `GATE-REL-07 BLOCKED`

## Current test actions and results

- On 18084, inspected Providers → Connections and visually confirmed two duplicate Google Gemini rows with identical display identity/fingerprint suffix (DEF-032 context).
- Used the actual row menu’s **Probe connection** action on the first visible Google row. Its specific Admin API test returned HTTP 200 `valid=false`, `upstream_unreachable`, `upstream connection could not be reached`; latency was about 2,088ms. UI displayed the safe message and the row remained Unverified. No model was promoted and no inference was sent.
- On 18086, signed in to the QA Owner and visually inspected the provider list. It had one synthetic Ollama connection and zero Google rows. No Google probe was attempted on this runtime because there was no configured Google credential/connection.
- Did not probe 18083 or 18085; their matching Google connection and credential availability were not established. Therefore this is not a complete current three-runtime comparison.

## Classification and historical reconciliation

- **`upstream_unreachable`:** current RC4 reproduction agrees with CP17. Historical CP26 (QA-001, different baseline) also observed it on listeners 8080 and restored 8082. QA-001 separately documents proxy success/direct egress failure; CP86 independently confirmed this managed Cloud network split. This supports Cloud direct-egress incompatibility, not a Tanpopo defect. No successful Tanpopo Google inference is claimed.
- **`probe_runtime_unavailable`:** QA-001 CP26 observed it only on listener 8081 while other listeners could acquire a probe runtime. It was not reproduced on RC4 18084; runtime 18086 has no Google record. The exact factory cause and RC4 reproduction are UNVERIFIED. Treat as listener-specific environment/configuration evidence pending same-baseline reproduction.
- Google adapter model validation, actual app-mediated inference, other credentials, and the old 8081-specific factory failure remain UNVERIFIED. Duplicate connection selection remains DEF-032.

The QA-001 material is cited as historical diagnostic context only; no QA-001 PASS is inherited into QA-002.

## Evidence

- `evidence/CP93-provider-inventory.png`
- `evidence/CP93-google-probe-first-duplicate.png`
- `evidence/CP93-google-probe-first.json`
- `evidence/CP93-google-runtime-18086-inventory.png`
- `evidence/CP93-google-runtime-18086-inventory.json`
- `evidence/CP93-google-connections-api-sanitized.json`
- `reports/QA-OBS-017-google-probe-runtime-and-egress.md`

All current screenshots were visually inspected. No secrets or raw provider response bodies were captured.

## Consolidated status

- **Historical DEF-001–015:** no defect retest in CP93; CP92 most recently retested DEF-001. Prior scoped dispositions remain, DEF-005 partial, independent acceptance incomplete, no QA-001 PASS inherited.
- **Confirmed open defects:** DEF-016–034 remain open; no status change. Preserve DEF-034 and DEF-020 recurrence.
- **New findings:** no defect. QA-OBS-017 records Cloud egress/runtime acquisition distinctions; DEF-032 remains the existing duplicate-row usability defect.
- **Coverage completed:** one current RC4 Google UI probe on 18084; 18086 inventory shows no Google connection; Cloud path classification reconciled with historical evidence.
- **Coverage blocked:** current same-config three-runtime Google comparison lacks a Google connection/credential on 18086 and was not established on 18083/18085; full app-mediated Google inference awaits guarded direct egress. Existing webhook, disabled-A2A-runtime, RC4 artifact and OTel collector blockers remain.
- **Coverage remaining:** independent DEF-002–015 residual scenarios; current same-config 3-runtime Google test; DEF-005 SSRF cases; DEF-007 failures/populated erasure; all open DEF-016–034 regression/remediation; remaining roles, products, browsers/platforms and integrations.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. No engineering handoff for environment-classified Google transport failure.

QA-002 remains in progress.
