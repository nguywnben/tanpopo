# DEF-071 — Unsigned external extension can claim a reserved Tanpopo namespace

**Severity:** Major
**Status:** FAIL on frozen RC6
**Baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Discovered:** QA-004 CP88
**Affected workflows:** marketplace/local extension import, publisher identity, permission review and approval, supply-chain trust governance

## Summary

An external local archive with no signature and a manifest ID matching the reserved `@tanpopo(-suffix)/` namespace was accepted for staging. The API created a `PENDING_PERMISSION_APPROVAL` candidate and the Console displayed the reserved-looking name. The UI truthfully labeled the package unsigned and unverified and required explicit consent; no consent was given. This does not make it trusted or active, but violates the namespace denial contract and exposes operators to a spoofed reserved identity during approval.

## Reproduction

On isolated frozen RC6 runtime `127.0.0.1:18129`, sign in as Org Owner and open **Extensions → Stage package**. Put the following manifest in a local `.tpkg` tar-gzip archive under the configured archive root, calculate the archive's exact SHA-256, then submit the archive URI and digest through the actual Console form:

```yaml
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo-qa004-spoof/canary"
  name: "QA Reserved Namespace Spoof Probe"
  version: "0.1.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
permissions:
  networkEgress:
    allowedHosts: ["api.qa004.invalid"]
    allowPrivateIp: false
    requireTls: true
```

The archive contained only `manifest.yaml`: no executable payload, sidecar or signature. The supplied URI was `tanpopo-local://cp88-reserved-namespace.tpkg`. The actual `/admin/v1/extensions/install` response was HTTP 202, with candidate version `0.1.0`, `origin_trust_tier: unverified_external`, and `signature_verified: false`. Readback and the permission review showed a new pending candidate named `@tanpopo-qa004-spoof/canary`. The exact submitted archive SHA-256, request response, before/after installation lists and screen text are in [CP88 results](../evidence/CP88-reserved-namespace-results.json); the form and resulting permission review were visually inspected in [form](../evidence/CP88-reserved-namespace-form.png) and [accepted candidate](../evidence/CP88-reserved-namespace-stage-accepted.png).

## Expected versus actual

- **Expected:** reserved external IDs matching `^@tanpopo(-[a-z0-9]+)?/` receive HTTP 403 `reserved_namespace_spoofing`; no install row or permission review is created. OpenAPI documents this 403 for `POST /admin/v1/extensions/install`.
- **Actual:** exact-digest local import received 202 and created a pending installation. The UI exposed it under the reserved-looking namespace while truthfully labelling it Unverified/Unsigned. Consent gates remained unchecked and active; the package was never approved or executed.

## Business impact and severity

**Major.** The reserved namespace is a publisher identity boundary. An attacker or untrusted package author can present a Tanpopo-branded identity during review. Existing unverified-source warnings mitigate accidental trust, but an operator can still approve the package after seeing that misleading identity. This impacts offline imports, marketplace review, supply-chain governance and approval decisions. No authenticity or release-readiness inference should be made from this result.

## Root-cause hypothesis (not confirmed)

The local archive staging route may fail to propagate the manifest's `RequiresRootSignature` state into provenance verification, or the inspected verifier path may not match the frozen release artifact's runtime path. These are only hypotheses; confirm the exact artifact wiring and verifier inputs before assigning a cause.

## Acceptance criteria

1. Exact-release tests reject unsigned packages matching both `@tanpopo/` and `@tanpopo-<suffix>/` through each supported external source with HTTP 403 `reserved_namespace_spoofing`.
2. Rejection creates no installation, candidate or approval action. A provenance-rejected audit event may be recorded, but no staging-success event may be emitted.
3. A reserved namespace is accepted only with a valid signature verified against the configured Tanpopo release root. Publisher/enterprise signatures alone cannot claim it.
4. Ephemeral test-root success remains explicitly test-scoped and is never presented as authentic release provenance. Authentic production signatures remain a separate release prerequisite.
5. Regression covers the UI and API, local and registry sources, fresh and populated installations, organization trust policies, restart/readback and the corresponding audit trail.

## Cleanup and related evidence

I canceled the review without consenting, used the Console's **Uninstall package** action with an audit reason, verified the synthetic candidate disappeared while the pre-existing test package remained, and deleted the archive. Audit events #16 and #17 link staging and cancellation to the same installation; the Security → Audit Log UI verified the RFC 8785 chain intact across 17 events. See [CP88 cleanup](../evidence/CP88-spoof-cleanup.json), [post-cleanup Console](../evidence/CP88-spoof-cleaned.png), and [chain verification](../evidence/CP88-spoof-cleanup-chain.png).

## Coverage limits

One unsigned local archive using an `@tanpopo-` namespace variant was tested. Other reserved-name variants, valid root-signed packages, publisher/enterprise signature combinations, OCI registry source, malformed archives and post-approval execution are not verified. This finding does not resolve the known production signing blocker.
