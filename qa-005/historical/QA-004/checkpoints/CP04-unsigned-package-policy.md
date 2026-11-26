# CP04 — Unsigned marketplace candidate policy boundary

**Result:** PARTIAL. Tested the default UI flow and cancellation without activating an unsigned package.

- On exact RC6, marketplace UI/API described the eight package records as unverified/unsigned with `signature_verified=false` and `origin_trust_tier=unverified_external`.
- Normal Stage for review returned 202 and created `PENDING_PERMISSION_APPROVAL`. Dialog explicitly reported no verified offline release signature and warned of `unverified_external` execution. Permission deltas were inspectable. Approve controls remained disabled until the explicit consent checkbox; QA did not provide consent or activate.
- QA canceled and discarded the pending candidate via authenticated API with matching ETag; response was 204 and installation list returned to zero.
- No namespace spoof, tamper, signature verifier, upgrade, rollback, quarantine, install execution, WASM/sidecar isolation, or provider credential flow was exercised.

Evidence: `evidence/CP02-marketplace-catalog.png`, `evidence/CP04-unsigned-extension-stage.png`, `evidence/CP04-unsigned-catalog-and-stage.json`, `evidence/CP04-stage-discard.json`. No production or ephemeral signing key was created.
