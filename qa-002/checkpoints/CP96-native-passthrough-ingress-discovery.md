# CP96 — Native passthrough ingress discovery and credential cleanup

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169` (frozen RC4)
**Runtime:** Console/QA runtime `18084`; selected `QA002 Audit mwto47wa / stage`
**Browser:** real Chromium; UI screenshots inspected
**Product implementation:** unchanged

## Attempt

FT-02 is a major unverified area, so I attempted a native Ollama passthrough request against the existing synthetic Ollama deployment. The virtual key was created through the Console as PROVIDER plane with the narrow `/native/*` route grant. The request used a JSON payload containing the Ollama-specific `keep_alive`, `options`, and nested opaque marker `cp96-native-field-7f2c`.

The POST to `/native/ollama/v1/chat/completions` at the exposed Console origin returned HTTP 405, `text/plain`, `HTTP method not allowed for this console resource.` This is the Console/web UI fallback response. It does not establish that native ingress was reached, that the provider fixture was contacted, or that native payload fields were preserved. Other locally exposed candidates tested in read-only probing also returned the same method fallback. A valid ingress host/port/configuration could not be determined from the selected runtime’s empty 3-byte YAML file and exposed endpoint behavior. No product defect is assigned from this inconclusive result.

## Cleanup and evidence

Two temporary PROVIDER keys restricted to `/native/*` were issued during safe setup retries. Both were revoked through the real Access & Keys UI and then confirmed `REVOKED` by authenticated Admin API listing. The screenshot [CP96-native-key-revoked.png](../evidence/CP96-native-key-revoked.png) was visually inspected. One-time secret material was never printed or stored in QA evidence; the secure temporary file was removed. No request replay was attempted after revocation.

- [Sanitized request/response and cleanup record](../evidence/CP96-native-passthrough-result.json)
- [Persisted key state](../evidence/CP96-native-key-state.json)
- [Restricted key form before issuance](../evidence/CP96-native-key-issue-configured.png)

## Results

| Case | Result | Evidence and limits |
| --- | --- | --- |
| `/native/ollama/v1/chat/completions` positive route | BLOCKED / UNVERIFIED | Exposed origin returned Console method fallback 405; no native handler or provider dispatch established. |
| Native unknown-field preservation | UNVERIFIED | No positive native upstream exchange occurred. |
| Credential lifecycle | PASS (cleanup only) | Two one-time keys issued via UI with PROVIDER plane + `/native/*`; both revoked in UI and persisted as REVOKED. This is not an inference authorization pass. |
| Product defect | None assigned | Correct data-plane endpoint/configuration is not known; evidence does not separate disabled runtime configuration from an endpoint-selection issue. |

## Required environment to unblock

Provide or restore the QA runtime’s supported Data Plane native-ingress address and port, with Native passthrough enabled for the RC4 runtime and routing to the isolated synthetic provider. The endpoint must be reachable from this Cloud test environment. Keep normal SSRF and Virtual Key plane/route checks enabled. Then repeat the authorized Ollama fixture exchange, capture the fixture’s received JSON after secret redaction, and verify persistence/settlement. This blocker does not stop unrelated QA work.

## Consolidated defect/status summary

- **Historical DEF-001–015:** CP96 did not retest them. Preserve scoped QA-002 outcomes only: DEF-001–004, DEF-006, and DEF-008–015 have limited passing paths; DEF-005 remains partial; DEF-007 successful audit paths do not close failure atomicity/populated erasure. Full acceptance remains incomplete; no QA-001 PASS is inherited.
- **Historical open DEF-016–035:** remain open; CP96 changed no defect disposition.
- **New defects:** none. DEF-035 remains the newest confirmed defect from CP95.
- **Coverage completed:** one restricted-key UI creation/revocation lifecycle and authenticated persistent-state check; one unsuccessful native endpoint selection attempt with sanitized response evidence.
- **Coverage blocked:** FT-02 native exchange and fidelity due the unresolved QA native Data Plane address/enablement. This is not classified as a product defect.
- **Coverage remaining:** all previously open feature, role, security, browser/platform and integration cases; FT-02 positive native body/response/SSE fidelity; all DEF-001–015 independent acceptance; DEF-005 residual SSRF coverage; DEF-007 fault paths; open DEF-016–035 regression.
- **Recommended handoffs:** no new engineering handoff from CP96. Existing recommendations remain DEF-035 with DEF-023/024/029/030, DEF-034, DEF-033, DEF-005 without weakening SSRF, and DEF-007.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
