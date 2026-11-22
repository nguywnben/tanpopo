# `TP-IMP-0044` Execution & Verification Report — Security Audit, `N-1` `migrate-check`, Five-Surface Pen-Test (`GATE-REL-03`), Bounded Fuzzing & Cross-Browser Audit

> **Unit:** `TP-IMP-0044` (`M5` / `EP-09` · **`REQUIRES_STRONG_REVIEW`**)
> **Branch / Workflow:** `develop` -> PR `#36` *(pending merge to `main`, accepted `2026-11-22` under [`DEC-093`](../governance/decisions.md))*
> **Author:** **`Gemini 4 Hydrogen`**
> **Independent Reviews (`REQUIRES_STRONG_REVIEW` + Security & QA Sign-Off):**
> - `code-reviewer` (`60a73de3090a2091de676628b48a3850`): **`APPROVE`** (Critical: 0, Required: 0, Optional: 0, Nit: 0)
> - `security-auditor` (`b1dc21ed1b419ca78f5b63789558091f`): **`PASS`** (Critical: 0, High: 0, Medium: 0, Low: 0, Info: 0)
> - `test-engineer` (`b15828fddb4c2dd3cf527e919d221c07`): **`PASS`** *(with explicit `COND-PO-01` / `GATE-REL-07` disposition ledger)*

---

## 1. Delivered Scope & Security Remediations

1. **Supply-Chain & Static Analysis (`SAST`) + Hardening Fixes:**
   - [`internal/extension/sidecar/mtls_creds.go`](../../internal/extension/sidecar/mtls_creds.go) & [`sidecar_test.go`](../../internal/extension/sidecar/sidecar_test.go): Remediated `gosec` `G123` by setting `SessionTicketsDisabled: true` and enforcing `VerifyConnection` (`verifyConnectionSPIFFE`) across client & server TLS 1.3 configs, verified via repeated non-resuming handshake tests (`DidResume == false`).
   - [`internal/extension/testharness/{validate.go,pack.go}`](../../internal/extension/testharness/validate.go): Remediated `gosec` `G122` by confining all package `manifest.yaml`, `WalkDir` entry, and Tier-2 `.wasm` entrypoint reads inside `os.OpenRoot(absDir)` + `root.ReadFile(...)` alongside `os.Lstat` symlink/regular-file guards (`0` residual `G122` / `G123` findings on `go1.26.11` `gosec`).
   - [`internal/webui/{embed.go,embed_test.go}`](../../internal/webui/embed.go): Cleaned request paths (`path.Clean("/" + r.URL.Path)`) **before** checking `isReservedBackendPrefix`, failing closed (`404 text/plain`) on `/v1`, `/admin/v1`, `/native`, `/mcp`, `/a2a`, and `/.well-known` including unnormalized probes (`//mcp`, `/foo/../mcp`, `/x/../native/openai`, `//.well-known/agent.json`) while preserving strict CSP (`script-src 'self'; frame-ancestors 'none'`).
   - [`internal/admin/http/server.go`](../../internal/admin/http/server.go): Enforced default `Cache-Control: no-store` across all `/admin/v1/*` Control Plane responses.
   - [`internal/cli/support/doctor.go`](../../internal/cli/support/doctor.go): Added `os.Lstat` regular-file/symlink check before reading `lockPath` consistent with `dbPath` and extension `.tpkg` artifacts.
   - [`internal/ingress/http/budget_test.go`](../../internal/ingress/http/budget_test.go): Replaced fragile `time.Sleep(80ms)` inside `TestTP0021_DuplicateClientRequestIDDoesNotBypassBudget` with a deterministic atomic `blockedSeen` barrier (`5/5` pass under `-race`).
   - `govulncheck v1.10.0` (`GOTOOLCHAIN=go1.26.11 ./..`): **`No vulnerabilities found.`** (`exit 0`); `web/console` `npm audit --omit=dev --offline`: **`0` vulnerabilities**.
2. **`N-1` Extension Contract Compatibility Check (`CT-24`, `BA-OQ-05`):**
   - [`internal/extension/testharness/{migrate_check.go,migrate_check_test.go}`](../../internal/extension/testharness/migrate_check.go) & [`internal/cli/plugin/{cmd.go,cmd_test.go}`](../../internal/cli/plugin/cmd.go): Added `tanpopo plugin migrate-check` across all **15** CLI locales ([`internal/pkg/i18n/locales/*.json`](../../internal/pkg/i18n/locales/en.json)) enforcing contract/Core range windows (`ExitContract` `22`), order-independent dotted contract version comparison (`N-1` `COMPATIBLE_WITH_DEPRECATION_WARNING` exit `0`), `ExitSecurity` (`14`) mapping for `scoped_secret_unseal` across `capabilities`/`hooks`/`auth` and wildcard egress / `G122` symlinks (`9/9` subtests `-race` PASS).
