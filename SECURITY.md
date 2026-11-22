# Security Policy

The Tanpopo project takes security, tenant isolation, credential custody, and plugin supply-chain integrity seriously.

## Coordinated Vulnerability Disclosure

**Please do not report security vulnerabilities through public GitHub issues or pull requests.**

To report a suspected vulnerability privately:
1. Use GitHub's **Private Vulnerability Reporting** ("Security" -> "Advisories" -> "Report a vulnerability") on [`nguywnben/tanpopo`](https://github.com/nguywnben/tanpopo/security/advisories/new).
2. Include a minimal reproduction, affected commit SHA or release version, impacted plane (Data Plane `/v1/*` / `/native/*`, Control Plane `/admin/v1/*`, MCP `/mcp`, A2A `/a2a/*`, CLI/Bundle, or Web Console), and expected vs. observed behavior.

### Response & Remediation Targets (`SPEC-QA-04`)

| Severity | Definition | Target Triage | Release Gate Rule |
|---|---|---|---|
| **`Sev-0 / P0`** | Sandbox escape (Wasm/Sidecar), upstream secret leakage, cross-tenant `FORCE RLS` / cache bleed, SSRF to metadata endpoints, unflagged ledger corruption, `DEC-004` compliance breach | Immediate | **Hard blocks releases (`0` open P0)**; triggers emergency patch + `revocations.json` advisory |
| **`Sev-1 / P1`** | Broken primary routing/failover fence, side-effecting MCP blind retry, broken `N-1` rollback, auth/CSRF bypass | `< 24 hours` | **Blocks releases (`0` open P1)**; requires failing Prove-It regression test before fix |
| **`Sev-2 / P2`** | Moderate issue with documented workaround and no tenant/security bypass | `< 72 hours` | Documented under `Known Issues` in Release Notes if deferred |
| **`Sev-3 / P3`** | Low-risk hardening / UX polish | Next sprint | Routine batching |

---

## Supported Release Branches & Toolchain Baseline

- **Active Development:** `main` (release-gated trunk) and `develop`.
- **Minimum Go Toolchain:** Go **`1.26.11+`** recommended for builds (`govulncheck` stdlib CVE clean baseline per [`docs/security/pentest-v1.0-remediation-report.md`](./docs/security/pentest-v1.0-remediation-report.md)).
- **Web Console Supply Chain:** Dependencies are pinned via `web/console/package-lock.json` and installed with `npm ci --ignore-scripts`.

---

## Core Security Architecture & Hardening Controls

- **Write-Only 3-Tier Envelope Vault:** `Master KEK -> Org DEK -> Project DEK` (`AES-256-GCM`) with context-bound `AEAD AAD` and optional Cloud KMS / Vault custody (`AWS KMS`, `GCP KMS`, `HashiCorp Vault Transit`). Plaintext secrets are never returned over `/admin/v1/*` or exposed to untrusted plugins (`BlindHostCredentialBroker`).
- **Egress & DNS Rebinding Protection:** `SSRFGuard` inspects resolved peer IPs inside `net.Dialer.Control` at connect time (`DisableKeepAlives: true`), blocking loopback/private/link-local CIDRs unless explicitly consented with non-wildcard CIDRs and control-plane port blocking.
- **Multi-Tenant Isolation:** PostgreSQL 16+ `FORCE RLS` (`tanpopo.org_id` / `tanpopo.operator`), uniform `404 resource_not_found` anti-oracle enforcement (`AUTH-15`), top-level `org_id` body spoofing rejection (`403 org_id_spoofing_forbidden`), strict Control/Data plane credential firewall (`vk_tp_*` barred from `/admin/v1/*`), `__Host-tanpopo_admin_session` + `X-CSRF-Token`, and `Cache-Control: no-store`.
- **Supply-Chain & Air-Gap Verification:** Digest-pinned `.tpkg` archives, `os.OpenRoot` + `os.Lstat` non-symlink confinement, `tanpopo.lock`, Ed25519 domain-separated `.tpbundle` verification (`CT-31`), and TLS 1.3 non-resuming SPIFFE mTLS (`SessionTicketsDisabled: true`) on Tier-3 sidecars.
- **Compliance Boundary (`DEC-004`):** Tanpopo prohibits building, bundling, or documenting consumer-subscription credential harvesting or ToS-evasion proxies.
