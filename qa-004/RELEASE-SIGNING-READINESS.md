# Release signing readiness — QA-004 RC6

**Status: BLOCKED for authentic production signing; verification implementation and package trust behavior remain under test.** RC6 intentionally ships an empty `extensions/official/release-signatures.json`. QA does not possess or request any production private key.

| Check | Status | Evidence / detail |
|---|---|---|
| RC6 source signature registry | PASS (presence only) | Exact baseline registry is empty (`signatures: []`, 23 bytes). |
| Production release signatures present | FAIL / known blocker | No package has a release signature in this frozen source bundle. No claim of production authenticity is made. |
| RC6 CI release dry-run | PASS (workflow execution only) | Run `45581179274` succeeded for exact baseline; this does not itself establish each package signature. |
| Artifact archive checksums | PASS (5/5) | `CP01-artifact-verification.json`; SHA256SUMS verified. |
| GitHub artifact attestation API | UNVERIFIED | `gh attestation` is unavailable in the installed CLI; REST lookup by artifact digest returned HTTP 404. Run success is not substituted for cryptographic verification. |
| Unsigned catalog provenance and trust policy | PARTIAL | Fresh offline authenticated catalog lists eight unsigned entries. An ephemeral QA-only Ed25519 root verified the exact bundled Ollama package in a separate isolated runtime; the UI showed `official`/signature verified, displayed permission changes, required explicit consent and then activated the package. The private test key was never written or committed; test signature does not establish authentic Tanpopo provenance. See CP05. |
| Reserved-namespace spoof denial | FAIL | CP88 reproduced DEF-071: an unsigned `@tanpopo-` package was accepted for staging. It was not approved or activated. Other variants and a valid root-signed namespace case remain unverified. |
| Archive digest-pin tamper rejection | PASS (scoped) | CP41 rejected a mutated expected digest; CP118 changed one archive byte while retaining the original pin and received HTTP 400 `hash_mismatch` before a candidate was created. CP118 also found DEF-073: the Console exposes a low-level digest error message. Recomputed-digest content changes remain open. |
| Ed25519 signature mutation / bad-signature rejection | UNVERIFIED | No end-to-end RC6 request with a mutated Ed25519 signature or a modified archive/manifest whose expected digest is recomputed has been exercised. Test-root signing is not production release signing. |

Production release signing remains blocked pending Product Owner controlled signing infrastructure. QA-only ephemeral key evidence is strictly isolated verifier evidence and is not an authentic Tanpopo signature. No production signing key was accessed or created.


CP41 confirmed the current cached catalog shows seven unsigned/unverified built-ins and one test-root-verified Ollama package; the permission review correctly kept the unsigned Anthropic candidate pending and disabled approval until explicit review/acknowledgement. Hash-pin mismatch and package-ID-style built-in URI attempts were rejected without changing installation state. This remains scoped test-key evidence, not production release-signing readiness. See [CP41](checkpoints/CP41-extension-trust-integrity-and-unsigned-staging.md).
