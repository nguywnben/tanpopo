# CP61 — Google provider readiness and Gemini signing blocker

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Status:** provider credential/catalog reachability PASS through the configured HTTPS proxy; Tanpopo Gemini adapter activation and inference BLOCKED by the known release-signing state. No inference spend.
**Product source/runtime changes:** none.

## Provider credential and network check

The configured `QA_GOOGLE_AI_STUDIO_API_KEY` binding was present. A no-cost request to Google’s model catalog from the QA runner failed without a proxy (`ECONNREFUSED`) but succeeded through its configured HTTPS proxy with HTTP 200 and 50 returned models. The credential itself, query string, and model response payload were not committed or printed. This verifies the binding and runner-to-Google route only; it does not prove that Tanpopo’s process uses the same proxy or can reach Google.

## RC6 Marketplace and policy boundary

The real Owner Console showed zero installed extensions and an offline cached catalog. The built-in `@tanpopo/gemini` v0.1.0 candidate was explicitly labeled `Unverified`, `Unsigned`, and `sandboxed_wasm`. I opened the real permission review, which disclosed the `generativelanguage.googleapis.com` host, `api_key` secret slot, declared provider hooks and resource limits, and warned that the package would run as `unverified_external` without release signing keys. The two explicit review/unsigned-package acknowledgements remained unchecked. I did not approve or activate it, issue credentials to it, or configure a provider. The pending candidate was discarded through the reasoned uninstall flow (HTTP 204); afterward the installed inventory returned to zero.

This followed the current trust policy. I did not use an ephemeral signature to make the adapter appear production-signed, did not treat a test signature as authentic, and did not consume the real provider key in an inference request. Actual Google adapter testing is blocked until Tanpopo supplies authentic release signatures or Product Owner-controlled signing infrastructure makes the package available under the intended trust state. No new product defect is assigned from this blocked integration path.

## Evidence and coverage impact

- [Sanitized readiness result](../evidence/CP61-google-provider-readiness.json)
- [Cached Marketplace trust labels](../evidence/CP61-gemini-marketplace-unsigned.png)
- [Gemini permission review](../evidence/CP61-gemini-permission-review.png)
- [Pending candidate inventory](../evidence/CP61-gemini-pending-candidate.png)
- [Reasoned discard confirmation and result](../evidence/CP61-gemini-discard-confirm.png), [post-discard inventory](../evidence/CP61-gemini-candidate-discarded.png)

Google’s external catalog is reachable through the QA runner proxy, but Gemini connection validation, model/deployment/route setup, actual inference, streaming, function calling, multimodal behavior, quota/retry recovery, exact usage settlement, and Tanpopo-runtime egress remain UNVERIFIED/BLOCKED. Existing Ollama synthetic inference passes do not cover these Google paths. QA remains ongoing; `GATE-REL-07` remains **BLOCKED**.
