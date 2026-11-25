# Release signing readiness — QA-004 RC6

**Status: BLOCKED for authentic production signing; verification implementation and package trust behavior remain under test.** RC6 intentionally ships an empty `extensions/official/release-signatures.json`. QA does not possess or request any production private key.

| Check | Status | Evidence / detail |
|---|---|---|
| RC6 source signature registry | PASS (presence only) | Exact baseline registry is empty (`signatures: []`, 23 bytes). |
| Production release signatures present | FAIL / known blocker | No package has a release signature in this frozen source bundle. No claim of production authenticity is made. |
| RC6 CI release dry-run | PASS (workflow execution only) | Run `45581179274` succeeded for exact baseline; this does not itself establish each package signature. |
| Artifact archive checksums | PASS (5/5) | `CP01-artifact-verification.json`; SHA256SUMS verified. |
| GitHub artifact attestation API | UNVERIFIED | `gh attestation` is unavailable in the installed CLI; REST lookup by artifact digest returned HTTP 404. Run success is not substituted for cryptographic verification. |
| Unsigned catalog provenance and trust policy | PARTIAL | Fresh offline authenticated catalog lists eight entries; no package installed or trust decision exercised yet. See CP02. |
| Namespace spoof denial, tamper rejection, ephemeral test-signature verification | UNVERIFIED | Not yet exercised in QA-004. |

Production release signing remains blocked pending Product Owner controlled signing infrastructure. Ephemeral keys may be used only for isolated verifier tests and will not be described as authentic Tanpopo signatures.
