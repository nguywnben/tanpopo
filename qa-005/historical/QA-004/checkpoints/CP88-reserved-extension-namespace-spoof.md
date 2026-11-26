# CP88 — External package accepted in reserved Tanpopo namespace

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (release artifact runtime, isolated data directory).
**Outcome:** FAIL — DEF-071. An external unsigned package using the reserved `@tanpopo(-suffix)/` namespace was staged as a pending candidate instead of rejected.
**Gate:** GATE-REL-07 remains BLOCKED; authentic release signing remains BLOCKED. No product files changed.

## Reproduction

An Org Owner used the actual Extensions Console Stage package dialog against offline test runtime `127.0.0.1:18129`. I created a synthetic 402-byte declarative `.tpkg` with a single `manifest.yaml`, package ID `@tanpopo-qa004-spoof/canary`, no executable payload, no signature, and one reserved `.invalid` egress hostname. I computed the exact SHA-256 of the archive and supplied the local archive URI `tanpopo-local://cp88-reserved-namespace.tpkg` and that digest through the UI.

The POST to `/admin/v1/extensions/install` returned HTTP 202 and created installation `ins_01m8aksq1ne0090tv6hv4s1kt4` in `PENDING_PERMISSION_APPROVAL`. The visible permission review displayed the reserved package name, Origin trust `Unverified`, Publisher signature `Unsigned`, and the warning about unverified external execution. It required both explicit consent checkboxes. I did not consent or activate it, and no package code ran. This is a namespace enforcement failure despite the separate unverified trust disclosure and consent gate.

The contract describes reserved namespace enforcement and a 403 `reserved_namespace_spoofing` response. Detailed request/result evidence: [sanitized result](../evidence/CP88-reserved-namespace-results.json). Screenshots of the supplied stage form and resulting permission review were visually inspected: [form](../evidence/CP88-reserved-namespace-form.png), [accepted candidate/review](../evidence/CP88-reserved-namespace-stage-accepted.png).

## Cleanup and persistence check

I canceled the review, used the real Installed row action **Uninstall package**, supplied an audit reason, and confirmed. Readback returned only the pre-existing `@qa004/lifecycle-canary` installation; the synthetic candidate was gone. The Console visibly confirmed uninstall. Audit events #16 and #17 link `extension.staged_pending_approval` and `extension.staged_install_cancelled` to the same installation; the Audit Log showed the cancellation and its Verify hash chain action confirmed the RFC 8785 JCS chain intact across 17 events. The local package archive was deleted after cleanup.

Evidence: [cleanup readback and audit events](../evidence/CP88-spoof-cleanup.json), [cleanup confirmation screenshot](../evidence/CP88-spoof-cleanup-confirm.png), visually inspected [post-cleanup Console](../evidence/CP88-spoof-cleaned.png), [Audit Log](../evidence/CP88-spoof-cleanup-audit.png), and [verified chain](../evidence/CP88-spoof-cleanup-chain.png).

## Expected versus actual

- **Expected:** an external unsigned package whose manifest matches `^@tanpopo(-[a-z0-9]+)?/` is rejected with HTTP 403 `reserved_namespace_spoofing`; no candidate is created or offered for approval.
- **Actual:** the exact-digest package was staged with HTTP 202 and appeared in the Console under the reserved-looking name as unsigned/unverified. Human consent was still required and was not given.

## Severity and impact

**Major.** This bypasses the namespace reservation used to distinguish Tanpopo-controlled packages from external publishers. The existing warning and consent mitigate activation risk but do not make the reserved identity claim safe: an operator can be presented with a Tanpopo-branded namespace entry, review it as an external candidate, and choose to approve it. Marketplace inventory, offline package import, extension trust review, operator consent, and supply-chain governance are affected. This finding does not imply that the package became trusted, signature-verified, or active.

## Root-cause hypothesis (not confirmed)

The local archive staging path may not be carrying the manifest’s reserved-namespace requirement into the provenance check, or the release artifact may differ from the inspected verifier path. These are hypotheses only. Confirm the exact source-to-release wiring and verifier input before choosing a fix.

## Acceptance criteria

1. Exact-RC6-class tests submit an unsigned package with `@tanpopo/` and `@tanpopo-qa004/` IDs via every supported external source path and receive 403 `reserved_namespace_spoofing`.
2. Rejection leaves no installation/candidate or approval action; audit records a provenance rejection without recording successful staging.
3. Valid root-signed reserved packages pass only when the signature verifies against the configured Tanpopo release root. Test-only roots remain explicitly test-scoped and never count as authentic release signatures.
4. Publisher or enterprise signatures alone cannot claim a namespace reserved for Tanpopo root signatures. Unreserved third-party namespaces continue to use the normal provenance and consent policy.
5. Regression covers UI and API paths, fresh and populated catalogs, restart/readback, and organization trust policies.

## Coverage boundary

This reproduces one unsigned local archive using the `@tanpopo-` reserved form. It does not test every alias/form, genuine root-signed packages, publisher/enterprise signatures, OCI source, malicious payload structure, or whether approved package execution is isolated. Authentic production signatures remain unavailable and GATE-REL-07 remains BLOCKED.
