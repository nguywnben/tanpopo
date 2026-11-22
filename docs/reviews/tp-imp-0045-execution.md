# `TP-IMP-0045` Execution & Verification Report — Air-Gap Bundle Toolchain & Multi-Org GA Console

- **Unit ID:** `TP-IMP-0045`
- **Model:** **Gemini 4 Hydrogen**
- **Decision:** `DEC-094` *(initial unit acceptance under `DEC-092` M5 authorization; `13`-locale completion and `astryx doctor` closure later verified under `DEC-097`)*
- **Branch / PR:** `develop` → `main` *(PR [#37](https://github.com/nguywnben/tanpopo/pull/37), merged `main` SHA [`e7189be8`](https://github.com/nguywnben/tanpopo/commit/e7189be8820a374c0c98c89a94fd4a41186cf0f5); post-merge CI [`45005087526`](https://github.com/nguywnben/tanpopo/actions/runs/45005087526) [`Stage 0-1`] & [`45005087419`](https://github.com/nguywnben/tanpopo/actions/runs/45005087419) [`Release Dry Run`] `SUCCESS`)*
- **Review Tier:** **`REQUIRES_STRONG_REVIEW`**
- **Verdict:** **`DONE`** *(with explicit honest limitation notes per `COND-PO-01`)*

---

## 1. Delivered Architecture & Files

### Track A — Air-Gap Offline Bundle Toolchain (`internal/cli/bundle/*`, `cmd/tanpopo/*`)
- **`tanpopo bundle export`, `tanpopo bundle verify`, and `tanpopo bundle import` (`internal/cli/bundle/{bundle,export,verify,import,cmd}.go`, `cmd/tanpopo/main.go`):**
  - `.tpbundle` compressed `.tar.zst` archive format (`github.com/klauspost/compress/zstd`) signed with Ed25519 domain separation (`"tanpopo:airgap-bundle:v1\x00"`), bundling digest-pinned `.tpkg` archives, `tanpopo.lock`, `feeds/index.json`, optional `feeds/revocations.json`, CycloneDX/SPDX SBOMs, and offline deployment artifacts.
  - Zero-network cryptographic verification (`verify.go`) using external pinned trust root (`--public-key`, `TANPOPO_TRUST_ROOT_PUB`, or `/etc/tanpopo/trust-root.pub`), constant-time SHA-256 verification, tar/zstd bomb bounds, `os.OpenRoot` + `os.Lstat` non-symlink confinement, inner `.tpkg` `installer.ExtractSafeArchive` check, and `registry.VerifyFeed` expiry + anti-rollback cursor checks.
  - Atomic rollback-safe import (`import.go`): pre-checks both `regStore.LastIndexSerial` and `regStore.LastRevocationSerial` before touching disk (`CT-31`), stages into sibling temp directories, swaps via two-phase `prepareDirSwap` with LIFO rollback across `destDir`, local/`TANPOPO_BUNDLE_OCI_MIRROR_ROOT` mirror directory, and SQLite feed persistence.
- **Runtime `--offline` & `--multi-org` Flags (`cmd/tanpopo/serve.go`, `internal/admin/http/*`):**
  - Added `--offline` (`TANPOPO_OFFLINE_MODE`) and `--multi-org` (`TANPOPO_MULTI_ORG_ENABLED`, default `true`) flags to `tanpopo serve`.
  - Preserved 5-arg `buildRuntimeInternal` signature, auto-selected `<dataDir>/airgap/packages` when `offlineMode == true` and `TANPOPO_MARKETPLACE_ARCHIVE_ROOT` is unset, and blocked remote `oci://` network pulls when offline.
  - Added `offline_mode` and `multi_org_enabled` flags on `/admin/v1/bootstrap/status` and `/admin/v1/auth/session` / `/admin/v1/auth/login` / `/admin/v1/auth/oidc/refresh`.
  - Added `internal/pkg/i18n/locales/*.json` (15 CLI locales) for `cli.serve.flag.{offline,multi_org}` and `cli.bundle.*`.

### Track B — Multi-Org GA Backend & Web Console (`internal/admin/http/*`, `openapi/admin-v1.yaml`, `web/console/*`)
- **Multi-Org RBAC, PG16 RLS & Anti-Oracle (`internal/admin/http/{server,auth,bootstrap,tenancy,ops,change_requests}.go`):**
  - `requestedTargetOrgID` reads `X-Tanpopo-Org-ID` or `?org_id=`. Non-`OPERATOR` principals belonging to multiple orgs list memberships across PG16 `FORCE RLS` via per-org `withOrgReadTx` while unauthorized `X-Tanpopo-Org-ID` probes return uniform `404 resource_not_found` (`AUTH-15`).
  - `MultiOrgDisabled` (`--multi-org=false`) blocks cross-org switching AND blocks `POST /admin/v1/organizations` with `403 multi_org_disabled`.
  - Instance-wide `EXTENSION` kill-switches triggered by `OPERATOR` via `POST /admin/v1/kill-switches` force `orgID = ""` (`OrgID == nil`) even when `X-Tanpopo-Org-ID` is sent, and `DELETE /admin/v1/kill-switches/{id}` falls back to `GetInstance` when `?org_id=` is absent.
- **Web Console v2 Multi-Org & Air-Gap UX (`web/console/src/*`, `internal/webui/dist/`):**
  - `client.ts` & `managementClient.ts` inject `X-Tanpopo-Org-ID` across `/admin/v1/*` requests, regenerate `generated.ts` from `openapi/admin-v1.yaml`, export `purgeOrgScopedClientState()` (registered listeners, URL query strip, `Playground` local storage clear), and purge org state on `logoutAdmin()`.
  - `OrgSwitcher.tsx` implements progressive disclosure inside the top-bar scope popover (visible when `multi_org_enabled && (organizations.length > 1 || isOperator)`), while `ConsoleShell.tsx` handles org-unbound `OPERATOR` startup, guards rapid org switches via `orgSwitchSeqRef`, remounts `<WorkspaceRouter key={`${activeOrgId}:${orgSwitchEpoch}`}>`, and shows an air-gap banner on `/operations`.
  - `OrganizationsAdminPage.tsx` (`/ops/organizations`) restricts organization creation and crypto-shred erasure to `OPERATOR`, handles both `200 CryptoShredReceipt` and `202 ChangeRequest` responses, and updates all 15 Console locales (`web/console/src/i18n/locales/*.json`) synced to `internal/webui/dist/`.
  - `ExtensionsPage.tsx` + `catalogClient.ts` route extension quarantine through `POST /admin/v1/extensions/{installationId}/quarantine` with `If-Match`.

---

## 2. Independent Strong Review Sign-Off (`REQUIRES_STRONG_REVIEW`)

| Reviewer | Role | Verdict | Findings & Remediation Summary |
|---|---|---|---|
| `033f82b5c7ec4f1d48633afb85c852e7` | `code-reviewer` | **`APPROVED`** *(after remediation)* | Initial review flagged 3 blocking items (`import.go` missing `LastIndexSerial` & non-atomic multi-swap rollback, `ConsoleShell.tsx` null `session.org_id` `OPERATOR` 403 startup, `auth.go` `handleAuthOIDCRefresh` missing `MultiOrgEnabled`) plus non-blocking cleanups (`verify.go` sorted `ExtensionSlugs`, async switch race guard, `logoutAdmin` purge, `OrganizationsAdminPage` 202 shred handling); second pass flagged `multi_org_test.go` kill-switch test contract; final re-inspection verified all fixes and approved. |
| `70d62b7702aeca23250502de93ff0a2d` | `security-auditor` | **`APPROVED`** *(after remediation)* | Initial audit flagged `[HIGH]` multi-tenant fail-open extension quarantine (`ExtensionsPage.tsx` + `ops.go` killswitch scoping when `X-Tanpopo-Org-ID` present), `[MEDIUM]` `--multi-org=false` bypass on `POST /admin/v1/organizations`, and `[LOW]` missing `LastIndexSerial` pre-check in `import.go`. All 3 findings remediated, tested (`TestOperatorExtensionKillSwitchGlobalEvenWithOrgHeader`, `TestMultiOrgListingAndHeaderSwitchingIsolation`), re-inspected, and approved (`0` open findings). |

---

## 3. Empirical Verification & Honest Limitations (`COND-PO-01`)

| Check | Outcome | Observed Result |
|---|---|---|
| Offline bundle & Admin HTTP `-race` | **PASS** | `TANPOPO_TEST_HERMETIC=1 go test -v -race -count=1 ./internal/admin/http -run 'TestMultiOrg\|TestOperatorExtensionKillSwitch'` (`4.447s`, `2/2` PASS); `./internal/cli/bundle/...` (`2.995s`, `3/3` PASS) |
| Full Go short suite (`CGO_ENABLED=0`) | **PASS** | `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...` -> exit `0` |
| Static / AST / OpenAPI / layout gates | **PASS** | `CGO_ENABLED=0 go run ./scripts/ci/check.go --all && go vet ./...` -> exit `0` |
| Web Console v2 verification & embed | **PASS** | `cd web/console && npm run typecheck && npm run check:i18n && npm test && npm run build && npm run sync:embed` -> exit `0` (`36/36` Vitest + `axe-core` tests PASS, `15` locales PASS, `Initial JavaScript gzip: 189.41 KB / 250 KB`, synced to `internal/webui/dist/`) |
| `astryx doctor --built dist` | **PASS** *(reconciled in `TP-IMP-0047`)* | Initial Unit 45 run hit `1` pre-existing `agent-docs` fail (`GEMINI.md`); subsequently resolved cleanly on `develop` (`20 pass, 2 warn [theme-fonts Figtree alias; self-hosted Google Sans fonts bundled]`, `0 fail`), `built-astryx-usage: [ok]`, `0` external browser font/network requests in 3-browser Playwright E2E |
| `--network=none` Container Enclave (`valkey/valkey:8-alpine`) | **PASS** | Outbound dial `1.1.1.1:443` blocked (`--network=none`); `bundle verify` exit `0`; tampered bundle `import` exit `22` with `sentinel.txt` intact; `bundle import --mirror-to oci://...` imported `2` artifacts; `serve --offline` `/readyz=200`, `bootstrap/status` (`offline_mode: true`, `multi_org_enabled: true`), `/admin/v1/marketplace/catalog` `200`, org creation & `X-Tanpopo-Org-ID` isolation PASS |
| Live 3-Browser Playwright Multi-Org & Air-Gap E2E (`web/console/e2e/console_e2e.py`) | **PASS** | Against live `tanpopo serve --offline --multi-org=true` behind ephemeral HTTPS `tlsproxy` (`TANPOPO_ALLOWED_ORIGINS`): non-`127.0.0.1` requests aborted; `Bravo Org` created in UI and switched via `OrgSwitcher`; `scrollWidth <= clientWidth` across `1440`/`768`/`375` px; `/operations` offline banner verified in `vi` & `ja`; `0` external reqs, `0` console/page errors across Chromium (`61` audits), Firefox (`24` audits), and WebKit (`24` audits) |

### Remaining Honest Dispositions (`COND-PO-01`)
1. **Non-`en`/`vi` Locale Strings (`CLOSED` in `TP-IMP-0047` follow-up):** All newly added `cli.bundle.*`, `orgs.*`, and Air-Gap CLI/Console entries across all `13` non-`en`/`vi` locales were translated natively with `0` English fallbacks and verified via `npm run check:i18n`, `go test ./internal/pkg/i18n/...`, and live Playwright locale switching (`vi` & `ja`).
2. **`bundle export` Flag Aliasing:** Because root Cobra binds `--output` globally, `tanpopo bundle export` exposes `--format` for structured output alongside `-o, --output` for the output `.tpbundle` file path.
3. **`astryx doctor --built dist` Exit Code (`CLOSED` in `TP-IMP-0047` follow-up):** Resolves cleanly with `0 fail` (`2 warn` on offline `theme-fonts` Figtree alias with self-hosted Google Sans bundled; `0` external browser font/network requests in Playwright).
