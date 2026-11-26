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

Pass the public key (never the private key) via `-ldflags` (or `TANPOPO_RELEASE_ROOT_PUBKEYS` when using GoReleaser, which `.goreleaser.yaml` injects automatically while leaving snapshot/RC builds empty):

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

### Step 5 — Pre-Tag Publish Gate & Human PO Sign-Off (`GATE-REL-07`)

Before creating any Git release tag, sign `gate-rel-07-approval.json` offline and run full `--mode publish` validation (both refuse to run inside CI):

```bash
go run ./scripts/release/pre_publish_check.go \
  --mode sign-approval \
  --target-version v1.0.0 \
  --commit-sha "$COMMIT_SHA" \
  --approved-by "nguywnben" \
  --decision GO_WITH_CONDITIONS \
  --condition "COND-PO-01: disclosed runtime ceilings accepted" \
  --private-key-file /secure/offline/tanpopo-po-approval.key.pem \
  --out /secure/offline/gate-rel-07-approval.json

go run ./scripts/release/pre_publish_check.go \
  --mode publish \
  --target-version v1.0.0 \
  --commit-sha "$COMMIT_SHA" \
  --approval-file /secure/offline/gate-rel-07-approval.json \
  --public-key-hex "$PO_PUB_HEX" \
  --release-signatures extensions/official/release-signatures.json \
  --release-root-pubkey-hex "$ROOT_PUB_HEX" \
  --write-manifest dist/GOVERNANCE_SHA256SUMS
```

---

## 3. Windows PowerShell Offline Signing Ceremony (Pre-Tag)

Execute these commands exclusively in an offline/air-gapped **Windows PowerShell (`pwsh` or Windows PowerShell 5.1)** session with Go `>= 1.26.11` and OpenSSL (bundled with Git for Windows at `C:\Program Files\Git\usr\bin\openssl.exe` or installed via `winget install ShiningLight.OpenSSL.Light`) before creating any release tag:

```powershell
$ErrorActionPreference = "Stop"

# 0. Point to a locked offline key directory (never inside the Git repo)
$KeyDir = "$HOME\tanpopo-offline-keys"
New-Item -ItemType Directory -Force -Path $KeyDir | Out-Null

$ReleasePrivPem = "$KeyDir\tanpopo-release-root.key.pem"
$ReleasePubPem  = "$KeyDir\tanpopo-release-root.pub.pem"
$PoPrivPem      = "$KeyDir\tanpopo-po-approval.key.pem"
$PoPubPem       = "$KeyDir\tanpopo-po-approval.pub.pem"
$ApprovalJson   = "$KeyDir\gate-rel-07-approval.json"

# Locate openssl (PATH or Git for Windows fallback; PowerShell 5.1+ compatible)
$OpenSSL = $null
$OpenSSLCmd = Get-Command openssl -ErrorAction SilentlyContinue
if ($OpenSSLCmd) { $OpenSSL = $OpenSSLCmd.Source }
if (-not $OpenSSL) { $OpenSSL = "C:\Program Files\Git\usr\bin\openssl.exe" }
if (-not (Test-Path $OpenSSL)) { throw "OpenSSL not found; install Git for Windows or OpenSSL." }

$Principal = if ($env:USERDOMAIN) { "$($env:USERDOMAIN)\$($env:USERNAME)" } else { $env:USERNAME }

# 1. Generate offline Ed25519 keypairs (one-time per key generation; restrict NTFS ACLs to current user)
if (-not (Test-Path $ReleasePrivPem)) {
  & $OpenSSL genpkey -algorithm ED25519 -out $ReleasePrivPem
  if ($LASTEXITCODE -ne 0) { throw "openssl genpkey (release root) failed" }
  & $OpenSSL pkey -in $ReleasePrivPem -pubout -out $ReleasePubPem
  if ($LASTEXITCODE -ne 0) { throw "openssl pkey (release root) failed" }
  icacls $ReleasePrivPem /inheritance:r /grant:r "${Principal}:(R,W)" | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "icacls failed for $ReleasePrivPem" }
}
if (-not (Test-Path $PoPrivPem)) {
  & $OpenSSL genpkey -algorithm ED25519 -out $PoPrivPem
  if ($LASTEXITCODE -ne 0) { throw "openssl genpkey (PO approval) failed" }
  & $OpenSSL pkey -in $PoPrivPem -pubout -out $PoPubPem
  if ($LASTEXITCODE -ne 0) { throw "openssl pkey (PO approval) failed" }
  icacls $PoPrivPem /inheritance:r /grant:r "${Principal}:(R,W)" | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "icacls failed for $PoPrivPem" }
}

# Helper: extract raw 64-hex Ed25519 public key from PKIX SPKI PEM (44 DER bytes = 12-byte header + 32-byte key)
function ConvertFrom-Ed25519PubPemToHex([string]$PemPath) {
  $b64 = ((Get-Content $PemPath) | Where-Object { $_ -and $_ -notmatch '^-----' }) -join ''
  $der = [Convert]::FromBase64String($b64)
  if ($der.Length -ne 44) { throw "Unexpected Ed25519 SPKI DER length $($der.Length) in $PemPath" }
  return ([BitConverter]::ToString($der[12..43]) -replace '-', '').ToLowerInvariant()
}

$ReleaseRootPubHex = ConvertFrom-Ed25519PubPemToHex $ReleasePubPem
$PoPubHex          = ConvertFrom-Ed25519PubPemToHex $PoPubPem
Write-Host "Release Root Public Key Hex: $ReleaseRootPubHex"
Write-Host "PO Approval Public Key Hex:  $PoPubHex"

# 2. Sign all embedded official adapters & verify before embedding into release builds
$CommitSha = (git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $CommitSha.Length -ne 40) { throw "git rev-parse HEAD failed" }
$TargetVersion = "v1.0.0"

$env:CGO_ENABLED = "0"
go run ./cmd/tanpopo plugin release-sign `
  --private-key-file $ReleasePrivPem `
  --builtin `
  --out extensions\official\release-signatures.json
