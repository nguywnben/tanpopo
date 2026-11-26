# Release Signing Ceremony & Key Management Runbook — Tanpopo

> **Supply-Chain & Governance Verification Protocol (`GATE-REL-07`, `CT-05`, `DEC-004`)**
> - **Status:** `READY_FOR_PO_SIGNING` — offline CLI tooling and verification hooks are complete; **production root keys are intentionally absent from the repository and internal RC builds** while `GATE-REL-07` remains `BLOCKED`.

## 1. Security Model & Prohibitions

1. **No Embedded Private Keys:** Tanpopo source trees, CI workspaces, container images, and release binaries must never contain any Ed25519 release root private key (`TANPOPO_RELEASE_ROOT_PRIVKEY` does not exist in runtime).
2. **Never Derive Root Trust from Instance Secrets:** Built-in packages are never signed using an instance Master KEK or runtime-generated keys. In unsigned development or internal RC builds, `extensions/official/release-signatures.json` remains `{"signatures": []}` and packages surface truthfully as `origin_trust_tier: "unverified_external"` with `signature_verified: false`.
3. **Reserved Namespace Enforcement (`CT-05`):** Any package ID matching `^(?i)@tanpopo(-[a-z0-9_-]+)?/` requires a cryptographically verified offline root signature when staged externally (`403 reserved_namespace_spoofing` otherwise).

## 2. Offline Product Owner Signing Ceremony

Perform the key generation and signing steps exclusively on an offline/air-gapped Product Owner workstation or HSM-backed signing host:

### Step 1 — Generate Offline Ed25519 Release Root Keypair (One-Time per Key Generation)

```bash
openssl genpkey -algorithm ED25519 -out /secure/offline/tanpopo-release-root.key.pem
chmod 0600 /secure/offline/tanpopo-release-root.key.pem
openssl pkey -in /secure/offline/tanpopo-release-root.key.pem -pubout -out /secure/offline/tanpopo-release-root.pub.pem
```

Export the raw 64-hex Ed25519 public key for build-time `-ldflags` injection:

```bash
python3 - <<'PY'
import subprocess, re
der = subprocess.check_output(["openssl", "pkey", "-pubin", "-in", "/secure/offline/tanpopo-release-root.pub.pem", "-outform", "DER"])
print(der[-32:].hex())
PY
```

### Step 2 — Sign All Embedded Official Built-in Adapters

From a clean, verified release tag checkout, run:

```bash
tanpopo plugin release-sign \
  --private-key-file /secure/offline/tanpopo-release-root.key.pem \
  --builtin \
  --out extensions/official/release-signatures.json
```

To sign an individual standalone `.tpkg` release artifact instead:

```bash
tanpopo plugin release-sign \
  --private-key-file /secure/offline/tanpopo-release-root.key.pem \
  --package-id "@tanpopo/example" \
  --version "1.0.0" \
  --archive "./dist/@tanpopo-example-1.0.0.tpkg" \
  --out ./dist/release-signatures.json
```

### Step 3 — Verify Offline Before Publishing

Verify both signature parity and archive digests against the public root key:

```bash
tanpopo plugin release-verify \
  --signatures extensions/official/release-signatures.json \
  --root-pubkey-file /secure/offline/tanpopo-release-root.pub.pem \
  --builtin
```

### Step 4 — Build Release Binaries with Embedded Public Keys

Pass the public key (never the private key) via `-ldflags`:

```bash
export ROOT_PUB_HEX="<64-hex-ed25519-public-key>"
go build -trimpath -mod=readonly \
  -ldflags "-X github.com/nguywnben/tanpopo/internal/extension/builtin.ReleaseRootPublicKeysHex=${ROOT_PUB_HEX}" \
  ./cmd/tanpopo
```

For air-gapped or enterprise-operated deployments using rotated public keys or custom signature manifests, operators may also configure runtime verification keys without rebuilding via:
- `TANPOPO_RELEASE_ROOT_PUBKEYS` (comma-separated 64-hex Ed25519 public keys)
- `TANPOPO_TRUST_ROOT_PUB` or `<data_dir>/trust-root.pub`
- `TANPOPO_RELEASE_SIGNATURES_PATH` or `<data_dir>/release-signatures.json`

## 3. Pre-GA Sign-Off Checklist (`GATE-REL-07`)

- [ ] Product Owner generates offline Ed25519 root keypair outside repository/VM.
- [ ] `tanpopo plugin release-sign --builtin` populates `extensions/official/release-signatures.json`.
- [ ] `tanpopo plugin release-verify --builtin` exits `0` for all built-in packages.
- [ ] Release workflow sets `ReleaseRootPublicKeysHex` to the matching 64-hex public key.
- [ ] Fresh startup confirms official adapters surface `origin_trust_tier: "official"` and `signature_verified: true`.
