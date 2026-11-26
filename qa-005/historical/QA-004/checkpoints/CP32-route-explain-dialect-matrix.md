# CP32 — Route Explain ingress-dialect comparison

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for the Console's `Unified normalized (/v1/*)` route simulation; explicit adapter-native dialect mismatch behavior is also observed. No new defect ID.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Real Console interaction

In Chromium as Team Admin at `default/dev`, I opened Routes & Policies → Route Explain. The target was `qa004-loopback`, data residency `On-prem`, with no optional capabilities. The selected deployment was `dpl_01m8969kjme00br1mjja9gam7q`.

With the UI's **Unified normalized (/v1/*)** selection, the live simulator returned HTTP 200 / dry-run true, one eligible deployment, and zero rejections. The selected deployment is the same deployment used as the successful secondary in CP31's real cookie-free `/v1/chat/completions` request (trace `trc_01m89jr4cbe008s9v9jpreg3x8`, HTTP 200). The [normalized screenshot](../evidence/CP32-route-explain-normalized.png) and CP31 [live Traces screenshot](../evidence/CP31-failover-trace-list.png) were visually inspected.

I then changed only the Console's **Ingress dialect** selector to `openai_compatible`. The simulator still returned HTTP 200 / dry-run true, but zero candidates and one `DIALECT_MISMATCH`: the deployment's native dialect is empty and the backing adapter's native dialect is Ollama. The [explicit-dialect screenshot](../evidence/CP32-route-explain-openai-compatible.png) was visually inspected. The direct API variant reproduced this result; omitted/`normalized` selected the candidate, `ollama` selected the Ollama candidate, and `openai_compatible` rejected it.

## Classification

This does **not** confirm a product defect. The Console has a separate `Unified normalized (/v1/*)` option, which matched the live `/v1` request. The explicit `openai_compatible` value is evaluated as a provider-native dialect and the Ollama deployment is not a native OpenAI-compatible deployment. This behavior is consistent with the adapter-native compatibility guard observed in the frozen RC6 resolver. It does not establish end-to-end coverage for any `/native/<dialect>` route; those paths remain UNVERIFIED. No new finding is entered.

The frozen routing design document describes normalized `openai_compatible` wire traffic separately from `native:<dialect>` and describes Route Explain as using the same resolution path; its governance status is `PROPOSED`, so this QA result records observed RC6 Console behavior without treating that draft as an approved contract.

## Evidence and status

- Sanitized browser/API comparison and same-target live inference reference: [CP32 result](../evidence/CP32-route-explain-dialect-results.json).
- **PASS (scoped):** normalized `/v1` Route Explain selects the live-routable deployment; native Ollama dialect matching behaves consistently in the tested options.
- **UNVERIFIED:** native Anthropic/Gemini/OpenAI-compatible endpoint paths, other adapters, dialect-specific failover and strict route-explain parity across every request feature.
- No new defect ID. The five confirmed new failures remain DEF-051 through DEF-055. QA-004 continues.