if ($LASTEXITCODE -ne 0) { throw "plugin release-sign failed" }

go run ./cmd/tanpopo plugin release-verify `
  --signatures extensions\official\release-signatures.json `
  --root-pubkey-file $ReleasePubPem `
  --builtin
if ($LASTEXITCODE -ne 0) { throw "plugin release-verify failed" }

# 3. Sign the GATE-REL-07 human PO approval record offline
go run ./scripts/release/pre_publish_check.go `
  --mode sign-approval `
  --target-version $TargetVersion `
  --commit-sha $CommitSha `
  --approved-by "nguywnben" `
  --decision GO_WITH_CONDITIONS `
  --condition "COND-PO-01: disclosed runtime ceilings accepted" `
  --private-key-file $PoPrivPem `
  --out $ApprovalJson
if ($LASTEXITCODE -ne 0) { throw "sign-approval failed" }

# 4. Run full fail-closed pre-publish gate (verifies GATE-REL-01..06, official signatures, matching root key, and GATE-REL-07)
go run ./scripts/release/pre_publish_check.go `
  --mode publish `
  --target-version $TargetVersion `
  --commit-sha $CommitSha `
  --approval-file $ApprovalJson `
  --public-key-hex $PoPubHex `
  --release-signatures extensions\official\release-signatures.json `
  --release-root-pubkey-hex $ReleaseRootPubHex `
  --write-manifest dist\GOVERNANCE_SHA256SUMS
if ($LASTEXITCODE -ne 0) { throw "pre_publish_check publish gate failed" }

# 5. Build Windows (or multi-OS GoReleaser) production binaries embedding only the public release root key
$BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-s -w -X main.version=$TargetVersion -X main.commit=$CommitSha -X main.date=$BuildDate -X github.com/nguywnben/tanpopo/internal/extension/builtin.ReleaseRootPublicKeysHex=$ReleaseRootPubHex"
go build -trimpath -mod=readonly -ldflags $LdFlags -o dist\tanpopo.exe .\cmd\tanpopo
if ($LASTEXITCODE -ne 0) { throw "signed release build failed" }

# Or run GoReleaser across all 5 OS/arch targets with the public key env var set:
# $env:TANPOPO_RELEASE_ROOT_PUBKEYS = $ReleaseRootPubHex
```

> **Note:** Unsigned internal RC builds intentionally skip Steps 2–4 and keep `extensions/official/release-signatures.json` as `{"signatures": []}` with empty `ReleaseRootPublicKeysHex`. They remain valid for internal testing and report `origin_trust_tier: "unverified_external"` and `signature_verified: false`, while `--mode publish` fails closed.

## 4. Pre-GA Sign-Off Checklist (`GATE-REL-07`)

- [ ] Product Owner generates offline Ed25519 root and approval keypairs outside repository/CI.
- [ ] `tanpopo plugin release-sign --builtin` populates `extensions/official/release-signatures.json`.
- [ ] `tanpopo plugin release-verify --builtin` exits `0` for all built-in packages.
- [ ] `pre_publish_check.go --mode publish` verifies all official package signatures, matching trusted release root public key(s), Go `>= 1.26.11`, and signed `GATE-REL-07` approval record.
- [ ] Release build sets `ReleaseRootPublicKeysHex` (`TANPOPO_RELEASE_ROOT_PUBKEYS` in GoReleaser) to the matching 64-hex public key.
- [ ] Fresh startup confirms official adapters surface `origin_trust_tier: "official"` and `signature_verified: true`.
