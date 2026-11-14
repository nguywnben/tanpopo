# `TP-IMP-0034` — `Authorized OAuth 2.1` Coordinator (`PKCE S256`, `Client Credentials`, `Device Flow`, Pending Session Table, Single-Flight Refresh & `RFC 8707` Binding)

| Attribute | Value |
|---|---|
| **Unit ID** | `TP-IMP-0034` |
| **Milestone / Epic** | `M3` (`v0.2 MCP Governance & Authorized OAuth 2.1`) · `EP-07` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0010`, `TP-IMP-0024` — `GAP-01, 04`)* |
| **Size** | `L` (`internal/security/oauth/*`, `internal/admin/http/oauth.go`, `db/migrations/sqlite/0011_oauth_sessions.sql`) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(OAuth 2.1 authorization flows, `RFC 8707` cross-audience token-theft prevention, `FMEA-08` refresh stampede protection & `DEC-004` compliance)* |
| **Technical Owner** | `Principal Identity & Security Engineer` |

---

## 1. Intent
Implement the Host-managed **`Authorized OAuth 2.1`** coordinator (`TP-FR-0205`, `SM-03`, `SPEC-EXT-03`) restricted strictly to commercial/enterprise OAuth flows officially sanctioned by upstream AI providers or target MCP servers (`DEC-004` — **never spoofing internal `client_id` values or scraping browser cookies from personal consumer subscriptions**):
1. Support the 3 canonical grant types: `Authorization Code + PKCE (mandatory S256)`, `Client Credentials`, and `Device Authorization Grant (RFC 8628)` backed by the short-lived `oauth_authorization_sessions` table (`TTL <= 10 minutes`, mitigating `CSRF / Session Fixation` via cryptographic `state` nonces bound to `principal_id` + `org_id`).
2. **Single-Flight Token Refresh (`Single-Flight Token Refresh` — `FMEA-08`):** When an `access_token` nears expiration under 500 concurrent requests, **exactly 1 upstream `/token` call** is dispatched (safely handling one-time rotating `refresh_token` grants) while the remaining 499 callers await the result on the same `singleflight.Group` without triggering cascading `refresh_token` invalidation.
3. **Strict Audience & Resource Binding (`RFC 8707 Resource Indicators` — `AUTH-16` / `CT-19`):** Every issued token is cryptographically bound to its target `bound_resource_uri` / `audience` — `BlindBroker` unconditionally rejects any attempt to replay a Service A token against Service B (`No Token Passthrough / No Confused Deputy`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/security/oauth/pkce.go`, `client_credentials.go`, `device_flow.go` (routing all HTTP calls to Authorization Servers through `SSRFGuardDialer`; paired with `internal/testkit/fakeoauth/` for hermetic testing).
  - `internal/security/oauth/refresh_coordinator.go` (`singleflight` keyed by `credential_id`, proactive refresh at `skew = 60s`, atomic persistence of rotated `access_token` + `refresh_token` pairs into `vault` with `AEAD AAD`; on upstream `400 invalid_grant`, immediately halts retries at `0` retries, transitions `SM-02/SM-03` to `AUTH_EXPIRED_REAUTH_REQUIRED`, and emits `ALT-OAUTH-REAUTH` per `AUTH-10` / `INC-05`).
  - `internal/admin/http/oauth.go` (`/admin/v1/oauth/authorize`, `/admin/v1/oauth/callback`, `/admin/v1/oauth/device/start`, `/admin/v1/oauth/device/poll`).
  - `db/migrations/sqlite/0011_oauth_sessions.sql` (`oauth_authorization_sessions`, `oauth_token_leases`).
- **Out-of-Scope:** Console administrator login via `Enterprise OIDC SSO` (covered in `TP-IMP-0041`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0205`, `TP-BR-0301`..`0304`, `DEC-004`, `SM-03`, `CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`, `FMEA-08`, `INC-05`.
- **Dependencies:** `TP-IMP-0010`, `TP-IMP-0024`.
- **Design Refs:** [`authentication-connections.md`](../../specifications/authentication-connections.md) (`SPEC-EXT-03`), [`secrets-and-tenant-isolation.md`](../../security/secrets-and-tenant-isolation.md) Section 3.3, [`nonfunctional-slo.md`](../../quality/nonfunctional-slo.md) (`FMEA-08`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-19`, `AUTH-09`, `AUTH-10`, `AUTH-16`).

## 4. API, Wire & Data Contracts
- **Anti-`state`-Swap & `PKCE` Downgrade Prevention Invariant:** Reject any configuration specifying `code_challenge_method = "plain"` or unencrypted `http://` endpoints (except `127.0.0.1` in hermetic test mode); `callback` succeeds only when invoked from the exact browser session of the `principal_id` who initiated an unexpired `state`.
- **Zero Caller-to-Upstream Token Passthrough (`No Upstream Token Relay`):** Caller credentials presented to Tanpopo authenticate exclusively against Tanpopo; outbound Provider/MCP tokens are always injected by `BlindBroker` from vault leases bound to the exact target `resource`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`FMEA-08` & `CT-19` Single-Flight Refresh Under 200 Concurrent Calls):** Given `FakeOAuthIdP` enforcing `One-Time Rotating Refresh Token` semantics (where a second request reusing the old `refresh_token` revokes the entire grant), When the token expires as 200 goroutines concurrently invoke `BlindBroker`, Then `FakeOAuthIdP` observes **exactly 1 `/token` request** and all 200 callers succeed at `100%`.
- **`NEG-1` (`AUTH-09` OAuth Callback CSRF / Replay):** Calling `GET /admin/v1/oauth/callback` with a forged `state`, an expired `state` (`> 10m`), a replayed `state`, or from a different user's session fails with `400 invalid_oauth_state`.
- **`NEG-2` (`AUTH-16` & `CT-19` Cross-Audience Token Theft Attempt):** Configuring a connection to `https://mcp-other.example.com` backed by a token bound to `resource = https://mcp-billing.example.com` is hard-blocked by `BlindBroker` with `403 credential_audience_mismatch`.
- **`NEG-3` (`AUTH-10` & `INC-05` Upstream `invalid_grant` Revocation):** When `FakeOAuthIdP` returns `400 invalid_grant` during refresh, the coordinator aborts retries immediately (`0` retries to prevent retry storms), transitions the credential to `AUTH_EXPIRED_REAUTH_REQUIRED`, and increments `ALT-OAUTH-REAUTH`.
- **`NEG-4` (`DEC-004` Compliance Guard):** Attempting to submit browser-session `Cookie` extraction payloads into an OAuth connection schema is rejected at schema validation (`CR-05`).

## 6. Test Commands & Required Evidence
- Pure Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/security/oauth/... ./internal/admin/http/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/security/oauth/... ./internal/admin/http/...`)*
- Race detector (`-race`): `go test -v -race ./internal/security/oauth/... ./internal/admin/http/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** `access_token`, `refresh_token`, and `code_verifier` values are sealed under `AEAD AAD` with transient plaintext byte slices zeroed immediately after use; `0` token bytes appear in post-callback UI redirect URLs.
- **Rollback:** Additive `0011_oauth_sessions.sql` migration; `OAUTH2` connections can be set to `DISABLED` without affecting `API_KEY` connections.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0003` (`oauth_authorization_sessions` DDL), `TP-IMP-0010`, and `TP-IMP-0024` are complete.
- **DoD:** `CT-19, 20`, `AUTH-09, 10`, and the 200-concurrency `FMEA-08` stampede test passing under `-race`, with `REQUIRES_STRONG_REVIEW` sign-off achieved.
