# Multi-Source Extension Lifecycle & Marketplace Governance Specification — Tanpopo

> **Supply-Chain Architecture & Governance Specification Document (ASTRA Phase A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Document ID:** `SPEC-EXT-04` | **Version:** `0.1.0-step04-draft` | **Last Updated:** `2026-11-11`
> - **Authoring Role:** Platform Architect & Security-Minded SDK Designer
> - **Standardization Status Statement:** The `tanpopo.lock` lockfile schema, static Marketplace index layout, and cryptographic signing flows below are **illustrative candidate designs (`ILLUSTRATIVE — PRE-ADR`)** and are not claimed as frozen standards prior to supply-chain ADR approval.
> - **Requirements Traceability:** `DEC-002`, `DEC-004`, `SM-01`, `SM-08`, `WF-04`, `WF-11`, `WF-12`, `OPS-08`, `OPS-09`, `TP-FR-0304`..`0307`, `TP-FR-0703`, `TP-FR-1002`, `TP-FR-1004`, `TP-NFR-0403`.

---

## 1. Foundational Supply-Chain Principle: Distinguishing `Origin` ≠ `Publisher Trust` ≠ `Safety`

A common and dangerous flaw in plugin ecosystems is equating *"cryptographically valid signature"* with *"unconditionally safe execution"*. Tanpopo strictly separates three orthogonal dimensions:

| Evaluation Dimension | Question Answered | Technical Evidence | Practical Limitation (**`Signature != Safety`**) |
|---|---|---|---|
| **1. `Package Origin`** *(Transport Source)* | *"Over which distribution channel was this artifact fetched?"* | Official Marketplace index, Git repository URL (pinned to commit SHA), internal OCI registry, or local `.tpkg`/`.zip` archive. | Transport security (HTTPS/OCI) prevents in-transit tampering but proves nothing about author identity or code behavior. |
| **2. `Publisher Trust`** *(Publisher Provenance)* | *"Who packaged this release, and what vetting tier did it pass?"* | Cryptographic signature (Ed25519 / Sigstore OIDC) and trust badge `official`, `verified`, or `unverified-external` (`TP-BR-0201`). | A signature proves **integrity and non-repudiation** since signing time only. If a community publisher's signing key is compromised or the code contains a logic bug, the signature remains 100% valid! |
| **3. `Runtime Safety`** *(Behavioral Containment)* | *"Which resources can this package touch if it malfunctions or is compromised?"* | Explicit `Approved Permission Grant`, default-deny Sandbox isolation, `Blind Credential Broker`, and hard CPU/RAM quotas. | **Mandated for EVERY package (including `official` and `verified`)** — Sandbox containment is never disabled merely because a package carries a valid signature. |

---

## 2. Four Canonical Installation Sources & `Hermetic Packaging` Rules

### 2.1. Four Installation Channels Decoupled from Core Release Cycles

| Installation Method | Illustrative CLI / Declarative Syntax | Immutable Pinning & Defense Mechanisms | Target Milestone |
|---|---|---|---|
| **1. Curated Marketplace** | `tanpopo plugin install @tanpopo/openai@^1.2.0` | Verifies against Tanpopo's signed static Marketplace index, checks package signature, `SHA-256` digest, and the active `Revocation Feed`. | `v0.2` (core official bundle pre-packaged in `v0.1`) |
| **2. Git URL / Release / Commit** | `tanpopo plugin install git+https://github.com/org/ext.git#v0.4.1` | When given a branch or tag ref, the installer resolves and pins the **full `commit SHA`** + artifact `SHA-256` inside `tanpopo.lock`; **strictly executes zero install hooks** (`postinstall`, `Makefile`) on the Gateway host. | `v0.1` (`Must`) |
| **3. Signed Package / Archive** | `tanpopo plugin install ./acme-adapter-0.2.0.tpkg` | Enforces maximum uncompressed archive size (default `<= 64 MB`), blocks path-traversal entries (`../` *Zip-Slip*), rejects external `symlinks`, and verifies embedded digests/signatures. | `v0.1` (`Must`) |
| **4. OCI Artifact / Private Registry** | `tanpopo plugin install oci://registry.corp.internal/ai-ext/acme:0.2.0` | Pulls standard OCI Artifacts; hard-pins by immutable **`digest` (`@sha256:...`)** rather than mutable tags (`:latest`), supporting private registry auth and offline `Air-gapped Bundle` export. | `v0.2` (`Should`) / `v1.0` (`Air-gapped`) |

### 2.2. `Zero Transitive Runtime Dependency Rule`
- **Build-Time Bundling Only (`Build-Time Bundling Only`):** Every library dependency of an `Extension Package` must be pre-bundled by its author into the `.wasm` module (Tier 2) or immutable container image (Tier 3) at build time. A Tanpopo Gateway host **never** invokes `npm install`, `pip install`, or dynamic Internet dependency fetches during extension installation.
- **Zero Cross-Plugin Dependencies (`Flat Dependency Graph`):** Across `v0.1`–`v1.0`, each `Extension` interacts exclusively with Tanpopo Core's `Plugin Contract` and never invokes internal functions of another `Extension`, eliminating the *Diamond Dependency Problem* entirely.

---

## 3. Nine-Stage Operational Lifecycle from Verification to Quarantine (`SM-01` Deep Dive)

```mermaid
sequenceDiagram
    actor Op as Operator
    actor Rev as Reviewer / Org Owner
    participant Inst as Installer & Verifier
    participant Stage as Staging & Consent Gate
    participant Lock as tanpopo.lock & Audit
    participant RT as Isolated Runtime Pool

    Op->>Inst: 1. Request package install / version upgrade
    Inst->>Inst: 2. Verification: Enforce size cap, Zip-Slip guard, SHA-256, signature & Contract Version
    Inst->>Stage: 3. Staging: Parse static Manifest ONLY (ZERO guest code execution at this step!)
    Stage->>Stage: 4. Permission Diff: Compare requested permissions against active grant (on Update)
    alt Fresh install OR Update requests wider permissions (Permission Escalation)
        Stage-->>Rev: Present Permission Diff & Trust Tier Badge (Hold in PENDING_APPROVAL / UPDATING_PENDING_RECONSENT)
        Rev->>Stage: Explicitly approve permission grant
    end
    Stage->>RT: 5. Self-Test: Load into scratch Sandbox & run isolated initialization check (Dry-Run Health)
    RT-->>Lock: 6. Activation: Commit artifact SHA-256 + approved grant to tanpopo.lock & emit AuditEvent
    Lock->>RT: Cut new traffic over to new version; gracefully drain in-flight streams on prior version (DRAINING)
    opt Operational regression detected -> Rollback
        Op->>RT: 7. `tanpopo plugin rollback` -> Atomically revert to previous pinned SHA-256 in tanpopo.lock
    end
    opt Malicious behavior / egress violation / critical Advisory
        Op->>RT: 8. `tanpopo plugin quarantine` -> Immediately sever execution & network, preserving forensic evidence
    end
```

### Illustrative Immutable `tanpopo.lock` Schema (`ILLUSTRATIVE — PRE-ADR`)

```yaml
# ILLUSTRATIVE SUPPLY-CHAIN LOCKFILE CANDIDATE (Pending ADR Approval)
lockfileVersion: 1
generatedAt: "2026-11-11T15:00:00Z"

extensions:
  "@acme-cloud/regional-llm-adapter":
    version: "0.2.0"
    origin: "oci://registry.corp.internal/ai-ext/acme@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
    artifactSha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    contractVersion: "0.1"
    trustTier: "unverified-external"
    orgInternalSignatureVerified: true
    approvedPermissions:
      approvedBy: "principal:usr_sec_lead_01"
      approvedAt: "2026-11-11T14:58:12Z"
      allowedHosts:
        - "inference.eu-central-1.acme-ai.example"
        - "inference.ap-northeast-1.acme-ai.example"
      secretBindingModes:
        primary_api_key: "host_blind_inject"
        hmac_signing_secret: "host_crypto_sign"
    previousPinnedRollback:
      version: "0.1.9"
      artifactSha256: "4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a"
```

### `Uninstall` vs. `Quarantine` Semantics
- **`Uninstall` (Referentially Safe Removal):** Permitted only when `0` `Provider Connection` records and `0` `Policy` rules reference the extension; purges cached runtime artifacts while preserving historical `AuditEvent` entries intact.
- **`Quarantine` (Emergency Containment — `TP-BR-0404`):** Executes immediately regardless of active connection references; dependent traffic fails safely with `503 extension_quarantined`, while the artifact bundle is sealed in place for digital forensics and blast-radius inspection (`TP-FR-0705`).

---

## 4. Marketplace Governance, Software Provenance (`SBOM / Provenance`) & `Revocation`

1. **Publisher Identity Verification (`Publisher Verification`):**
   - The `@tanpopo/*` scope is exclusively reserved and signed by the Tanpopo project's offline root signing keys.
   - Publishers applying for the `verified` tier must prove organizational domain ownership (DNS TXT) or source repository provenance via transparent OIDC signing (aligned with Sigstore/Cosign) to prevent *typosquatting* attacks.
2. **Moderation Pipeline (`Moderation Pipeline`):**
   - *Automated Gate (CI Gate):* Validates `Manifest` schema, scans license compatibility, verifies attached **SBOM** (`CycloneDX` or `SPDX`), runs the full cassette-based `tanpopo plugin test` contract suite (`CT-01`..`CT-18`), and automatically rejects wildcard `*` network egress requests.
   - *Human Compliance Review (`Marketplace Reviewer`):* Audits adherence to upstream provider Terms of Service — unconditionally rejecting packages that scrape personal consumer subscriptions or impersonate vendor brands (`TP-BR-0302`, `TP-AC-SEC-06`).
3. **Licensing, SBOM & Build Provenance (`License, SBOM & Provenance`):**
   - Every Official Marketplace listing must declare a valid SPDX license identifier (accepting compatible open-source or explicit redistribution licenses and flagging restrictive copyleft dependencies), attach `sbom.cdx.json`, and include `Build Provenance` attestations linking back to the exact source commit.
4. **Security Advisories & Signed Revocation Feed (`Security Advisories & Revocation Feed`):**
   - The project publishes a cryptographically signed static advisory feed (`revocations.json`) with two severity actions:
     - `CRITICAL_MALICIOUS` (confirmed malware / credential exfiltration): Self-hosted instances with advisory sync enabled automatically transition matching artifact digests to `QUARANTINED` (or fire a critical page when configured in alert-only mode).
     - `VULNERABLE_UPGRADE_ADVISED` (patched security vulnerability): Surfaces a prominent warning banner in the Web Console and `tanpopo doctor` CLI output.
   - For `Air-gapped` deployments, an `Operator` can import `revocations.json` bundles offline via CLI.
5. **Vulnerability & Abuse Reporting (`Reporting & Takedown`):**
   - Publishes a clear `Coordinated Vulnerability Disclosure` policy and a trademark / ToS-violation takedown workflow committed to delisting confirmed malicious or infringing packages from the public index immediately upon verification.
