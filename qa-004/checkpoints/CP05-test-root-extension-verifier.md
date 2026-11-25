# CP05 — Isolated verifier positive path and marketplace activation

**Outcome:** PARTIAL. A valid test-only signature was accepted and an explicitly reviewed package was activated in an isolated RC6 process. Production signature readiness remains BLOCKED.

## Environment and boundary

- Executed the exact RC6 artifact at `/tmp/qa004-run-45581179274/extracted/tanpopo` in a fresh runtime on `127.0.0.1:18124` with a separate data directory.
- Injected a temporary Ed25519 test public root and matching signature registry for the bundled Ollama extension only. The private key was generated in memory and not persisted; the test root/signature are not authentic Tanpopo release materials.
- Marketplace fetches remained offline. No external provider credentials or production signing materials were used.

## User journey

1. Created a fresh synthetic Owner and organization through real Chromium setup.
2. Opened Marketplace and inspected the bundled catalog; with the test root, Ollama appeared signature-verified while other entries remained unsigned.
3. Staged Ollama through the UI. Permission review showed the package's outbound/private-host permissions, required an explicit consent checkbox, and kept activation unavailable until consent.
4. Approved the reviewed test-signed candidate and verified API activation state `ENABLED_ACTIVE`, `signature_verified=true`, plus the test-root fingerprint. Reopened review and checked state in UI.

## Evidence

Screenshots were visually inspected: `evidence/CP05-test-root-setup-initial.png`, `evidence/CP05-test-root-step2.png`, `evidence/CP05-test-root-review.png`, `evidence/CP05-test-root-overview.png`, `evidence/CP05-test-root-marketplace.png`, `evidence/CP05-test-root-ollama-stage-review.png`, `evidence/CP05-test-root-ollama-consent.png`, `evidence/CP05-test-root-ollama-activated.png`, and `evidence/CP05-test-root-ollama-review-reopened.png`. Sanitized catalog, activation and cleanup API evidence is stored beside them.

## Limits

This proves one positive Ed25519 verification and consent/activation path in a test-only trust domain. Namespace spoofing, tampered archive/manifest, wrong key, bad signature, upgrade/rollback/quarantine/uninstall, runtime isolation, and genuine production release signature are UNVERIFIED. It is not evidence that any official RC6 package is release-authentic. Production signing remains BLOCKED.
