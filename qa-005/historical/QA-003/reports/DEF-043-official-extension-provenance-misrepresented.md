# DEF-043 — Built-in Official provenance is locally signed and displayed as unsigned

- **Severity:** Moderate
- **Status:** Confirmed RC5 provenance-contract discrepancy with misleading UI trust labels
- **Baseline:** frozen RC5 `3423d4f523ef0ac77469a1201b8b753d1d31c902`
- **Affected workflows:** Extension Marketplace, permission review, official adapter trust decisions, package installation and supply-chain review
- **Checkpoint:** [CP23](../checkpoints/CP23-extension-marketplace-provenance-and-lifecycle.md)

## Reproduction

1. On the isolated SQLite RC5 runtime, sign in as the synthetic bootstrap Operator/Org Owner and open Extensions → Marketplace.
2. The built-in Anthropic card is presented as **Official** with **Signature: None**. Select **Stage for review**. The real `POST /admin/v1/extensions/install` returns 202 and the installation remains `PENDING_PERMISSION_APPROVAL` with no active version.
3. In the permission review sheet, the UI displays `Origin trust: Official` and `Publisher signature: Unsigned`. The sheet shows candidate outbound host `api.anthropic.com`, secret slot `api_key`, added hooks, and the WASM sandbox tier. No provider connection or external request was made. Evidence: [Marketplace](../evidence/CP23-marketplace-list.png), [staged review](../evidence/CP23-anthropic-stage-review.png), [scrolled consent control](../evidence/CP23-anthropic-consent-visible.png).
4. The explicit consent checkbox initially leaves **Approve & activate** disabled. After scrolling to the bottom, checking consent enables it. `POST /admin/v1/extensions/{id}/approve-permissions` returns 200; API readback is `ENABLED_ACTIVE`, `origin_trust_tier=official`, `enterprise_signed=false`, version `0.1.0`. See [consent/activation result](../evidence/CP23-anthropic-approval-result.json) and [active catalog screenshot](../evidence/CP23-anthropic-approved-active.png).
5. Read-only version metadata for `@tanpopo/anthropic` shows `signature_verified=true`, a signing-key fingerprint is present, and `enterprise_signed=false`. The sanitized observation is [here](../evidence/CP23-anthropic-api-result.json). The UI's **Publisher signature: Unsigned** therefore does not describe the signature that established its official provenance.
6. Frozen-source inspection shows `serve.go` derives the built-in root public key from the active instance KEK and defines the signer with that same KEK/domain; `builtin.Catalog.ResolveStageRequest` signs the built-in package during staging. The installer then verifies against that runtime-derived root key. See [`serve.go`](../../cmd/tanpopo/serve.go#L310-L319), [`builtin/catalog.go`](../../internal/extension/builtin/catalog.go#L128-L149), and [`provenance.go`](../../internal/extension/installer/provenance.go#L163-L190). This is not evidence that a Tanpopo offline release-root signature was verified.

The package was never used for inference or connected to an upstream. After capture, the test quarantined it and uninstalled it from the isolated database. No provider traffic occurred.

## Expected vs actual

- **Documented contract:** `TP-BR-0201` defines `official` as cryptographically signed with Tanpopo's offline release root key. `TP-AC-F0301B` requires verified Official Reference Adapter signatures. Enterprise signing is an independent flag, not a substitute for publisher provenance.
- **Actual:** the staged built-in package is marked official after the running instance signs it with a key derived from its active Master KEK. Persistent metadata says a signature verified, but the UI labels publisher signature `Unsigned` because the independent enterprise-signature flag is false.

## Business and security impact

Operators and reviewers cannot tell whether “Unsigned” means no publisher signature, no enterprise signature, or both. The running instance can mint the signature that its own keyring accepts as root, so this flow does not establish the documented offline release-root identity across installations. No exploit or unauthorized activation was demonstrated; explicit permission consent and backend activation controls worked.

## Facts vs interpretation

**Facts:** the visible labels, persisted `signature_verified=true` / `enterprise_signed=false`, and source call path are recorded above. The user explicitly consented before activation; active state persisted and the artifact was later removed.

**Interpretation requiring product confirmation:** the runtime-derived domain key may be intended as local attestation for embedded built-ins. If so, the `offline release root` requirement and the UI's publisher-signature label need revision; if not, the release must supply and verify the offline publisher signature rather than minting it at install time.

## Acceptance criteria

1. Verify Official built-in artifacts against a Tanpopo release-root public key that is independent of the instance's Master KEK; do not generate the publisher's signature during installation.
2. Preserve the independent `enterprise_signed` indicator and display publisher/release signature and enterprise signature as separate facts.
3. Show the verified signer identity or fingerprint and verification status consistently in Marketplace, staging review, and Installed views.
4. Add cross-instance tests proving the same Official artifact verifies to the same release signer, while unsigned or spoofed `@tanpopo*` packages remain rejected.
5. Preserve the working pending-consent behavior: candidate inactive before approval, and activation impossible until the complete permission sheet is explicitly approved.

## Remaining uncertainty

The RC5 release binary/artifact held in a separate DevBox remains inaccessible and therefore **UNVERIFIED**. The source/runtime behavior above is independently reproduced on the frozen source baseline; it does not establish whether a separately packaged release artifact adds another trust layer. Other official adapters and browsers remain untested.
