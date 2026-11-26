# CP41 — Extension trust, integrity, and unsigned staging

**Status:** PARTIAL; selected negative and consent-boundary behaviors PASS.
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged.

## Scope and trust boundary

Used the real Extensions Console and authenticated Org Owner session in the offline test-root runtime on `18124`. No public network fetch, real provider credential, or production signing key was used. The runtime's test-only keyring marks the staged Ollama package signature verified; that injected test trust root is not Tanpopo release signing infrastructure and does not establish authentic RC6 release provenance.

The current cached catalog listed eight embedded packages. Seven (`@tanpopo/anthropic`, `@tanpopo/aws-bedrock`, `@tanpopo/azure-openai`, `@tanpopo/cohere-rerank`, `@tanpopo/gemini`, `@tanpopo/openai`, `@tanpopo/openai-compatible`) were `signature_verified=false` and `unverified_external`. The bundled Ollama entry was verified only under the test root. The screenshot visually shows the distinct catalog signature/trust labels and air-gapped mode.

## Executed workflows and results

1. **Archive digest mismatch:** in the authenticated browser session, posted the exact Anthropic built-in source URI with a one-character-mutated SHA-256. The API returned HTTP 400 `hash_mismatch`, with the expected digest distinct from the actual embedded archive digest. No candidate was added; installation inventory remained unchanged.
2. **Built-in URI namespace/name spoof:** requested `tanpopo-builtin://@tanpopo/ollama.tpkg` with Ollama's catalog digest. The resolver returned HTTP 404 `resource_not_found`; the package-ID form was not accepted as a built-in archive slug. Installation inventory remained unchanged.
3. **Unsigned package staging:** used the Marketplace **Stage for review** action for Anthropic with the catalog's correct source and digest. The actual permission-review screen showed `Origin trust: Unverified`, `Publisher signature: Unsigned`, `Enterprise signature: None`, and an explicit warning that the package runs as `unverified_external` unless release signing keys are configured. It listed the added host, secret slot, hooks, execution tier, network/body permissions, and resource limits. Both approval choices remained disabled until the required review and unverified-package acknowledgement controls are selected. No consent was granted and no code was activated.
4. **Safe discard and audit:** used the real **Uninstall package** flow with an audit reason to discard the pending candidate. The candidate disappeared; the pre-existing test-signed Ollama installation remained active. Audit records `extension.staged_pending_approval` (#42) and `extension.staged_install_cancelled` (#43) target the same installation. Security → Audit Log verified the RFC 8785 chain intact across 43 events.

## Evidence

- `evidence/CP41-marketplace-inventory.png` and `CP41-marketplace-inventory.json` — visually inspected catalog and sanitized API trust metadata.
- `evidence/CP41-marketplace-catalog.png` — Marketplace trust labels and offline banner.
- `evidence/CP41-extension-provenance-negative-results.json` — digest-mismatch and URI rejection results, unchanged installation inventory, and catalog provenance.
- `evidence/CP41-unsigned-candidate-staged.png` and `CP41-unsigned-candidate-stage.json` — actual unverified-package permission-review dialog and pending state; visually inspected.
- `evidence/CP41-unsigned-candidate-pending.png`, `CP41-unsigned-candidate-actions.png`, `CP41-unsigned-candidate-uninstall-confirm.png`, `CP41-unsigned-candidate-cleaned.png`, and `CP41-unsigned-candidate-cleanup.json` — pending candidate, explicit discard confirmation, and post-cleanup state; visually inspected.
- `evidence/CP41-extension-audit.json`, `CP41-extension-audit-staged.png`, `CP41-extension-audit-cancelled.png`, and `CP41-extension-audit-chain.png` — API/UI audit linkage and intact chain; screenshots visually inspected.

## Limits

This does not test a custom tampered `.tpkg` whose modified payload has a freshly recomputed archive digest, invalid Ed25519 signature, namespace-impersonating signed archive, malicious manifest/archive structure, upgrade/re-consent, rollback, quarantine/stream termination, lockfile/GitOps reconciliation, or WASM side-channel/resource exhaustion. No authentic built-in release signature exists in the RC6 release-signatures artifact. Staging one unsigned candidate confirms the consent gate only; it does not make the release signing blocker pass. No defect was assigned from this checkpoint. QA-004 remains ongoing and **GATE-REL-07 remains BLOCKED**.