3. **Five-Surface Grey-Box Adversarial Penetration Test (`GATE-REL-03`):**
   - [`tests/security/pentest_5surfaces_test.go`](../../tests/security/pentest_5surfaces_test.go): Exercised S1 (`/v1` auth plane, cookie rejection, two-lookup `TTL=0` DNS rebinding `93.184.216.34 -> 169.254.169.254`), S2 (`vk_tp` on `/admin/v1`, CSRF missing token, `200 OK` `/admin/v1/auth/session` `no-store`, unnormalized WebUI probes, real seeded `orgB` `ProviderConnection` `404` IDOR isolation with intact `orgB` row, `FINOPS_VIEWER` `403 insufficient_role_permission`), S3 (real `gzip+tar` `.tpkg` `ErrArchivePathTraversal` + symlink `ErrProhibitedArchiveEntry`, schema-complete `@tanpopo-official/fake` `VerifyProvenance` `ErrReservedNamespaceSpoof`, `Cookie` ban, 16 MiB Wasm OOM trap, Sidecar mTLS ticket suppression), S4 (MCP 1-char schema rug-pull drift, single-use HITL nonce replay, A2A `hop_count=6` `400 delegation_depth_exceeded`), and S5 (`200 OK` `/admin/v1/secrets?project_id=<projA.ID>` zero plaintext leak, cross-org GCM ciphertext transplant rejection, `ShredProjectDEK` cryptographic erasure) + `goleak.VerifyNone(t)`.
4. **Full Empirical Evidence & Honest `COND-PO-01` Disclosure:**
   - [`docs/security/pentest-v1.0-remediation-report.md`](../security/pentest-v1.0-remediation-report.md): Recorded 7-target bounded fuzz runs (`0` crashes across all 7 targets; `>= 4h` soak `UNVERIFIED` and `FuzzLedgerInvariants` `8` execs / `FuzzArchiveAndManifest` `140` execs `PARTIAL`), live Chrome (`23` checks across `1280px`/`768px`/`375px`), Firefox (`23` checks), WebKit (`6` checks, `PARTIAL` HTTP-loopback `__Host-` cookie enforcement), live Chrome CDP CWV (`TTFB=2ms`, `FCP=512ms`, `LCP=824ms`, `CLS=0.0023`), `npx astryx doctor --built dist` (`19 pass, 2 warn, 1 root AGENTS.md fail`), plus open `UNVERIFIED` (`lighthouse` offline, `INP <= 150ms`, remaining `SCR-*/UJ-*`, persistent Playwright CI spec) and open `serious` `axe-core` SC 2.5.3 `label-content-name-mismatch` (`Stepper`, desktop `.tp-scope-switcher`/`.tp-jump-btn`) routed cleanly to **`GATE-REL-07`**.

---

## 2. Verification & Test Evidence Summary

- **Repo Quality Gates & Vet (`CGO_ENABLED=0 go run ./scripts/ci/check.go --all && go vet ./...`):** **`PASS`** (`0` errors).
- **Full Hermetic Short Suite (`CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...`):** **`PASS`** (every Go package green).
- **Security, Hardening & Race Suites (`-race -count=1`):**
  - `tests/security/...` (`TestGATEREL03_FiveSurfaceGreyBoxPentest` + `goleak.VerifyNone`): **`PASS`**.
  - `internal/webui`, `internal/extension/sidecar` (`1.300s`), `internal/extension/testharness` (`1.990s`), `internal/cli/plugin` (`1.511s`), `internal/cli/support` (`2.770s`): **`PASS`**.
  - `internal/ingress/http` (`TestTP0021_DuplicateClientRequestIDDoesNotBypassBudget -count=5`): **`PASS` (`5/5`, `35.648s`)**.
  - Live Dual-DB `PostgreSQL 16` (`FORCE RLS`) + `3-shard Valkey Cluster` (`TestTPIMP0040|TestTPIMP0042` **`-race`**): **`PASS` (`30.115s`)**.
- **Web Console (`(cd web/console && npm run typecheck && npm run check:i18n && npm test)`):** **`PASS`** (`15` locales, `6` test files / `33` Vitest + `axe-core` tests).
