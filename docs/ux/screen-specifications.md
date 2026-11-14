# Screen Inventory, UI Contracts & UX Guardrails — Tanpopo

> **Detailed Screen Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-UX-03` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authored By:** Lead Product Designer & Security UX Architect
> - **Machine-Readable Contract Parity:** Every `/admin/v1/*` and Data Plane endpoint listed under `Data Contracts` across `SCR-01`..`SCR-16` matches [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml) and [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml) `100%` character-for-character (HTTP method, slash-normalized path, path parameters, `ETag`/`If-Match`, `X-CSRF-Token`, and cursor pagination).
> - **Measurement Honesty (`COND-PO-01`):** Every quantitative completion window, propagation SLA, and frame-rate target (`< 3 minutes`, `< 15 seconds`, `< 1 s`, `< 5 s`, `< 60 s`) is **`TARGET / UNVERIFIED`** until empirical E2E and load verification in `M2`–`M5`.
> - **Requirements Traceability:** `TP-FR-0201`..`0904`, `TP-NFR-0402`, `TP-NFR-0404`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-1001`, `TP-NFR-1002`, `DEC-004`, `CR-03`, `CR-04`, `CR-10`.

---

## 1. Six Mandatory Experience Guardrails (`UX-GR-01` .. `UX-GR-06`)

Every screen in the catalog below enforces six non-negotiable security, compliance, and technical-honesty guardrails without exception:

| Guardrail ID | Core Principle | Mandatory UI Behavior & Strict Prohibitions |
|---|---|---|
| **`UX-GR-01`**<br/>**Write-Only Secrets & One-Time Virtual Key Reveal** | Never re-display sensitive secret material once persisted. | - Every upstream API Key, HMAC Secret, OAuth Client Secret, and Webhook Secret field submits write-only to `POST /admin/v1/secrets`; once sealed, only an opaque `credential_id`, trailing fingerprint (e.g., `...9a2f`), and creation timestamp are shown — **strictly prohibiting any `"Show / Reveal Secret"` button**.<br/>- Upon issuing or rotating a `Virtual Key` (`vk_tp_...`), a modal `Dialog` displays `secret_key` **strictly once** alongside the warning: *"Copy this key now; Tanpopo stores only its plain SHA-256 digest and cannot display it a second time."* |
| **`UX-GR-02`**<br/>**Honest Disclosure of Unsupported Capabilities** | Never silently drop parameters or promise `100%` universal parity. | - Across `Model Catalog`, `Route Builder`, and `Playground`, every canonical `snake_case` capability (`streaming`, `tool_calling`, `parallel_tool_calls`, `strict_json_schema`, `json_object`, `vision`, `audio_input`, `document_input`, `reasoning`, `prompt_caching`, `embeddings`, `rerank`) carries its empirical assurance `Token` (`VERIFIED` / `TESTED` / `DECLARED` / `UNSUPPORTED`).<br/>- Whenever a feature operates losslessly only via native dialect passthrough (`/native/{dialect}/{upstreamPath}`), the UI states that explicitly and offers a one-click toggle to the `Native Passthrough` snippet rather than silently stripping request fields. |
| **`UX-GR-03`**<br/>**Strict Prohibition of `"Official / Supported"` Labels on Consumer Subscriptions** | Enforce the legal and terms-of-service boundary of `DEC-004` & `ADR-0005`. | - Strictly **never** attach `Official`, `Verified`, or `Supported` labels — nor provide instructions — for turning individual consumer subscriptions (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Google Antigravity`, etc.) into shared multi-user API gateways.<br/>- The `Provider Connection` form exposes strictly two billing modes: `COMMERCIAL_API` and `SELF_HOSTED_INFRA`; both UI validation and `BlindBroker` reject pasted browser `Cookie` headers or unauthorized personal session tokens. |
| **`UX-GR-04`**<br/>**Never Hide Third-Party Risk Behind a Single Install Button** | Surface permissions, publisher identity, and origin prior to any activation. | - Across `Extension Marketplace` listings and custom `.tpkg` installers, the primary call-to-action is always **`"Inspect Permissions & Trust..."`** (never a silent one-click install-and-enable button).<br/>- The mandatory inspection sheet (`GET /admin/v1/extensions/{installationId}/permission-diff`) renders `Origin`, `Origin Trust Tier` (`official`, `verified`, `unverified_external` + `enterprise_signed` flag per `CR-03`), `Allowed Hosts`, `Secret Slots`, and `16 MiB` Wasm quotas before activation unlocks. Checking the `unverified_external` acknowledgment box explicitly notes that operator consent **never waives `DEC-004` / `TP-BR-0301` compliance rules**. |
| **`UX-GR-05`**<br/>**Proactive, Context-Timely Operational Warnings** | Surface data-loss, scale-ceiling, and billing-drift hazards before impact. | The `AppShell` system `Banner` or contextual warning callout triggers automatically whenever any of the following conditions are detected:<br/>1. Unacknowledged backup of `TANPOPO_MASTER_KEY` in a production deployment;<br/>2. Single-node SQLite mode exceeding recommended operating thresholds (`> 100 RPS` sustained or WAL file `> 512 MiB` — `TARGET / UNVERIFIED`) -> prompting `tanpopo db migrate-engine` migration to PostgreSQL;<br/>3. Data Plane cluster node revocation sync lag exceeding `> 5 seconds` (`tanpopo_config_snapshot_age_seconds > 5`);<br/>4. Requests settled as `UNMETERED_FLAGGED` due to missing model rate cards;<br/>5. Active extensions targeting a deprecated `N-1` contract version approaching end-of-support. |
| **`UX-GR-06`**<br/>**Optimistic Concurrency Control & Anti-Fat-Finger Destructive Gates** | Prevent silent overwrites across concurrent administrators and accidental blast-radius actions. | - Every mutating configuration request attaches `If-Match: "<etag>"` (and `X-CSRF-Token` for browser cookie sessions); on `409 optimistic_lock_conflict`, the UI renders a side-by-side `Diff` against `error.current_resource` rather than discarding the operator's draft (`UX-AC-11`).<br/>- High-blast-radius destructive operations (`Quarantine Extension`, `Freeze Project`, `Delete Project`, `Delete Organization`) invoke an `AlertDialog` requiring exact `confirm_slug` re-entry and a mandatory audit `reason` (`>= 5` characters). |

---

## 2. Shared Accessibility (`WCAG 2.2 AA`), 6-Role RBAC & Four-Tier Responsive Contract

1. **Canonical 6-Role RBAC Model (`CR-04`):**
   Every screen enforces action-level permissions across the **6 canonical RBAC roles**: `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, and `DEVELOPER` (in `Personal Admin` standalone bootstrap, the initial owner holds `OPERATOR + ORG_OWNER`).
2. **Keyboard & Screen Reader Accessibility (`WCAG 2.2 AA`):**
   - `AppShell` provides a built-in `Skip to main content` link at the first `Tab` stop, a `banner` landmark, distinct labeled `nav` landmarks, and exactly one `Heading level={1}` inside `main`.
   - Minimum contrast ratio `>= 4.5:1` for body text and `>= 3:1` for large text and interactive boundaries across both Light and Dark modes of the `neutral` theme.
   - **Never rely on color alone:** Every status `Token` pairs a valid built-in semantic `Icon` from `@astryxdesign/core/Icon` with a concise text `label`.
   - Asynchronous UI state changes announce via `useAnnounce` (`polite` for status updates, `assertive` for errors and urgent alerts); every `Dialog` and `AlertDialog` traps focus, closes on `Escape`, and restores focus on close.
3. **Continuous Four-Tier Responsive Viewport Contract (`375px` to `>= 1280px` — Zero Breakpoint Gaps):**
   - **Desktop (`>= 1280px`):** Displays all 3 workspace regions simultaneously (`SideNav 256px` + `LayoutContent fill` + `LayoutPanel 380px` when inspecting a selected row).
   - **Compact Desktop / Laptop (`1024px – 1279px`):** Retains persistent `SideNav` (`220px–256px`) + `LayoutContent fill`, automatically dropping optional wizard guidance rails first and presenting `LayoutPanel` detail inspectors as scrim-free/light slide-over `Drawer` overlays (`width={360}`) so primary `Table` viewports never crush below readable column widths.
   - **Tablet (`768px – 1023px`):** Collapsible `SideNav` paired with `LayoutPanel` inspectors converted into a modal `Dialog` or scrim-backed `Drawer` (`useMediaQuery('(max-width: 1023px)')`).
   - **Mobile Emergency (`375px – 767px`):** `SideNav` collapses into `AppShell`'s mobile navigation drawer (`mobileNav={{ breakpoint: 'md' }}`), KPI grids stack cleanly to 1 column (`Grid columns={{ minWidth: 240, max: 4 }}`), touch targets enforce `>= 44x44 CSS px`, multi-column tables scroll horizontally with a sticky leading identifier column, and the **4 emergency on-call workflows (`SCR-02`, `SCR-13`, `SCR-14`, `SCR-16`)** complete one-handed in **`< 15 seconds` (`TARGET / UNVERIFIED`)**.

---

## 3. Detailed Specifications for All 16 Canonical Screens (`SCR-01` .. `SCR-16`)

### Group 1: Bootstrap, System Posture & Model Gateway (`SCR-01` .. `SCR-05`)

#### `SCR-01`: First-Run Setup Wizard & Admin Login (`/setup`, `/login`)
- **Objective:** Securely transfer instance ownership on first boot via a one-time `Bootstrap Token` and guide the operator to their first verified streaming request in **`< 3 minutes` (`TARGET / UNVERIFIED` — `UJ-01`)**, or authenticate daily administrative sessions (local password / Enterprise OIDC SSO).
- **Actor & RBAC:** `Personal Admin` / `OPERATOR` (first run at `/setup`); all 6 Console roles at `/login`.
- **Data Contracts:** `GET /admin/v1/bootstrap/status`, `POST /admin/v1/bootstrap/complete`, `POST /admin/v1/secrets`, `POST /admin/v1/connections`, `POST /admin/v1/connections/{connectionId}/test`, `POST /admin/v1/deployments`, `POST /admin/v1/routes`, `POST /admin/v1/virtual-keys`, `POST /v1/chat/completions` (first SSE stream smoke test), `POST /admin/v1/auth/login`, `GET /admin/v1/auth/oidc/start`, `GET /admin/v1/auth/oidc/callback`, `GET /admin/v1/auth/session`, `POST /admin/v1/auth/logout`.
- **Actions:** Verify bootstrap posture; redeem `Bootstrap Token` to create initial `ORG_OWNER` + default `Organization`/`Project`/`Environment`; acknowledge `TANPOPO_MASTER_KEY` custody; connect an official commercial API key **or** a local `Ollama`/`vLLM` endpoint (with explicit **`Allow Private / Loopback Upstream (`allow_private_ip`)`** consent checkbox persisting `provider_connections.allow_private_ip = true`); atomically provision `Model Deployment` in **`ACTIVE_HEALTHY`** state + `Default Route` + show-once `Virtual Key` (`CR-10`); fire a live streaming test request.
- **Validations:** Unexpired `Bootstrap Token`; admin password `>= 12` characters; even when `allow_private_ip = true` is checked for `127.0.0.1:11434` or LAN GPU subnets, cloud metadata endpoints (`169.254.169.254`, `fd00:ec2::254`) and Tanpopo's own listener ports (`:8080`, `:9090`) remain unconditionally blocked (`SSRFGuard`).
- **Errors:** `410 bootstrap_already_completed` once setup is sealed, `401` expired/invalid bootstrap token (displays `tanpopo admin reset-bootstrap-token`; for forgotten local owner credentials post-setup, documents host break-glass `tanpopo admin reset-local-owner` per `CR-09`), `422` upstream probe failure, `429 auth_rate_limited`.
- **Confirmation:** Mandatory checkbox confirming the show-once `Virtual Key` has been stored before dismissing the key dialog (`UX-GR-01`).
- **Audit:** `system.bootstrap.claimed`, `iam.owner.created`, `connection.created`, `deployment.created`, `route.created`, `virtual_key.issued`.
- **Success Feedback:** Step completion card displaying `request_id`, first-call `TTFT` latency, and primary button entering `/overview`.
- **Accessibility & Responsive:** Built on `form-wizard-vertical` (guidance rail drops at `1024px–1279px`, stepper pivots horizontally below `768px`); `/login` uses `login-sso` with initial focus on the email field.

#### `SCR-02`: Overview & System Posture Dashboard (`/overview`)
- **Objective:** Deliver an immediate posture snapshot covering request throughput, internal vs. upstream error rate, `p95` proxy overhead, `micro-USD` settled + active pre-hold burn, unhealthy `Model Deployments`, and actionable alerts.
- **Actor & RBAC:** All 6 roles (telemetry scoped to caller RBAC: `FINOPS_VIEWER` sees spend/KPIs; `DEVELOPER` sees authorized project/key metrics).
- **Data Contracts:** `GET /admin/v1/overview/summary` (`?window=1h|24h|7d|30d`), `GET /admin/v1/deployments` (`?status=CIRCUIT_OPEN_UNHEALTHY`), `GET /admin/v1/alerts/active`.
- **Actions:** Switch time window (`1h`, `24h`, `7d`, `30d`); deep-link from any alert (`ALT-GATEWAY-BURN`, `ALT-REVOCATION-LAG`, `ALT-BUDGET-80`, `ALT-OAUTH-EXPIRY`, `ALT-UNMETERED-SPEND`) to its remediation screen.
- **Validations:** Query window falls within configured retention bounds.
- **Errors:** Renders local telemetry accompanied by a sync-lag warning `Banner` when OTLP export buffers drop spans (`tanpopo_telemetry_dropped_total > 0`).
- **Confirmation:** None required for read-only window filtering.
- **Audit:** None for read-only dashboard queries.
- **Success Feedback:** Freshness indicator (`Updated 5s ago`) and polite `useAnnounce` notification for newly arrived alerts.
- **Accessibility & Responsive:** Built on `dashboard-alert-rail`; KPI row uses `Grid columns={{ minWidth: 240, max: 4 }}` folding cleanly across `1024px`, `768px`, and `375px`.

#### `SCR-03`: Provider Adapters Directory (`/providers/adapters`)
- **Objective:** Inspect all protocol-level `Provider Adapters` installed on the instance (5 `v0.1` Official Reference Adapters: `OpenAI`, `Anthropic`, `Gemini`, `OpenAI-Compatible/vLLM`, `Ollama`, plus `v0.2` `Bedrock`/`Azure` and external `.tpkg` adapters per `CR-01`), compare capabilities, and launch `Provider Connection` creation.
- **Actor & RBAC:** Read: all 6 roles; Install/manage instance packages: `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/provider-adapters` (`?limit=50&cursor=...`), `GET /admin/v1/provider-adapters/{adapterId}`.
- **Actions:** Filter by wire protocol or execution tier (`declarative`, `sandboxed_wasm`, `isolated_sidecar`); inspect supported auth methods (`host_blind_inject`, `oauth2_pkce`, `host_crypto_sign`) and click **`"New Connection with this Adapter"`**.
- **Validations:** Connection creation enabled solely when backing extension installation is `ENABLED_ACTIVE` and permitted by `org_extension_policies`.
- **Errors:** `QUARANTINED` adapters render a red error `Token` disabling connection creation with a link to the quarantine audit record.
- **Confirmation:** Not applicable (navigates to `SCR-04` or `SCR-06`).
- **Audit:** Not applicable for read-only inspection.
- **Success Feedback:** Smooth `LayoutPanel` / `Drawer` detail expansion without full-page reload.
- **Accessibility & Responsive:** Built on `table-page`; detail inspector slides over at `1024px–1279px` and opens as `Dialog` at `<= 1023px`.

#### `SCR-04`: Provider Connections & Authorized OAuth Vault (`/providers/connections`)
- **Objective:** Manage live upstream credential profiles (`SM-02`, `SM-03`), seal write-only multi-slot secrets into the 3-tier Envelope Vault, execute `Authorized OAuth 2.1 + PKCE (S256)` flows, run 2-phase blind connectivity tests, and disable failing connections (including directly from `DEGRADED`).
- **Actor & RBAC:** `ORG_OWNER` (`Org` scope), `TEAM_ADMIN` (`Project` scope); metadata read-only (`0` secret write/probe privileges): `SECURITY_REVIEWER`, `FINOPS_VIEWER`.
- **Data Contracts:** `GET /admin/v1/secrets`, `POST /admin/v1/secrets`, `DELETE /admin/v1/secrets/{credentialId}` (`If-Match`), `GET /admin/v1/connections`, `POST /admin/v1/connections`, `PATCH /admin/v1/connections/{connectionId}` (`If-Match`), `DELETE /admin/v1/connections/{connectionId}` (`If-Match`), `POST /admin/v1/connections/{connectionId}/test`, `POST /admin/v1/connections/{connectionId}/oauth/start`, `GET /admin/v1/oauth/callback`.
- **Actions:** Seal write-only credentials (`secret_bindings` supporting multiple slots); create connection (`COMMERCIAL_API` or `SELF_HOSTED_INFRA`, optional `allow_private_ip` opt-in); initiate or re-authorize OAuth 2.1; run **`"Test Connection"`** (`BuildValidationProbe` -> `VerifyValidationResult`); disable or delete connection.
- **Validations:** Billing mode restricted to `COMMERCIAL_API | SELF_HOSTED_INFRA` (`UX-GR-03`); outbound `Cookie` header injection is hard-banned (`NEG-3`); custom endpoints must match `allowed_hosts` and pass `SSRFGuard`.
- **Errors:** `400 ssrf_blocked_private_ip`, `409 optimistic_lock_conflict`, `422` OAuth scope/audience mismatch; `AUTH_EXPIRED_REAUTH_REQUIRED` surfaces a warning `Token` with one-click re-authorization.
- **Confirmation:** Disabling or deleting a connection referenced by active `Model Deployments` opens an `AlertDialog` enumerating impacted `Routes`.
- **Audit:** `secret.created`, `secret.deleted`, `connection.created`, `connection.updated`, `connection.oauth_authorized`, `connection.disabled`, `connection.deleted`.
- **Success Feedback:** `Toast` reporting verified probe latency; write-only input buffer cleared immediately from DOM/memory.
- **Accessibility & Responsive:** `Table` (`table-page`) + focus-trapped `Drawer`; `375px` mobile view preserves one-tap connection disablement.

#### `SCR-05`: Model Catalog & Deployments Matrix (`/models`)
- **Objective:** Govern bindings from `Provider Connections` to physical `Model Deployments` (with `Data Region`, `idle_chunk_timeout_ms`, and integer `nano-USD` rate cards) while giving engineers a transparent `snake_case` capability lookup directory (`UJ-04`).
- **Actor & RBAC:** Create/Edit/Delete `Model` or `Deployment`: `ORG_OWNER`, `TEAM_ADMIN`; Browse capability matrix: `DEVELOPER`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/models` (`?env_id=...`), `POST /admin/v1/models`, `GET /admin/v1/deployments` (`?env_id=...`), `POST /admin/v1/deployments`, `PATCH /admin/v1/deployments/{deploymentId}` (`If-Match`), `DELETE /admin/v1/deployments/{deploymentId}` (`If-Match`).
- **Actions:** Multi-facet filtering (`Capability`, `Assurance Tier`, `Data Region`, `Health SM-04`); register `Logical Model` or `Model Deployment`; configure integer `nano-USD` token and media unit rates; transition `CIRCUIT_OPEN_UNHEALTHY` deployments to `DISABLED` when needed (`SM-04`); copy canonical `Model Slug`.
- **Validations:** All unit prices are non-negative integers (`nano-USD`); self-hosted zero-cost models require checking `self_hosted_zero_rate = true`; `data_region` must be `EU | US | APAC | ON_PREM`.
- **Errors:** Explicit callouts on `DECLARED`-only capabilities (`UX-GR-02`); `409 optimistic_lock_conflict`.
- **Confirmation:** Disabling or deleting the last healthy `Deployment` in an active `Route` opens an `AlertDialog`.
- **Audit:** `model.created`, `deployment.created`, `deployment.updated`, `deployment.deleted`.
- **Success Feedback:** Save `Toast` with in-place row update preserving scroll offset.
- **Accessibility & Responsive:** Built on `table-filter` with `useTableColumnSettingsState`.

---

### Group 2: Extension Ecosystem, Routing & MCP/A2A Planes (`SCR-06` .. `SCR-10`)

#### `SCR-06`: Extension Marketplace & Custom Source Installer (`/extensions/marketplace`)
- **Objective:** Browse signed Marketplace extensions or stage `.tpkg`, SHA-pinned Git, or digest-pinned OCI packages with mandatory pre-activation permission inspection (`UJ-02`, `UX-GR-04`).
- **Actor & RBAC:** Browse catalog: all admin roles; Stage installation: `OPERATOR`; Independent sign-off when `Four-Eyes` is enabled: `SECURITY_REVIEWER`.
- **Data Contracts:** `GET /admin/v1/marketplace/catalog`, `POST /admin/v1/extensions/install`, `GET /admin/v1/extensions/{installationId}/permission-diff`, `POST /admin/v1/extensions/{installationId}/approve-permissions` (`If-Match`).
- **Actions:** Filter by capability type and `origin_trust_tier` (`official`, `verified`, `unverified_external`) + `enterprise_signed`; stage package via `source_uri` + `expected_sha256`; inspect full permission sheet; approve into `INSTALLED_DISABLED` or `ENABLED_ACTIVE` (`SM-01`).
- **Validations:** `expected_sha256` matches `^[a-f0-9]{64}$`; enforces reserved namespace regex `^@tanpopo(-[a-z0-9]+)?/` (`CR-03`); rejects mutable Git refs (`CT-25`), wildcard `*` hosts, and Tier-2 memory quotas `> 16 MiB`; `unverified_external` acknowledgment clarifies non-waiver of `DEC-004` / `TP-BR-0301`.
- **Errors:** `400 hash_mismatch`, `400 contract_version_incompatible`, `400 mutable_git_ref_forbidden`, `403 reserved_namespace_spoofing`, `403 self_approval_forbidden`.
- **Confirmation:** Modal **Permission Inspection Sheet** `Dialog` keeps **`"Approve & Activate"`** locked until `Origin`, `Trust Tier`, `Allowed Hosts`, `Secret Slots`, and `Quotas` are rendered (`UX-AC-03`).
- **Audit:** `extension.staged`, `extension.permissions_approved`, `extension.install_rejected`.
- **Success Feedback:** `Toast` confirming `tanpopo.lock` update and hot activation with zero Core restart.
- **Accessibility & Responsive:** Built on `table-filter` + focus-trapped `Dialog`.

#### `SCR-07`: Installed Plugins, Org Policies, Lockfile Diff & Rollback (`/extensions/installed`, `/extensions/lockfile`)
- **Objective:** Govern installed extensions (`SM-01`), configure per-Organization enablement (`org_extension_policies` — `CR-04`), review or discard permission-escalating upgrades (`UPDATING_PENDING_RECONSENT` — `UJ-07`), execute `< 5s` (`TARGET / UNVERIFIED`) version rollbacks or emergency `QUARANTINE`, and export `tanpopo.lock`.
- **Actor & RBAC:** `OPERATOR` (instance install/upgrade/rollback/quarantine/uninstall), `ORG_OWNER` (`PUT /admin/v1/organizations/{orgId}/extension-policies`), `SECURITY_REVIEWER` (re-consent escalated permissions).
- **Data Contracts:** `GET /admin/v1/extensions/installations`, `GET /admin/v1/extensions/{installationId}/permission-diff`, `POST /admin/v1/extensions/{installationId}/upgrade` (`If-Match`), `POST /admin/v1/extensions/{installationId}/approve-permissions` (`If-Match`), `POST /admin/v1/extensions/{installationId}/rollback` (`If-Match`), `DELETE /admin/v1/extensions/{installationId}` (`If-Match`), `GET /admin/v1/extensions/lockfile`, `PUT /admin/v1/organizations/{orgId}/extension-policies` (`If-Match`), `POST /admin/v1/kill-switches` (`target_level = EXTENSION`).
- **Actions:** Stage upgrade; inspect **Side-by-Side Permission Diff** (`+` / `-` glyphs) or discard staged candidate (`DELETE`); trigger `Rollback`; configure org `min_plugin_trust` (`OFFICIAL_ONLY | VERIFIED_OR_ENTERPRISE_SIGNED | VERIFIED_ONLY | ANY_APPROVED`); trigger `Quarantine`; export `tanpopo.lock`.
- **Validations:** Blocks silent permission escalation (`CT-18`); blocks hard uninstall while active `Connections` reference the extension.
- **Errors:** `409 optimistic_lock_conflict`, `409 extension_referenced_by_active_connections`; warning `Token` on deprecated `N-1` contracts (`UX-GR-05`).
- **Confirmation:** `Rollback` and `Quarantine` require `AlertDialog` confirmation showing affected `Connections` and `Projects`.
- **Audit:** `extension.upgraded`, `extension.escalation_reconsented`, `extension.rolled_back`, `extension.uninstalled`, `org.extension_policy_updated`, `extension.quarantined`.
- **Success Feedback:** Immediate row state transition and blast-radius link upon quarantine.
- **Accessibility & Responsive:** Built on `table-collapsible` + diff `Dialog`.

#### `SCR-08`: Capability Routes & Dry-Run `Route Explain` Studio (`/routes`, `/routes/explain`)
- **Objective:** Author capability-aware routes (`PRIORITY_FAILOVER`, `WEIGHTED`, `LOWEST_COST` with `max_failover_hops <= 3`, `1 + 3 = 4` max attempts pre-first-chunk), enforce fail-closed data residency, and dry-run the 6-step routing decision tree (`UJ-05`).
- **Actor & RBAC:** Create/Edit/Delete `Route`: `ORG_OWNER`, `TEAM_ADMIN`; Execute `Route Explain`: all 6 roles (including `DEVELOPER` and `FINOPS_VIEWER`).
- **Data Contracts:** `GET /admin/v1/routes` (`?env_id=...`), `POST /admin/v1/routes`, `PUT /admin/v1/routes/{routeId}` (`If-Match`), `DELETE /admin/v1/routes/{routeId}` (`If-Match`), `POST /admin/v1/routes/explain`.
- **Actions:** Configure route targets and weights (explicit keyboard up/down priority buttons alongside pointer controls); configure `request_timeout_ms` (`<= 300000 ms` for reasoning models); run zero-side-effect `POST /admin/v1/routes/explain`.
- **Validations:** `WEIGHTED` targets sum to `100`; rejects routes whose targets all violate inherited `Data Residency` policy.
- **Errors:** `409 optimistic_lock_conflict` (opens side-by-side `Diff` modal — `UX-AC-11`), `422 invalid_weight_sum`; dry-run previews `503 no_compliant_deployment_available` or `400 capability_not_supported`.
- **Confirmation:** Editing a live `prod` route displays a change-summary dialog with a one-click `Route Explain` preview.
- **Audit:** `route.created`, `route.updated`, `route.deleted`.
- **Success Feedback:** `Toast` confirming `ConfigSnapshot` epoch update (`< 1s` single-node `TARGET / UNVERIFIED`).
- **Accessibility & Responsive:** Built on `form-two-column`; stacks cleanly below `1024px`.

#### `SCR-09`: Hierarchical Policies & Guardrails Configuration (`/policies`)
- **Objective:** Govern monotonic restrictive policy inheritance (`Instance ∩ Organization ∩ Project ∩ Environment`) across `RPM`/`TPM`/`Max Concurrent Streams`, `AllowedDataRegions`, `ModelAllowlist`, `MinPluginTrust`, and `Guardrail Hooks`.
- **Actor & RBAC:** `Instance` floor: `OPERATOR`; `Organization` ceiling: `ORG_OWNER`; `Project`/`Environment` tightening: `TEAM_ADMIN`; Read-only audit inspection: `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER`.
- **Data Contracts:** `GET /admin/v1/policies/effective` (`?project_id=...&env_id=...`), `PUT /admin/v1/policies/{policyId}` (`If-Match`).
- **Actions:** Inspect merged **Effective Policy** with tier provenance badges; tighten child ceilings; attach `FAIL_CLOSED` or `FAIL_OPEN` Guardrail extensions.
- **Validations:** Blocks any child attempt to relax parent `RPM`/`TPM`, `AllowedDataRegions`, or `MinPluginTrust` (`TP-BR-0501`).
- **Errors:** `422 policy_relaxation_forbidden`, `409 optimistic_lock_conflict` (`UX-AC-11`).
- **Confirmation:** Attaching blocking `FAIL_CLOSED` guardrails in `prod` requires `AlertDialog` confirmation.
- **Audit:** `policy.updated`, `policy.guardrail_attached`.
- **Success Feedback:** Immediately renders recalculated effective policy snapshot.
- **Accessibility & Responsive:** Built on `table-tree` / `settings-sidebar`.

#### `SCR-10`: MCP Governance Hub & A2A Agent Registry (`/mcp/*` — `v0.2`, `/a2a/*` — `v0.3`)
- **Objective:** Register Streamable HTTP / Sidecar `MCP Servers` (`MCP 2025-03-26`), pin RFC 8785 JCS `schema_hash` fingerprints per tool, assign `snake_case` risk tiers (`read_only`, `mutating`, `destructive` — `UJ-06`), and govern `A2A AgentEndpoints` (`v0.3`, `SM-09`, `max_delegation_hops <= 5`).
- **Actor & RBAC:** Register & sync: `ORG_OWNER`, `TEAM_ADMIN`; Approve/re-pin schemas: `ORG_OWNER`, `SECURITY_REVIEWER`; Browse permitted tools/agents: `DEVELOPER`.
- **Data Contracts:** `GET /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers/{serverId}/sync-tools`, `POST /admin/v1/mcp/tools/{toolId}/approve-schema` (`If-Match`), `GET /admin/v1/a2a/agents` (`v0.3`), `POST /admin/v1/a2a/agents` (`v0.3`).
- **Actions:** Sync `<server_slug>__<tool_name>` catalogs; inspect **Schema Diff** modal on `SCHEMA_DRIFT_LOCKED` tools; assign risk tiers (unclassified tools default fail-closed to `mutating`); register A2A agents (`v0.3`).
- **Validations:** Raw shell `stdio` commands are prohibited in UI/API; URLs must pass `SSRFGuard` (with explicit `allow_private_ip` governance for LAN servers); `max_delegation_hops` bounded `1..5`.
- **Errors:** `SCHEMA_DRIFT_LOCKED` token locks mutated tools immediately; `502` unreachable upstream MCP server.
- **Confirmation:** Downgrading a tool from `destructive` to `read_only` requires `AlertDialog` justification.
- **Audit:** `mcp.server_registered`, `mcp.tool_schema_pinned`, `mcp.schema_drift_detected`, `a2a.agent_registered`.
- **Success Feedback:** `Toast` reporting count of JCS `schema_hash`-pinned tools.
- **Accessibility & Responsive:** `TabList` (`MCP Servers & Tools` vs `A2A Agents`) over `table-grouped`.

---

### Group 3: Observability, FinOps, Access Control & Security Operations (`SCR-11` .. `SCR-16`)

#### `SCR-11`: Request Traces & Waterfall Inspector (`/observability/traces`)
- **Objective:** Search metadata request traces and inspect the 12-stage hot-path timeline (`waterfall_stages`) of any `request_id` while preserving zero-prompt-logging privacy by default (`UJ-08`, `TP-BR-0802`).
- **Actor & RBAC:** `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER`, `FINOPS_VIEWER`, `DEVELOPER` (scoped to authorized projects/keys; opt-in debug payload inspection restricted to `ORG_OWNER` / `SECURITY_REVIEWER`).
- **Data Contracts:** `GET /admin/v1/traces` (`?env_id=...&status=...&model=...&limit=50&cursor=...`), `GET /admin/v1/traces/{requestId}`, `GET /admin/v1/traces/{requestId}/debug-payload`, `POST /admin/v1/routes/explain` (replay routing metadata).
- **Actions:** Exact lookup by `request_id` (`Ctrl+K`); filter by HTTP status, `mid_stream_abort`, `Virtual Key`, `Route`, or `usage_accuracy`; inspect 12-stage waterfall; click **`"Simulate Metadata in Route Explain"`**.
- **Validations:** Omits prompt/completion bodies by default; opt-in `Project DEK`-encrypted debug payloads enforce `TTL <= 72h` and emit an immutable audit record on every read.
- **Errors:** Uniform `404 resource_not_found` across tenant boundaries (`AUTH-15` anti-IDOR oracle).
- **Confirmation:** Viewing opt-in debug payload requires acknowledging that `trace.debug_payload_viewed` will be logged.
- **Audit:** `trace.debug_payload_viewed`.
- **Success Feedback:** `Toast` on copying `request_id` or redacted diagnostic JSON.
- **Accessibility & Responsive:** Built on `table-filter` + `LayoutPanel` ( paired with millisecond text table for screen readers).

#### `SCR-12`: FinOps Budgets, Pre-Hold Monitor, Chargeback & Webhooks (`/finops/budgets`, `/finops/chargeback`)
- **Objective:** Configure hierarchical `BIGINT micro-USD` budgets, monitor settled spend + active `RESERVED_HOLD` / `STREAMING_ACCRUING` holds (`SM-07`), export formula-sanitized CSV/JSON chargeback reports partitioned by `EXACT` / `ESTIMATED` / `UNMETERED_FLAGGED`, and configure HMAC-signed budget webhooks (`v0.2`, `CR-09`).
- **Actor & RBAC:** Configure budgets/webhooks: `ORG_OWNER`, `TEAM_ADMIN`; Read/export FinOps reports: `FINOPS_VIEWER`, `SECURITY_REVIEWER`, `OPERATOR`, `DEVELOPER` (own key scope).
- **Data Contracts:** `GET /admin/v1/budgets`, `PUT /admin/v1/budgets/{budgetId}` (`If-Match`), `GET /admin/v1/usage/summary` (`?group_by=project|env|key|model&from=...&to=...`), `GET /admin/v1/usage/export` (`?format=csv|json`), `GET /admin/v1/webhooks` (`v0.2`), `POST /admin/v1/webhooks` (`v0.2`).
- **Actions:** Set daily/monthly ceilings and `50%`/`80%`/`100%` alerts; inspect `Negative Balance Carryover` (`BA-OQ-002`); export 3-column separated chargeback (`exact_micro_usd`, `estimated_micro_usd`, `unmetered_request_count`); register HMAC-signed alert webhooks (`v0.2`).
- **Validations:** Lossless integer `micro-USD` input conversion; CSV exports neutralize leading `=`, `+`, `-`, `@`, `\t`, `\r` characters (`TP-IMP-0025`).
- **Errors:** `409 optimistic_lock_conflict`; persistent warning `Banner` when `unmetered_request_count > 0` linking to `SCR-05`.
- **Confirmation:** Raising or removing hard ceilings triggers `AlertDialog` (and `Four-Eyes` approval when enabled).
- **Audit:** `budget.updated`, `ledger.exported`, `webhook.created`.
- **Success Feedback:** Live refresh of three-segment budget composition (`Settled` + `Reserved Hold` + `Remaining`).
- **Accessibility & Responsive:** Built on `dashboard-comparison` + `Table` with explicit text percentages and USD figures beneath every progress bar.

#### `SCR-13`: Virtual API Keys, Projects, Members & 6-Role RBAC (`/access/virtual-keys`, `/access/members`, `/access/roles`)
- **Objective:** Issue `Virtual Keys` hard-bound to **exactly 1 `Environment`** (`TP-BR-0103`), scope `allowed_models` and `allowed_mcp_tools`, run `Graceful Dual-Key Rotation` or `Emergency Security Revoke` (including from `QUOTA_PAUSED` per `SM-05`), and administer principals across the **6 canonical RBAC roles**.
- **Actor & RBAC:** `ORG_OWNER` (org-wide keys, projects, environments & roles), `TEAM_ADMIN` (project keys & members), `DEVELOPER` (self-service personal keys when permitted by project policy), `SECURITY_REVIEWER` (read-only audit & emergency key revoke).
- **Data Contracts:** `GET /admin/v1/projects`, `POST /admin/v1/projects`, `GET /admin/v1/projects/{projectId}/environments`, `POST /admin/v1/projects/{projectId}/environments`, `GET /admin/v1/virtual-keys` (`?env_id=...`), `POST /admin/v1/virtual-keys`, `POST /admin/v1/projects/{projectId}/virtual-keys`, `PATCH /admin/v1/virtual-keys/{keyId}` (`If-Match`), `POST /admin/v1/virtual-keys/{keyId}/rotate` (`If-Match`), `POST /admin/v1/virtual-keys/{keyId}/revoke` (`If-Match`), `GET /admin/v1/members`, `POST /admin/v1/members`, `PATCH /admin/v1/members/{memberId}/roles` (`If-Match`).
- **Actions:** Issue `Virtual Key` (`vk_tp_<env>_<32_base62>`); rotate with `grace_window_seconds` (`60s..259200s`); revoke via `GRACEFUL_DRAIN` or `EMERGENCY_ABORT_STREAMS`; manage members across `{OPERATOR, ORG_OWNER, TEAM_ADMIN, SECURITY_REVIEWER, FINOPS_VIEWER, DEVELOPER}`.
- **Validations:** Strictly 1 `env_id` per key; plain `SHA-256` key hashing; anti-self-privilege-escalation guard.
- **Errors:** `403 role_escalation_denied`, `409 optimistic_lock_conflict`.
- **Confirmation:** Show-once `Dialog` on key creation/rotation (`UX-GR-01`); explicit drain vs. stream-severing `AlertDialog` on revocation (`< 1s` single-node / `< 5s` cluster `TARGET / UNVERIFIED`).
- **Audit:** `project.created`, `environment.created`, `virtual_key.issued`, `virtual_key.updated`, `virtual_key.rotated`, `virtual_key.revoked`, `iam.member_role_changed`.
- **Success Feedback:** `Toast` reporting `severed_streams_count` upon emergency revocation.
- **Accessibility & Responsive:** Built on `table-page`; `375px` mobile view keeps **`Emergency Revoke`** prominent (`>= 44x44 CSS px`).

#### `SCR-14`: Security Center — Approval Queue, Audit Log & 4-Level Kill-Switches (`/security/*`)
- **Objective:** Unified security command center combining: (1) HITL approval queue for `MCP destructive` calls (5-tuple JCS `approval_binding_hash`) and `EXTENSION_ESCALATION` (`SM-06`), (2) `Per-Org + SYSTEM` JCS hash-chained `Audit Log` (`TP-FR-0704`), and (3) 4-level emergency `Kill-Switch` + `Blast-Radius` forensics (`OPS-04`).
- **Actor & RBAC:** Approval decisions: `SECURITY_REVIEWER`, `ORG_OWNER` (`Four-Eyes` enforced); Kill-Switch & Blast-Radius: `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `SECURITY_REVIEWER` (within authorized scope); Read/verify Audit Chain: `SECURITY_REVIEWER`, `ORG_OWNER`, `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/approvals` (`?status=PENDING`), `POST /admin/v1/approvals/{approvalId}/decide` (`If-Match`), `GET /admin/v1/audit-events` (`?actor=...&action=...&cursor=...`), `POST /admin/v1/audit-events/verify`, `POST /admin/v1/kill-switches`, `GET /admin/v1/blast-radius`, `GET /admin/v1/webhooks` (`v0.2`), `POST /admin/v1/webhooks` (`v0.2`).
- **Actions:** Inspect escaped plain-text JSON arguments and approve/reject pending requests; trigger 4-level Kill-Switch (`VIRTUAL_KEY | PROJECT | PROVIDER_CONNECTION | EXTENSION`); query `blast-radius`; verify `Per-Org + SYSTEM` audit hash chain.
- **Validations:** Blocks `Four-Eyes` self-approval (`403 self_approval_forbidden`); disables `Approve` if MCP tool `schema_hash` is drifted; requires `reason >= 5` characters.
- **Errors:** `409 approval_already_decided_or_expired`, `403 self_approval_forbidden`.
- **Confirmation:** `AlertDialog` requiring exact `confirm_slug` re-entry and `abort_active_streams` toggle.
- **Audit:** `approval.approved`, `approval.rejected`, `killswitch.triggered`, `audit.chain_verified`.
- **Success Feedback:** Cluster epoch propagation receipt + blast-radius inventory.
- **Accessibility & Responsive:** Built on stable `table-grouped` / `table-filter` (replaces WIP `incident-console` in `@astryxdesign/cli@0.7.13`); engineered for `<= 3`-tap `375px` mobile execution (`< 15s` `TARGET / UNVERIFIED`).

#### `SCR-15`: Developer Workspace — Playground, API Explorer, SDK Snippets & Smoke Diagnostics (`/devtools/*`)
- **Objective:** Enable engineers to compare streaming models interactively via short-lived cost-capped ephemeral Playground keys (`CR-10`), browse embedded offline OpenAPI `3.1.0` docs, copy SDK snippets, and run smoke diagnostics.
- **Actor & RBAC:** `DEVELOPER`, `TEAM_ADMIN`, `ORG_OWNER`, `OPERATOR` (`FINOPS_VIEWER` is explicitly barred from `POST /admin/v1/playground/sessions`; `support-bundle` export is restricted to `OPERATOR`).
- **Data Contracts:** `POST /admin/v1/playground/sessions`, `POST /v1/chat/completions`, `POST /v1/responses`, `POST /native/{dialect}/{upstreamPath}`, `POST /admin/v1/routes/explain`, `GET /admin/v1/openapi.json`, `POST /admin/v1/diagnostics/smoke-test`, `POST /admin/v1/diagnostics/support-bundle` (`OPERATOR`).
- **Actions:** Mint ephemeral session key (`TTL <= 15m`, micro-USD capped, auto-revoked on logout) and stream side-by-side comparisons on `/v1/*` (`100 tok/s` target `TARGET / UNVERIFIED`); inspect embedded OpenAPI explorer; copy OpenAI/Anthropic/Gemini/MCP snippets; export secret-scrubbed `support-bundle`.
- **Validations:** Zero server-side prompt persistence in `Playground` (ephemeral tab RAM only); `0` admin session cookies ever sent to `/v1/*` (`AUTH-07`).
- **Errors:** Side-by-side `CanonicalErrorEnvelope` inspector + remediation card.
- **Confirmation:** `support-bundle` previews manifest confirming `0` raw secrets (`OPS-AC-10`).
- **Audit:** `playground.session_created`, `diagnostics.support_bundle_generated`.
- **Success Feedback:** `Toast` confirming snippet copy or diagnostic pass.
- **Accessibility & Responsive:** Built on `ai-chat` (`/playground`) and `documentation-technical` (`/sdk-snippets`).

#### `SCR-16`: Instance Operations, Backups, Upgrades & Privacy Settings (`/ops/*`, `/settings/*`)
- **Objective:** Self-contained operations console for `OPERATOR` and `ORG_OWNER`: monitor node health and `ConfigSnapshot` epoch lag, run `Upgrade Preflight` checks, trigger `.tpop.enc` encrypted backups, configure retention `TTL`s, and execute `< 60s` (`TARGET / UNVERIFIED`) `Project` or `Organization` `Crypto-Shredding`.
- **Actor & RBAC:** `/ops/*` and `POST /admin/v1/organizations`: `OPERATOR` exclusively; `/settings/retention` and tenant deletion: `ORG_OWNER` / `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/ops/cluster-status`, `POST /admin/v1/ops/backups`, `POST /admin/v1/ops/upgrades/preflight`, `PUT /admin/v1/settings/retention` (`If-Match`), `GET /admin/v1/organizations`, `POST /admin/v1/organizations`, `DELETE /admin/v1/projects/{projectId}` (`If-Match`), `DELETE /admin/v1/organizations/{orgId}` (`If-Match`).
- **Actions:** Monitor per-node epoch sync lag and Wasm pool stats; trigger hot `.tpop.enc` backup; run `SM-08` contract-compatibility preflight; configure trace (`1..90d`), debug payload (`<= 72h`), and audit (`>= 365d`) retention; execute `Project DEK` or `Org DEK` crypto-shredding (`CR-06`).
- **Validations:** Rejects debug payload `TTL > 72h`; blocks lowering audit retention below compliance floor; drains/aborts active streams prior to `DEK` destruction.
- **Errors:** Lists `BLOCKED_INCOMPATIBLE` extensions on preflight; `409 optimistic_lock_conflict`.
- **Confirmation:** Highest-severity `AlertDialog` requiring exact `confirm_slug` entry before irreversible `Project DEK` / `Org DEK` destruction (`< 60s` `TARGET / UNVERIFIED`).
- **Audit:** `ops.backup_created`, `ops.upgrade_preflight_ran`, `settings.retention_updated`, `project.crypto_shredded`, `organization.crypto_shredded`.
- **Success Feedback:** Displays `.tpop.enc` `SHA-256` digest or `CryptoShredReceipt`.
- **Accessibility & Responsive:** Built on `settings-sidebar` + node health `Table` with explicit accessible text status tokens.
