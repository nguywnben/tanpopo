# CP100 — Provider credentials and adapter availability

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Coverage result:** configured provider-specific inference on runtime 18128 **BLOCKED** by absent credentials and adapters; no product defect inferred.

## Inspection

Signed in as the QA Org Owner on healthy runtime 18128, `QA004 Test Root Org / default / dev`, and inspected Providers → Connections, Adapters, and Vault Secrets. The real Console listed exactly two active connections, both QA-only loopback Ollama endpoints (127.0.0.1:18125 and :18126), and neither had a vault fingerprint bound. The adapter catalog contained one active Official-origin `@tanpopo/ollama` declarative `openai_chat` adapter. The Vault Secrets screen reported zero sealed credentials.

No Google/Gemini, Anthropic, or other external-provider credential/connection is configured in this runtime; provider-specific live inference cannot be executed here without changing the environment. This is a runtime coverage blocker, not evidence of a Tanpopo inference defect. The Cloud Console also visibly reports Air-gapped offline mode. Earlier CP61 separately confirmed that the QA runner HTTPS proxy could reach Google AI Studio's model catalog; that result is not a provider connection, credential, adapter activation, or inference in runtime 18128 and remains distinct. CP61's authentic release-signing blocker for Gemini also remains in force.

No credential value or secret metadata was exposed in the evidence. No provider was configured and no external request or spend was made.

## Evidence

- Sanitized [runtime inventory](../evidence/CP100-provider-inventory.json)
- Visually inspected real Console screenshots: [connections](../evidence/CP100-provider-inventory-ui.png), [adapter catalog](../evidence/CP100-provider-adapters-ui.png), and [empty vault](../evidence/CP100-provider-vault-ui.png)
- Separate earlier runner catalog and signing evidence: [CP61](CP61-google-adapter-readiness-and-signing-blocker.md)

## Coverage status

- Runtime 18128 connections and credentials inventory: **PASS (scoped)**.
- Google/Gemini or other external-provider dispatch from this runtime: **BLOCKED** (no configured credential/connection or adapter).
- Official Gemini adapter activation: **BLOCKED** by the authentic release-signing prerequisite; test signing is not production trust.
- Other runtimes, adapters, credentials, Google proxy behavior from Tanpopo, and external-provider inference remain **UNVERIFIED**.

QA continues in independent areas. This blocker does not stop synthetic/runtime-independent testing. **GATE-REL-07 remains BLOCKED.**
