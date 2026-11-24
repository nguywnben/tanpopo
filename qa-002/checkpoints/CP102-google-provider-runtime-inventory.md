# CP102 — Google provider setup comparison across isolated runtimes

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`<br>
**Runtimes:** `18083`–`18086`; authenticated real Chromium Owner sessions where credentials worked<br>
**Product implementation:** unchanged<br>
**Release gate:** `GATE-REL-07 BLOCKED`

## Method and results

Opened Providers in the real Console on each isolated runtime and read the authenticated connection, adapter, and extension-installation collections. No connection test, provider inference, or provider/configuration mutation was submitted. The retained QA Owner credential for 18085 was tried once; the Console remained at `/login` and showed “The email address or password is not valid.” That screenshot was inspected. Screenshots of the three authenticated inventories were also visually inspected.

| Runtime | Browser result | Google setup | Other observed setup |
| --- | --- | --- | --- |
| 18083 | Owner authenticated | 0 connections, 0 adapters, 0 installations | Empty provider state |
| 18084 | Owner authenticated | 2 `QA002 Google Gemini` connection rows, 1 Google adapter; both connections Unverified and visually identical | 5 connections total, 2 active adapter installations (Google and Ollama) |
| 18085 | Authentication blocked | Not inspectable | Saved 18083 Owner credential rejected; no authenticated API call attempted |
| 18086 | Owner authenticated | 0 Google connections/adapters | 1 active synthetic Ollama connection and 1 adapter/installation |

The 18084 inventory reproduces the duplicate Google rows already tracked by DEF-032; this phase did not choose or probe a row. Counts and names are visible in the inspected screenshots; sanitized row-level collection summaries are retained in the JSON evidence.

## Google failure classification

This comparison does **not** establish a same-configuration, three-runtime experiment. Only 18084 has Google configured; 18083 has no provider setup, 18086 has only synthetic Ollama, and 18085 is inaccessible with the retained credential. Those are test setup/access differences, not product outcomes.

- **`upstream_unreachable`:** remains supported as managed Cloud direct-egress incompatibility based on the actual RC4 UI probe and separate proxy-versus-direct diagnostics documented in CP17, CP86, CP93 and QA-OBS-017. This phase did not repeat that probe.
- **`probe_runtime_unavailable`:** remains **UNVERIFIED on RC4**. It was not reproduced by this inventory-only phase; the former QA-001 listener-specific report is historical evidence from a different baseline. No product defect is assigned.
- Google adapter behavior under reachable guarded direct egress and app-mediated inference remain **UNVERIFIED**. No credential was printed, tested, or changed.

## Evidence

- `../evidence/CP102-google-runtime-inventory.json` — sanitized collection counts/rows and browser outcomes; contains no credential value.
- `../evidence/CP102-provider-inventory-18083.png` — authenticated empty provider page, visually inspected.
- `../evidence/CP102-provider-inventory-18084.png` — authenticated provider page with two Google rows, visually inspected. It displays only the previously retained partial fingerprint suffix.
- `../evidence/CP102-provider-inventory-18086.png` — authenticated synthetic Ollama-only page, visually inspected.
- `../evidence/CP102-provider-inventory-18085-login-failure.png` — rejected retained credential message, visually inspected; no password appears.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain only their scoped QA-002 outcomes; no QA-001 PASS is inherited. DEF-005 remains PARTIAL; DEF-007 failure atomicity/populated erasure remains UNVERIFIED.
- **Historical defects failing or unverified:** DEF-016–036 remain open; DEF-023 is the latest reconfirmed product failure. No defect disposition changed in CP102.
- **New defects:** none. Runtime configuration differences and one rejected retained QA credential are not product defects.
- **Coverage completed:** read-only, real-browser provider/adapter/installation inventory on 18083, 18084, and 18086; login-blocker capture on 18085; visual screenshot inspection; sanitized comparison across all four runtime instances.
- **Coverage blocked:** same-config multi-runtime Google probe is blocked by the absence of Google configuration on 18083/18086 and unavailable Owner login on 18085. Google app transport/inference remains Cloud direct-egress blocked as recorded in QA-OBS-017.
- **Coverage remaining:** restore authorized access to the 18085 copy or use another explicitly authorized isolated runtime; provision matching adapter/connection configuration only within isolated QA instances; then compare the same probe path. Reproducing `probe_runtime_unavailable` requires a supported RC4 runtime configuration where probe-runtime acquisition can be observed. Continue all other incomplete QA without waiting on this provider test.
- **Recommended engineering handoffs:** no new provider handoff. Continue DEF-005 without weakening SSRF controls; prioritize existing high-impact workspace, webhook revocation, audit, and budget defects in the defect register.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
