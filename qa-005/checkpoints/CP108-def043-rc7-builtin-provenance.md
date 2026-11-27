# CP108 — DEF-043 exact-RC7 built-in extension provenance

**Disposition:** RC7 runtime provenance labels agree with API metadata in the inspected built-in package slice. DEF-043 moves from UNVERIFIED to **PARTIAL**, not closed. `GATE-REL-07` remains **BLOCKED**.

## Baseline and environment

- Source: exact frozen RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`.
- Runtime: locally compiled exact-source Linux/amd64 binary, SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated QA SQLite at `127.0.0.1:18145`; Chromium 156; synthetic Owner session.
- No production artifact, owner signing material, real provider, paid call, or product source change was used.

## Reproduction and observed result

1. Signed into the isolated runtime as QA Owner and opened Extensions → Marketplace.
2. Inspected the existing `@tanpopo/ollama` v0.1.0 catalog row. The real UI showed **Official**, **Signature verified (d04e71ce017b)** and **Installed**.
3. Opened Installed. The real UI showed `@tanpopo/ollama`, Active v0.1.0, Official, **Signature verified (d04e71ce017b)** and declarative execution tier.
4. The browser's real catalog and installation reads both returned HTTP 200. Sanitized readback agreed on `origin_trust_tier=official`, `signature_verified=true`, `enterprise_signed=false`, version `0.1.0`, and fingerprint prefix `d04e71ce017b`.
5. No staging, approval, activation, quarantine, uninstall, write, provider request or paid usage occurred. Both screenshots were opened and visually inspected; hashes and sanitized API fields are in [evidence](../evidence/CP108-def043-extension-provenance.json).

## Source evidence and interpretation

Exact-RC7 source loads configured release-root public keys and signature records, leaves the built-in signer callback nil in the server setup, uses an embedded release signature for built-in staging when no signer is provided, and evaluates catalog provenance by verifying the signature against the configured public roots. References: [`serve.go`](../../cmd/tanpopo/serve.go#L379), [`catalog.go`](../../internal/extension/builtin/catalog.go#L63), [`release_keys.go`](../../internal/extension/builtin/release_keys.go#L47) and [`release_keys.go`](../../internal/extension/builtin/release_keys.go#L266). The loader explicitly states it does not derive signing material from the instance KEK. This is a static source fact; this checkpoint did not stage a package or tamper with signatures.

QA-003's RC5 record reproduced Official provenance with the Publisher field saying Unsigned while metadata had `signature_verified=true`, `enterprise_signed=false` and source signed locally using KEK-derived material. In the current RC7 tested view, Signature verified and the independent enterprise flag is false, and the UI does not label the publisher as unsigned. QA-004's unsigned external-package observations remain preserved. Thus the old exact mismatch is not reproduced in this RC7 package/view slice.

This is not production-signing verification. The binary uses an isolated QA trust configuration; the authentic production trust root and owner-signed artifact were not used. The separately held owner manifest/signing branch remains outside this campaign's access, and canonical archive digest integration remains blocked by governance. No production private key was accessed.

## Expected versus actual

- Expected contract: Official provenance should represent a signature validated by the intended offline Tanpopo release root; publisher/release signature and enterprise-signature status should be distinguishable and displayed consistently.
- Actual in this scoped runtime: UI and API consistently report Official and signature verified, while `enterprise_signed=false`; source uses release-root verification rather than the former instance-KEK signing path.
- Unverified: production root identity, official release artifact and cross-instance signer consistency; altered/unsigned/spoofed artifacts; staging review and all lifecycle transitions; other official packages and browser engines.

## QA-005 disposition

**PARTIAL.** The historical UI/data mismatch is not reproduced in the current tested built-in package slice, and frozen-source inspection supports a changed root-verification path. Do not mark fixed or release-signature verified: production signer identity, archive digest reconciliation and cross-instance verification were deliberately not tested. No new defect is filed because no current RC7 failure was observed.

Evidence: [sanitized result](../evidence/CP108-def043-extension-provenance.json), [Marketplace screenshot](../evidence/screenshots/CP108-def043-marketplace-provenance.png), [Installed screenshot](../evidence/screenshots/CP108-def043-installed-provenance.png).
