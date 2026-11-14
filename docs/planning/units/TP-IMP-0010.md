# `TP-IMP-0010` — Connect-Time Anti-`SSRF / DNS Rebinding` Dialer, `Blind Host Credential Broker` & `extension/port` Contracts

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0010` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-02` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0004`, `TP-IMP-0008` — `GAP-01, 02`)* |
| **Size** | `M` (`internal/security/egress/*`, `internal/security/broker/*`, `internal/extension/port/*`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Outbound network trust boundaries `TB-3/TB-4` & raw-secret isolation from Plugins)* |
| **Technical Owner** | `Principal Security / Networking Engineer` |

---

## 1. Intent
Build the two foundational outbound security enforcement layers per [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md) and [`threat-model.md`](../../security/threat-model.md) (`TM-05`, `TM-06`), while locking the shared `extension/port.ProviderRuntime` Go interface so Tier-1 (`0012`) and Tier-2 (`0013`) engines develop in parallel with zero signature drift:
1. **`SSRFGuardDialer`:** Validates target hostnames against `allowed_hosts` **and** inspects resolved IP addresses at the exact moment of kernel socket creation (`net.Dialer.Control`) to defeat `DNS Rebinding` (`TTL=0`), private `RFC1918` ranges, `loopback`, `link-local`, and cloud metadata endpoints `169.254.169.254` / `fd00:ec2::254` (unless `OPERATOR`/`ORG_OWNER` explicitly enables `allow_private_ip = true` with a narrowed `allowed_cidrs` list for self-hosted LAN GPU servers, while honoring `netguard.CheckLoopbackAddr` in hermetic test mode).
2. **`BlindHostCredentialBroker`:** Accepts intermediate HTTP requests carrying only opaque `secret_slot` names from Adapters/Wasm via an abstract `CredentialBindingResolver` interface (decoupled from `0011`/`0012` DB tables), verifies `bound_audience` against `target_host`, unseals via `vault.UnsealCapability` (`TP-IMP-0008`) to inject headers/signatures directly into the Host socket buffer, **hard-bans outbound `Cookie`/`Set-Cookie` injection (`CR-05`)**, and **automatically scrubs echoed secret substrings from upstream `401/403/500` error bodies (`CT-16`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/extension/port/runtime.go` (shared `ProviderRuntime` interface declaring `TransformRequest`, `TransformResponse`, `TransformStreamChunk`, `NormalizeError`, `ExtractUsage`, `InspectNativeRequest`, `BuildValidationProbe`, and `VerifyValidationResult` shared across `TP-IMP-0012`, `0013`, and `0042`).
  - `internal/security/egress/ssrf_dialer.go` (CIDR denylist blocking `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `100.64.0.0/10`, `::1/128`, `fc00::/7`, `fe80::/10`, and IPv4-mapped IPv6 `::ffff:169.254.169.254` directly inside `Control`).
  - `internal/security/egress/redirect_guard.go` (`CheckRedirect` re-evaluating `allowed_hosts` + `SSRFGuardDialer` on every `301/302/307/308` hop and stripping `100%` of auth headers on cross-origin redirects — `CT-15`).
  - `internal/security/broker/blind_broker.go` (`CredentialBindingResolver` + `port.AuditSink`; Host-side `Bearer`, `Header`, `Basic`, and `HMAC_SHA256` injection, hard rejection of `Cookie` injection instructions, and echoed-secret scrubbing on upstream error payloads prior to returning to Plugins/Clients — `CT-16`).
- **Out-of-Scope:** Full `AWS_SIGV4` signing for Bedrock & `OAuth 2.1` single-flight refresh (owned by `TP-IMP-0034` and `TP-IMP-0035` in `M3`); `Cloud Workload Identity` (owned by `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0202`, `TP-FR-0303`, `TP-FR-0702`, `TP-FR-0703`, `TP-BR-0304`, `TP-BR-0402`, `CT-14`, `CT-15`, `CT-16`, `CT-26`, `AUTH-16`.
- **Dependencies:** `TP-IMP-0004`, `TP-IMP-0008`.
- **Design Refs:** [`ADR-0002`](../../architecture/adr/0002-three-tier-extension-sandbox-and-blind-broker.md), [`authentication-connections.md`](../../specifications/authentication-connections.md) Sections 1 & 2, [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Section 3, [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-14`..`16`, `CT-26`, `AUTH-16`).

## 4. API, Wire & Data Contracts
- **`EgressRequest` Contract & Audience Binding (`AUTH-16`):**
  - Host invokes `resolver.ResolveSlot(ctx, orgID, connectionID, slotName)` → verifies `HostMatches(URL.Hostname(), AllowedHosts) && AudienceMatches(URL, Secret.BoundAudience)`. Any mismatch aborts prior to opening a TCP socket, records an audit event via `AuditSink`, and returns `403 egress_host_denied` or `403 credential_audience_mismatch`.
  - If an injection instruction specifies `Cookie` or `Set-Cookie` (case-insensitive) in `field_name` → hard-reject with `422 prohibited_cookie_injection` (`DEC-004` / `CR-05`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given a `Connection` declaring `allowed_hosts = ["127.0.0.1:19080"]` with `allow_private_ip = true` and `allowed_cidrs = ["127.0.0.1/32"]` (pointing at `FakeLLMServer`), When sending a valid `EgressRequest` referencing `secret_slot = "api_key"`, Then `FakeLLMServer` receives `Authorization: Bearer sk-tp-canary-...` while the response structure returned to the Plugin contains `0` bytes of the secret.
- **`NEG-1` (`CT-14` Unapproved Domain Exfiltration):** A Plugin approved for `api.example.com` attempting HTTP egress to `evil.attacker.invalid` → blocked prior to DNS resolution with `403 egress_host_denied`.
- **`NEG-2` (`CT-15` Cross-Host Redirect Strip & SSRF):** An approved upstream returning `302 Location: https://other-host.test/steal` or `http://169.254.169.254/` → blocked and/or stripped of `100%` of `Authorization` headers with `0` secret bytes leaked.
- **`NEG-3` (`CT-16` Upstream `401` Echoed-Secret Redaction & Outbound Cookie Ban):** Enabling `X-Tanpopo-Fake-Fault: echo_auth_header_in_error` on `FakeLLMServer` so its `401` body echoes `sk-tp-canary-...` verbatim → `BlindBroker` scrubs and replaces the secret with `[REDACTED_CREDENTIAL]` before handing the response to the Plugin/Client; attempting `Cookie` injection is rejected with `422`.
- **`NEG-4` (`CT-26` & `AUTH-16` DNS Rebinding `TTL=0`, Port Scan & Audience Mismatch):** A mock DNS resolver returning a public IP on lookup #1 and flipping to `169.254.169.254` or `[::ffff:127.0.0.1]` at `Dial` time → `Control` hook aborts before emitting a TCP `SYN`; probing an unapproved LAN port `10.20.0.5:6379` or binding an `api.openai.com` credential to `custom-proxy.test` is immediately blocked.

## 6. Test Commands & Required Evidence
- Pure-Go verification (`CGO_ENABLED=0`):
  - POSIX: `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/extension/port/... ./internal/security/egress/... ./internal/security/broker/...`
  - PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/extension/port/... ./internal/security/egress/... ./internal/security/broker/...`
- Data-race verification (`-race`): `go test -v -race ./internal/security/egress/... ./internal/security/broker/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Every outbound `http.Client` across the entire Tanpopo process **must** be constructed via `egress.NewGuardedHTTPClient(...)`; `http.DefaultClient` / `http.DefaultTransport` are strictly banned (enforced by `scripts/ci/check.go`).
- **Rollback:** Standalone foundational security library packages; cleanly revertible.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (locking `BlindBroker` RPC contracts), `TP-IMP-0004`, and `TP-IMP-0008` are complete.
- **DoD:** `100%` pass across adversarial suites `CT-14, 15, 16, 26` and `AUTH-16`, `CanaryScanner` confirms `0` secret leaks, and `REQUIRES_STRONG_REVIEW` sign-off is complete.
