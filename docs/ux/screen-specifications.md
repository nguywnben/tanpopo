# Screen Inventory, UI Contracts & UX Guardrails — Tanpopo

> **Detailed Screen Specification (Phase ASTRA A06 — `DRAFT / READY_FOR_REVIEW`)**
> - **Document ID:** `SPEC-UX-03` | **Version:** `0.1.0-step06-draft` | **Last Updated:** `2026-11-11`
> - **Authored By:** Lead Product Designer & Security UX Architect
> - **Governance Status:** **Proposed Design Specification (`PROPOSED — NO CODE`)** — tightly coupled with [`information-architecture.md`](./information-architecture.md), [`api-contracts.md`](../specifications/api-contracts.md) (`SPEC-API-01`), [`data-model.md`](../architecture/data-model.md), and [`accessibility-checklist.md`](../../.agents/references/accessibility-checklist.md).
> - **Requirements Traceability:** `TP-FR-0201`..`0904`, `TP-NFR-0402`, `TP-NFR-0404`, `TP-NFR-0701`, `TP-NFR-0702`, `TP-NFR-1001`, `TP-NFR-1002`, `DEC-004`.

---

## 1. Six Mandatory Experience Guardrails (`Mandatory UX Guardrails`: `UX-GR-01` .. `UX-GR-06`)

Every screen in the catalog below enforces six non-negotiable security, compliance, and technical-honesty guardrails without exception:

| Guardrail ID | Core Principle | Mandatory UI Behavior & Strict Prohibitions |
|---|---|---|
| **`UX-GR-01`**<br/>**Write-Only Secrets & One-Time Virtual Key Reveal** | Never re-display sensitive secret material once persisted. | - Every upstream API Key, Client Secret, and Webhook Secret field uses a one-way write-only input; once saved, only a trailing fingerprint (e.g., `sk-...9a2f`) and creation timestamp are shown — **strictly prohibiting any `"Show / Reveal Secret"` button** that fetches plaintext back from the server.<br/>- Upon issuing a `Virtual Key` (`vk_tp_...`), a modal `Dialog` displays the plaintext **strictly once at creation time** alongside the warning: *"Copy this key now; Tanpopo stores only its SHA-256 hash and cannot display it a second time."* |
| **`UX-GR-02`**<br/>**Honest Disclosure of Unsupported Capabilities** | Never silently drop parameters or promise `100%` universal parity. | - Across `Model Catalog`, `Route Builder`, and `Playground`, every capability (`strict_json_schema`, `audio_input`, `reasoning`, `prompt_caching`) carries its empirical assurance badge (`VERIFIED` / `TESTED` / `DECLARED` / `UNSUPPORTED`).<br/>- Whenever a feature operates losslessly only via native dialect passthrough (`/native/{dialect}/...`), the UI states that explicitly and offers a one-click toggle to the `Native Passthrough` snippet rather than silently stripping request fields. |
| **`UX-GR-03`**<br/>**Strict Prohibition of `"Official / Supported"` Labels on Consumer Subscriptions** | Enforce the legal and terms-of-service boundary of `DEC-004` & `ADR-0005`. | - Strictly **never** attach `Official`, `Verified`, or `Supported` badges — nor provide instructions — for turning individual consumer subscriptions (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Google Antigravity`, etc.) into shared multi-user API gateways.<br/>- The `Provider Connection` form exposes strictly two transparent billing postures: `Commercial API Billing` and `Self-Hosted Infrastructure`; it rejects pasted browser cookies or unauthorized personal session tokens. |
| **`UX-GR-04`**<br/>**Never Hide Third-Party Risk Behind a Single Install Button** | Surface permissions, publisher identity, and origin prior to any activation. | - Across `Extension Marketplace` listings and custom package installers, the primary call-to-action is always **`"Inspect Permissions & Trust..."`** (never a silent one-click install-and-enable button).<br/>- The mandatory inspection sheet renders `Publisher Identity`, `Origin`, `Trust Tier` (`Unverified External` carries its own distinct warning icon), `Allowed Hosts`, and `Secret Slots` before the activation confirmation button unlocks. |
| **`UX-GR-05`**<br/>**Proactive, Context-Timely Operational Warnings** | Surface data-loss, scale-ceiling, and billing-drift hazards before impact. | The `AppShell` system `Banner` or contextual warning callout triggers automatically whenever any of the following conditions are detected:<br/>1. Unacknowledged backup of `TANPOPO_MASTER_KEY` in a production deployment;<br/>2. Single-node SQLite mode exceeding recommended operating thresholds (`> 100 RPS` sustained or WAL file `> 512 MiB`) → prompting migration to PostgreSQL;<br/>3. Data Plane cluster node revocation sync lag exceeding `> 5 seconds`;<br/>4. Requests settled as `UNMETERED_FLAGGED` due to missing model rate cards;<br/>5. Active extensions targeting a deprecated `N-1` contract version approaching end-of-support. |
| **`UX-GR-06`**<br/>**Optimistic Concurrency Control & Anti-Fat-Finger Destructive Gates** | Prevent silent overwrites across concurrent administrators and accidental blast-radius actions. | - Every mutating configuration form (`PATCH`/`PUT`/`POST :approve`) attaches `If-Match: "<etag>"`; on `409 version_conflict`, the UI renders a side-by-side `Diff` rather than discarding the operator's draft or blindly overwriting.<br/>- High-blast-radius destructive operations (`Quarantine Extension`, `Freeze Project`, `Delete Organization`) invoke an `AlertDialog` requiring exact `slug` re-entry and a mandatory audit reason (`>= 5` characters). |

---

## 2. Shared Accessibility (`WCAG 2.2 AA`) & Multi-Viewport Responsive Contract (`Responsive Contract`)

Applied uniformly across all **16 screens (`SCR-01`..`SCR-16`)** specified below (`TP-NFR-0701`, `TP-NFR-0702`, [`accessibility-checklist.md`](../../.agents/references/accessibility-checklist.md)):
1. **Keyboard & Screen Reader Accessibility (`WCAG 2.2 AA`):**
   - `AppShell` provides a built-in `Skip to main content` link at the first `Tab` stop, a `banner` landmark, two `nav` landmarks with distinct `label` attributes, and exactly one level-1 heading (`Heading level={1}`) inside `main`.
   - Minimum contrast ratio `>= 4.5:1` for body text and `>= 3:1` for large text and interactive component boundaries across both Light and Dark modes of the `neutral` theme.
   - **Never rely on color alone:** Every status `Token` pairs a distinct `icon` with a concise text label (e.g., checkmark icon + `Healthy`, warning triangle + `Degraded`, shield/prohibition icon + `Quarantined`).
   - All asynchronous UI state changes (zero-result filter updates, copied key confirmation, successful saves, form validation errors) announce via `useAnnounce` (`polite` for status updates, `assertive` for errors and urgent alerts).
   - Every `Dialog` and `AlertDialog` traps focus while open, dismisses on `Escape`, and restores focus to its triggering control upon close.
2. **Three-Tier Breakpoint Contract (`Responsive Contract`):**
   - **Desktop (`>= 1280px`):** Displays all 3 workspace regions simultaneously (`SideNav 256px` + `LayoutContent fill` + `LayoutPanel 380px` when inspecting a selected row).
   - **Tablet / Narrow Laptop (`768px – 1023px`):** Retains `SideNav` (collapsible) while converting the `LayoutPanel` detail inspector into a modal `Dialog` or scrim-backed `Drawer` (`useMediaQuery('(max-width: 1024px)')`).
   - **Mobile (`375px – 767px`):** `SideNav` collapses automatically into `AppShell`'s mobile navigation drawer (`mobileNav={{ breakpoint: 'md' }}`), KPI grids stack cleanly to a single column (`Grid columns={{ minWidth: 240, max: 4 }}`), and interactive touch targets enforce a `44x44 CSS px` minimum. Multi-column configuration tables allow horizontal scrolling with a sticky leading identifier column, while the **4 emergency on-call workflows (`SCR-02`, `SCR-13`, `SCR-14`, `SCR-16`: inspecting active alerts, revoking a `Virtual Key`, approving/rejecting `destructive` calls, and triggering a `Kill-Switch`)** provide full-width action controls completable one-handed on a phone in under `15 seconds`.

---

## 3. Detailed Specifications for All 16 Canonical Screens (`SCR-01` .. `SCR-16`)

### Group 1: Bootstrap, System Posture & Model Gateway (`SCR-01` .. `SCR-05`)

#### `SCR-01`: First-Run Setup Wizard & Admin Login (`/setup`, `/login`)
- **Objective:** Securely transfer instance ownership on first boot via a one-time `Bootstrap Token` and guide the operator to their first successful inference request in under `3 minutes` (`UJ-01`), or authenticate daily administrative sessions (local password / OIDC SSO).
- **Actor & RBAC:** `Personal Admin` / `OPERATOR` (first run at `/setup`); all Console roles at `/login`.
- **Data Contracts:** `POST /admin/v1/setup:claim-bootstrap`, `POST /admin/v1/connections`, `POST /admin/v1/connections/{id}:validate`, `POST /admin/v1/virtual-keys`, `POST /v1/chat/completions` (first SSE stream smoke test), `POST /admin/v1/auth/login`.
- **Actions:** Verify bootstrap token; create initial `ORG_OWNER` account; acknowledge `TANPOPO_MASTER_KEY` backup; connect an official commercial API key or local `Ollama`/`vLLM` endpoint; issue the first `Virtual Key`; fire a live streaming test request.
- **Validations:** Valid, unexpired `Bootstrap Token`; administrator password `>= 12` characters; valid local model URL blocked from targeting cloud metadata addresses (`169.254.169.254`).
- **Errors:** `401 bootstrap_token_expired_or_used` (displays CLI reset command), `422 upstream_auth_failed`, `429 login_rate_limited`.
- **Confirmation:** Mandatory checkbox confirming the newly generated `Virtual Key` has been stored before dismissing the key-issuance dialog (`UX-GR-01`).
- **Audit:** `system.bootstrap.claimed`, `iam.owner.created`, `connection.created`, `virtual_key.issued`.
- **Success Feedback:** Step completion indicator with summary card displaying `request_id`, first-call `TTFT` latency, and a primary button entering `/overview`.
- **Accessibility & Responsive:** Built on `form-wizard-vertical` (step indicator pivots horizontally below `768px`); `/login` uses centered `login-sso` with automatic initial focus on the first input field.

#### `SCR-02`: Overview & System Posture Dashboard (`/overview`)
- **Objective:** Deliver an immediate `5-second` posture snapshot covering request throughput, error rate, `p95` latency, `micro-USD` budget burn rate, `Model Deployment` health, and actionable alerts requiring attention.
- **Actor & RBAC:** All roles (telemetry automatically filtered to the caller's `RBAC` scope).
- **Data Contracts:** `GET /admin/v1/telemetry/overview?window=1h|24h|7d`, `GET /admin/v1/deployments?status=unhealthy`, `GET /admin/v1/alerts/active`.
- **Actions:** Switch time window (`1h`, `24h`, `7d`, `30d`); deep-link directly from an alert item to its remediation workflow (`Re-authorize OAuth`, `Review Approval`, `Top-up Budget`, `Kill-Switch`).
- **Validations:** Requested query window falls within configured retention bounds.
- **Errors:** When metrics storage lags or external OTLP export is unreachable, renders local telemetry accompanied by a sync-lag warning `Banner`.
- **Confirmation:** None required for read-only window filtering.
- **Audit:** No audit event emitted for read-only dashboard queries.
- **Success Feedback:** Subtle freshness indicator (`Updated 5s ago`) and screen-reader summary announcement for newly arrived alerts via `useAnnounce`.
- **Accessibility & Responsive:** Built on `dashboard-alert-rail`; 4-tile KPI row uses `Grid columns={{ minWidth: 240, max: 4 }}` folding to 2 columns at `768px` and 1 column at `375px`, with the independently scrolling alert rail stacking cleanly on narrow viewports.

#### `SCR-03`: Provider Adapters Directory (`/providers/adapters`)
- **Objective:** Inspect all protocol-level `Provider Adapters` installed on the instance (both built-in official adapters and externally installed packages), compare supported capabilities, and launch `Provider Connection` creation.
- **Actor & RBAC:** Read: all administrative roles; Install new / configure instance-level settings: `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/provider-adapters?cursor=...&limit=50`, `GET /admin/v1/provider-adapters/{adapterId}`.
- **Actions:** Filter by vendor or wire protocol (`OpenAI-Compatible`, `Anthropic Messages`, `Gemini`, `Custom`); open side detail inspector to review supported authentication drivers (`API Key`, `Authorized OAuth 2.1`, `AWS SigV4`) and click **`"New Connection with this Adapter"`**.
- **Validations:** Connection creation button is enabled solely when the backing extension package is in `ENABLED_ACTIVE` state.
- **Errors:** If the backing extension is `QUARANTINED`, renders a red status token disabling connection creation with a direct link to the quarantine audit record.
- **Confirmation:** Not applicable (navigates to `SCR-04` or `SCR-06`).
- **Audit:** Not applicable for read-only inspection.
- **Success Feedback:** Smooth `LayoutPanel` detail expansion without full-page reload.
- **Accessibility & Responsive:** Tabular `Table` (`table-page`), with the detail panel converting to a modal `Dialog` at `<= 1024px`.

#### `SCR-04`: Provider Connections & Authorized OAuth Vault (`/providers/connections`)
- **Objective:** Manage live upstream credential profiles (`SM-02`, `SM-03`), seal write-only secrets into the `Write-Only Vault`, execute `Authorized OAuth 2.1 + PKCE` flows, validate live connectivity, and disable compromised or failing connections.
- **Actor & RBAC:** `ORG_OWNER` (`Org` scope), `TEAM_ADMIN` (`Project` scope), `REVIEWER` (metadata read-only, `0` secret read/write privileges).
- **Data Contracts:** `GET /admin/v1/connections`, `POST /admin/v1/connections`, `PATCH /admin/v1/connections/{id}` (`If-Match`), `POST /admin/v1/connections/{id}:validate`, `POST /admin/v1/connections/{id}/oauth:start`, `GET /admin/v1/oauth/callback`.
- **Actions:** Create connection (`COMMERCIAL_API` or `SELF_HOSTED_INFRA`); rotate secret credentials (write-only overwrite); initiate or re-authorize OAuth; run **`"Smoke Test Connection"`**; suspend or delete a connection.
- **Validations:** Billing mode must be `COMMERCIAL_API | SELF_HOSTED_INFRA` (`UX-GR-03`); custom endpoint hostnames must match the Adapter's `allowed_hosts` and pass SSRF egress filtering; secret inputs cannot be empty on initial creation.
- **Errors:** `400 ssrf_blocked_private_ip`, `409 version_conflict`, `422 oauth_scope_insufficient`; `AUTH_EXPIRED_REAUTH_REQUIRED` surfaces an orange `Token` with an inline re-authorization trigger.
- **Confirmation:** Deleting or disabling a connection referenced by active `Model Deployments` opens an `AlertDialog` enumerating the exact count of impacted `Routes`.
- **Audit:** `connection.created`, `connection.secret_rotated`, `connection.oauth_authorized`, `connection.disabled`, `connection.deleted`.
- **Success Feedback:** Confirmation `Toast` reporting verified round-trip latency (e.g., `Connection verified in 142 ms`); secret input buffer is immediately wiped from browser memory.
- **Accessibility & Responsive:** Dense `Table` layout with creation/rotation forms inside a focus-trapped `Drawer`; `375px` mobile view preserves one-tap emergency connection disablement.

#### `SCR-05`: Model Catalog & Deployments Matrix (`/models`)
- **Objective:** Govern bindings from `Provider Connections` to physical `Model Deployments` (with `Data Region` and `micro-USD` pricing rate cards) while giving application engineers a transparent capability lookup directory (`UJ-04`).
- **Actor & RBAC:** Create/Edit `Deployment`: `ORG_OWNER`, `TEAM_ADMIN`; Browse catalog & capability matrix: `DEVELOPER`, `REVIEWER`.
- **Data Contracts:** `GET /admin/v1/models?env_id=...`, `GET /admin/v1/deployments?env_id=...`, `POST /admin/v1/deployments`, `PATCH /admin/v1/deployments/{id}` (`If-Match`).
- **Actions:** Multi-facet filtering (`Capability`, `Assurance Level`, `Data Region`, `Health SM-04`); register new `Deployment`; override custom enterprise pricing rate cards; copy canonical `Model Slug` for API calls.
- **Validations:** Unit rates must be `>= 0` (for self-hosted models without electricity/GPU amortization rates, operators must explicitly check `Self-Hosted Zero Rate` or tag accordingly so traffic is never conflated with commercial `EXACT` spend); `Data Region` code must be explicitly declared (`EU`, `US`, `APAC`, `ON_PREM`).
- **Errors:** Explicit warnings on `UNSUPPORTED` or unverified `DECLARED`-only capabilities (`UX-GR-02`); real-time display of `CIRCUIT_OPEN` breaker posture.
- **Confirmation:** Demoting or detaching the last healthy `Deployment` backing an active `Route` requires confirmation via `AlertDialog`.
- **Audit:** `deployment.created`, `deployment.pricing_updated`, `deployment.state_changed`.
- **Success Feedback:** Save confirmation `Toast` with in-place table row refresh preserving scroll position.
- **Accessibility & Responsive:** Built on `table-filter` with column visibility managed via `useTableColumnSettingsState`; keyboard-scrollable capability comparison side panel.

---

### Group 2: Extension Ecosystem, Routing & MCP/A2A Planes (`SCR-06` .. `SCR-10`)

#### `SCR-06`: Extension Marketplace & Custom Source Installer (`/extensions/marketplace`)
- **Objective:** Browse curated extensions or stage packages from a pinned Git commit SHA, `.tpkg` URL, or Private OCI Registry with mandatory pre-activation inspection of publisher identity and requested permissions (`UJ-02`, `UX-GR-04`).
- **Actor & RBAC:** Browse catalog: all administrators; Stage & request installation: `OPERATOR`; Independent sign-off (when `Four-Eyes` is active): `REVIEWER`.
- **Data Contracts:** `GET /admin/v1/marketplace/catalog`, `POST /admin/v1/extensions:install` (stages as `PENDING_PERMISSION_APPROVAL`), `POST /admin/v1/extensions/{id}:approve-permissions` (`If-Match`).
- **Actions:** Filter by extension kind (`Provider Adapter`, `Auth Driver`, `Policy/Guardrail`, `Telemetry Exporter`) and `Trust Tier`; click **`"Inspect Permissions & Trust..."`** to open the inspection sheet; paste URI + 64-character `SHA-256` digest to stage an external package.
- **Validations:** `expected_sha256` must match `^[a-f0-9]{64}$`; rejects manifests missing permission declarations or requesting forbidden wildcard `*` network hosts; `Unverified External` packages require checking an explicit third-party risk acknowledgment box.
- **Errors:** `400 hash_mismatch`, `400 contract_version_incompatible`, `403 reserved_namespace_spoofing`; prominent red alert if the package matches a security `Revocation Advisory`.
- **Confirmation:** Modal **Permission Inspection Sheet** `Dialog` requires reviewing all network, secret, and resource capabilities before the **`"Approve & Activate"`** button unlocks (`UX-GR-04`).
- **Audit:** `extension.staged`, `extension.permissions_approved`, `extension.install_rejected`.
- **Success Feedback:** `Toast` confirming the package has been pinned in `tanpopo.lock` and activated hot without restarting Core, paired with a shortcut to create a `Connection`.
- **Accessibility & Responsive:** Structured table/list displaying 3 icon-paired `Token` badges (`Origin`, `Publisher Tier`, `Execution Tier`); full vertical scrolling inside the permission dialog on compact viewports.

#### `SCR-07`: Installed Plugins, Lockfile Diff & Rollback (`/extensions/installed`, `/extensions/lockfile`)
- **Objective:** Govern the lifecycle of installed instance extensions (`SM-01`), review upgrades paused for permission escalation (`UPDATING_PENDING_RECONSENT` — `UJ-07`), execute `< 5s` version rollbacks or emergency `QUARANTINE`, and export `tanpopo.lock` for GitOps pipelines.
- **Actor & RBAC:** `OPERATOR` (install/rollback/quarantine governance), `ORG_OWNER` (enable/disable within `Org` scope), `REVIEWER` (re-consent escalated permissions).
- **Data Contracts:** `GET /admin/v1/extensions/installations`, `POST /admin/v1/extensions/{id}:upgrade`, `POST /admin/v1/extensions/{id}:rollback` (`If-Match`), `GET /admin/v1/extensions/lockfile`, `POST /admin/v1/kill-switches:trigger` (`EXTENSION`).
- **Actions:** Check for updates; open the **Side-by-Side Permission Diff** dialog when an extension enters `UPDATING_PENDING_RECONSENT`; trigger `Rollback` to the previous pinned version; trigger emergency `Quarantine`; download or copy `tanpopo.lock`.
- **Validations:** Blocks activation of any permission-expanding upgrade until the permission diff is explicitly re-consented; prevents hard uninstallation while active `Connections` still reference the package (must disable or detach first).
- **Errors:** `409 version_conflict`; yellow warning badge for `Deprecated Contract N-1` (`UX-GR-05`).
- **Confirmation:** Both `Rollback` and `Quarantine` invoke an `AlertDialog` detailing the exact count of `Connections` and `Projects` that will be immediately suspended.
- **Audit:** `extension.upgraded`, `extension.escalation_reconsented`, `extension.rolled_back`, `extension.quarantined`.
- **Success Feedback:** Immediate row state update followed by a blast-radius summary report upon quarantine.
- **Accessibility & Responsive:** Tabular `Table` (`table-page`) / collapsible groups; permission diff view prefixes added/removed entries with explicit `+` / `-` glyphs alongside green/red highlights for color-blind clarity.

#### `SCR-08`: Capability Routes & Dry-Run `Route Explain` Studio (`/routes`, `/routes/explain`)
- **Objective:** Author capability-aware routes with pre-stream failover strategies (`Priority`, `Weighted`, `Lowest-Cost`), enforce fail-closed data residency constraints, and dry-run routing decision trees (`UJ-05`).
- **Actor & RBAC:** Create/Edit `Route`: `ORG_OWNER`, `TEAM_ADMIN`; Execute dry-run `Route Explain`: all roles (including `DEVELOPER`).
- **Data Contracts:** `GET /admin/v1/routes?env_id=...`, `POST /admin/v1/routes`, `PUT /admin/v1/routes/{id}` (`If-Match`), `POST /admin/v1/routes:explain`.
- **Actions:** Order priority targets or assign percentage traffic weights across `Model Deployments` (using accessible keyboard up/down controls alongside drag-and-drop); configure request timeouts and circuit-breaker thresholds; submit synthetic request metadata into `Route Explain` to inspect step-by-step candidate elimination.
- **Validations:** `Weighted` route weights must sum to `100%`; all target deployments must belong to the same `Environment` or permitted `Org` pool; surfaces a red error banner if every target violates an inherited `Data Residency` policy.
- **Errors:** `409 version_conflict`, `422 invalid_weight_sum`; dry-run simulator explicitly previews `503 no_compliant_deployment_available` when zero candidates survive filtering.
- **Confirmation:** Modifying a live high-traffic `prod` route presents a change-summary confirmation dialog with a one-click option to run `Route Explain` prior to committing.
- **Audit:** `route.created`, `route.updated`, `route.deleted`.
- **Success Feedback:** `Toast` confirming the new `ConfigSnapshot` epoch has propagated (`< 1s`).
- **Accessibility & Responsive:** Built on `form-two-column` on Desktop; every priority-reordering interaction provides explicit keyboard buttons (zero mouse-only drag-and-drop dependency).

#### `SCR-09`: Hierarchical Policies & Guardrails Configuration (`/policies`)
- **Objective:** Govern hierarchical policy inheritance (`Instance → Organization → Project → Environment`) across rate limits (`RPM`/`TPM`/`Concurrent Streams`), mandatory data residency, model allowlists, and content/PII `Guardrails`.
- **Actor & RBAC:** `Instance` floor: `OPERATOR`; `Org` ceiling: `ORG_OWNER`; `Project/Env` narrowing (strictly tighten-only; relaxing upstream ceilings is forbidden per `TP-BR-0501`): `TEAM_ADMIN`.
- **Data Contracts:** `GET /admin/v1/policies/effective?env_id=...`, `PUT /admin/v1/policies/{policyId}` (`If-Match`).
- **Actions:** Inspect the merged **Effective Policy** matrix (attributing the exact tier each rule inherits from); attach tighter project-level constraints; configure failure posture for PII/Guardrail plugins (mandatory `Fail-Closed` for regulated data vs `Fail-Open` with warning telemetry for auxiliary filters).
- **Validations:** Blocks both client-side and server-side attempts by a project admin to set an `RPM` ceiling above the `Org` cap or permit `US` routing when the `Org` enforces `EU Only`.
- **Errors:** `422 policy_relaxation_forbidden` (pinpointing the parent rule locked at the `Organization` tier), `409 version_conflict`.
- **Confirmation:** Activating broad blocking guardrails in `prod` requires `AlertDialog` confirmation.
- **Audit:** `policy.created`, `policy.updated`, `policy.guardrail_attached`.
- **Success Feedback:** Immediately renders the recalculated effective policy snapshot upon save.
- **Accessibility & Responsive:** Built on `table-tree` / `settings-sidebar`, conveying parent-child hierarchy via both structural indentation and explicit text tier tokens.

#### `SCR-10`: MCP Governance Hub & A2A Agent Registry (`/mcp/*` — `v0.2`, `/a2a/*` — `v0.3`)
- **Objective:** Register upstream `MCP Servers`, pin cryptographic `schema_hash` fingerprints per tool, assign risk classifications (`read_only` / `mutating` / `destructive` — `UJ-06`), and (in wave `v0.3`) govern the `A2A AgentCard` directory with delegation depth caps (`hop_count <= 5`).
- **Actor & RBAC:** Register & pin hashes: `ORG_OWNER`, `TEAM_ADMIN`; Re-approve drifted schemas: `ORG_OWNER` / `REVIEWER`; Browse permitted tools: `DEVELOPER`.
- **Data Contracts:** `GET /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers`, `POST /admin/v1/mcp/servers/{id}:sync-tools`, `POST /admin/v1/mcp/tools/{toolId}:approve-schema` (`If-Match`), `GET /admin/v1/a2a/agents` (`v0.3`).
- **Actions:** Synchronize tool catalogs from upstream MCP servers; assign fail-closed risk tiers (newly discovered unclassified tools never default to open `read_only`); inspect the `Schema Diff` modal whenever an upstream server mutates tool descriptions or parameters; enforce budget ceilings and `max_delegation_hops` on `A2A Agents`.
- **Validations:** Upstream MCP/A2A endpoint URLs must pass SSRF egress validation; `max_delegation_hops` must stay within `1..5`.
- **Errors:** Red `SCHEMA_DRIFT_LOCKED` token automatically locks any silently mutated tool until re-consented; `502 mcp_upstream_unreachable`.
- **Confirmation:** Downgrading a tool's classification from `destructive` to `read_only` (bypassing human-in-the-loop approval) requires `AlertDialog` confirmation with a mandatory audit justification.
- **Audit:** `mcp.server_registered`, `mcp.tool_schema_pinned`, `mcp.schema_drift_detected`, `mcp.risk_tier_changed`, `a2a.agent_registered`.
- **Success Feedback:** `Toast` reporting the exact count of tools cryptographically pinned by `schema_hash`.
- **Accessibility & Responsive:** Clean `TabList` separating `MCP Servers & Tools` from `A2A Agents`; dense `Table` rows without decorative card wrappers.

---

### Group 3: Observability, FinOps, Access Control & Security Operations (`SCR-11` .. `SCR-16`)

#### `SCR-11`: Request Traces & Waterfall Inspector (`/observability/traces`)
- **Objective:** Search real-time metadata request traces and dissect the 12-stage hot-path timeline of any `request_id` to diagnose failures or latency spikes instantly while preserving privacy-by-default (`UJ-08`, `TP-BR-0802`).
- **Actor & RBAC:** `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN`, `REVIEWER`, `DEVELOPER` (scoped strictly to authorized keys/projects).
- **Data Contracts:** `GET /admin/v1/traces?env_id=...&status=...&model=...&cursor=...`, `GET /admin/v1/traces/{requestId}`.
- **Actions:** Exact lookup by `request_id`; filter by HTTP status (`4xx`, `5xx`, `mid_stream_abort`), `Virtual Key`, `Route`, `Data Region`, or cost precision (`ESTIMATED`, `UNMETERED_FLAGGED`); open `LayoutPanel` to inspect the 12-stage `Waterfall` and click **`"Replay Metadata in Route Explain"`**.
- **Validations:** Enforces valid retention query windows; **omits prompt/completion body text by default** (if short-lived opt-in debug payload capture `TTL <= 72h` is active for a project, viewing payload text requires a dedicated permission and emits an immutable audit record).
- **Errors:** Never leaks existence of a `request_id` belonging to another tenant (`404 trace_not_found_in_scope`).
- **Confirmation:** Opening sensitive opt-in debug payloads displays an explicit notice that the access event will be recorded in the `Audit Log`.
- **Audit:** `trace.debug_payload_viewed` (emitted whenever opt-in debug payloads are inspected).
- **Success Feedback:** `Toast` confirmation upon copying `request_id` or redacted diagnostic metadata.
- **Accessibility & Responsive:** Built on `table-filter` paired with `LayoutPanel`; visual Waterfall bars are accompanied by a structured millisecond text breakdown table for screen readers.

#### `SCR-12`: FinOps Budgets, Pre-Hold Monitor & Chargeback Ledger (`/finops/budgets`, `/finops/chargeback`)
- **Objective:** Configure hard/soft budget ceilings (`BIGINT micro-USD`), monitor settled spend alongside in-flight stream reservations (`Active Pre-Request Holds` — `SM-07`), and export cost-allocation ledgers explicitly partitioned by precision (`EXACT` vs `ESTIMATED` vs `UNMETERED_FLAGGED`).
- **Actor & RBAC:** Configure/raise budget ceilings: `ORG_OWNER` (`Org`/`Project` scope), `TEAM_ADMIN` (sub-allocation within `Project`); Read reports: `REVIEWER`, `DEVELOPER` (own key scope).
- **Data Contracts:** `GET /admin/v1/budgets`, `PUT /admin/v1/budgets/{id}` (`If-Match`), `GET /admin/v1/ledger/chargeback?group_by=project|env|key|model&from=...&to=...`, `GET /admin/v1/ledger/export.csv`.
- **Actions:** Set daily/monthly ceilings and early-warning thresholds (`50%`, `80%`, `100%`); reconcile end-of-stream `Negative Balance Carryover` (`BA-OQ-002`); filter and export CSV/JSON `Chargeback` reports separating `Exact Commercial Spend`, `Estimated Spend`, and `Unmetered Requests Count` into 3 distinct columns.
- **Validations:** Child budget ceilings cannot exceed parent ceilings when hard hierarchical allocation is enabled; monetary inputs convert losslessly into integer `micro-USD` units without floating-point rounding drift.
- **Errors:** `409 version_conflict`; prominent warning `Banner` whenever any model accrues `UNMETERED_FLAGGED` traffic (missing rate card) linking directly to `SCR-05` for remediation (`UX-GR-05`).
- **Confirmation:** Raising budget caps above governance thresholds or removing a hard ceiling opens an `AlertDialog` (supporting `Four-Eyes` approval when dual financial control is enabled on the `Organization`).
- **Audit:** `budget.created`, `budget.limit_changed`, `budget.carryover_cleared`, `ledger.exported`.
- **Success Feedback:** Immediate update of the three-segment budget bar (`Settled` + `Reserved Hold` + `Remaining`).
- **Accessibility & Responsive:** Built on `dashboard-comparison` + `Table`; budget progress bars always print explicit percentage and `USD` figures in text directly underneath so comprehension never depends on bar color.

#### `SCR-13`: Virtual API Keys, Team Members & RBAC (`/access/virtual-keys`, `/access/members`, `/access/roles`)
- **Objective:** Issue `Virtual Keys` hard-bound to **exactly 1 `Environment`** (`TP-BR-0103`), scope permitted `Models`/`Routes`/`MCP Tools`, execute zero-downtime `Graceful Dual-Key Rotation` (`WF-10`) or stream-severing `Emergency Revoke` (`BA-OQ-003`), and administer members across the 5 RBAC roles.
- **Actor & RBAC:** `ORG_OWNER` (full `Org` control), `TEAM_ADMIN` (keys & members within `Project`), `DEVELOPER` (create/rotate personal keys only when permitted by project policy).
- **Data Contracts:** `GET /admin/v1/virtual-keys?env_id=...`, `POST /admin/v1/virtual-keys`, `POST /admin/v1/virtual-keys/{id}:rotate`, `POST /admin/v1/virtual-keys/{id}:revoke`, `GET /admin/v1/members`, `POST /admin/v1/members`.
- **Actions:** Issue `Virtual Key` (select single `Environment`, expiration, per-key `RPM`/`TPM`/spend ceiling, and `Models` & `MCP Tools` allowlists); click **`"Rotate Key (Grace Window)"`** with `1h..72h` overlap; click **`"Emergency Security Revoke"`** to sever active streams immediately; assign RBAC roles to team members.
- **Validations:** A `Virtual Key` cannot span multiple `Environments`; key expiration cannot exceed the organization's maximum security TTL; actors are blocked from `Self-Privilege Escalation`.
- **Errors:** `403 role_escalation_denied`, `409 version_conflict`.
- **Confirmation:**
  - Upon key creation or rotation: modal `Dialog` reveals the new key secret **strictly once** (`UX-GR-01`).
  - Upon revocation: `AlertDialog` requires explicitly selecting between **Mode 1: Graceful Drain (allow in-flight streams `<= 60s` to finish)** and **Mode 2: Emergency Security Revoke (abort all open SSE streams within `< 1s` single-node / `< 5s` cluster)**.
- **Audit:** `virtual_key.issued`, `virtual_key.rotated`, `virtual_key.revoked_graceful`, `virtual_key.revoked_emergency`, `iam.member_role_changed`.
- **Success Feedback:** Confirmation `Toast` reporting revocation completion alongside the exact count of severed in-flight streams.
- **Accessibility & Responsive:** On `375px` mobile viewports, key rows keep the **`Emergency Revoke`** control prominent with `>= 44px` touch target height so on-call responders can neutralize leaked credentials immediately from a phone.

#### `SCR-14`: Security Center — Approval Queue, Audit Log & 4-Level Kill-Switches (`/security/*`)
- **Objective:** Unified security command center combining three mission-critical capabilities: (1) Human-in-the-loop approval queue for `MCP destructive` calls and Plugin permission escalations (`SM-06`), (2) Tamper-evident hash-chained `Audit Log` (`TP-FR-0704`), and (3) the 4-level emergency `Kill-Switch` console (`OPS-04`).
- **Actor & RBAC:** Approval queue: `REVIEWER`, `ORG_OWNER`; Kill-Switch: `OPERATOR`, `ORG_OWNER`, `TEAM_ADMIN` (within authorized scope); Read & verify `Audit Log` chain integrity: `REVIEWER`, `ORG_OWNER`, `OPERATOR`.
- **Data Contracts:** `GET /admin/v1/approvals?status=PENDING`, `POST /admin/v1/approvals/{id}:decide` (`If-Match`), `GET /admin/v1/audit-events?actor=...&action=...&cursor=...`, `POST /admin/v1/audit-events:verify-chain`, `POST /admin/v1/kill-switches:trigger`.
- **Actions:** Approve or Reject pending requests (inspecting immutable `SHA-256`-pinned JSON arguments); trigger 1 of the 4 emergency kill-switches (`Revoke Key` / `Freeze Project` / `Disable Connection` / `Quarantine Extension`) and inspect the resulting `Blast Radius Report`; filter/export `Audit Log` entries and run cryptographic hash-chain verification.
- **Validations:** Requesters cannot self-approve their own submissions under `Four-Eyes` policy; rejection or Kill-Switch activation reasons require `>= 5` characters.
- **Errors:** `409 approval_already_decided_or_expired` (when TTL expires or another reviewer acted first), `403 self_approval_forbidden`.
- **Confirmation:** Every `Project`-, `Connection`-, or `Extension`-level `Kill-Switch` opens an `AlertDialog` requiring exact `slug` re-entry and an explicit toggle for severing active streams.
- **Audit:** `approval.approved`, `approval.rejected`, `killswitch.triggered`, `audit.chain_verified`.
- **Success Feedback:** Real-time cluster propagation confirmation (`Cluster Epoch #... acknowledged by all active nodes`) accompanied by the affected keys/projects inventory.
- **Accessibility & Responsive:** Built on `incident-console`; engineered first for one-handed `375px` mobile execution (`<= 3` taps from an alert notification to completed Kill-Switch activation).

#### `SCR-15`: Developer Workspace — Playground, API Explorer, SDK Snippets & Smoke Diagnostics (`/devtools/*`)
- **Objective:** Enable integration engineers to test models and routes interactively in the browser, browse embedded OpenAPI documentation, copy pre-populated gateway SDK snippets, and run `Virtual Key` smoke diagnostics.
- **Actor & RBAC:** `DEVELOPER`, `TEAM_ADMIN`, `ORG_OWNER` (`OPERATOR` additionally accesses the redacted `support-bundle` export tab).
- **Data Contracts:** `POST /v1/chat/completions` (or `/native/...` when testing inside `Playground` using a rate-capped ephemeral session key), `GET /admin/v1/openapi.json`, `POST /admin/v1/diagnostics:smoke-test`, `POST /admin/v1/diagnostics:support-bundle` (`OPERATOR`).
- **Actions:** Compare streaming responses side-by-side across 2 models/routes in `Playground` (displaying live `TTFT`, `tokens/sec`, settled `micro-USD` cost, and `Route Explain` trace); copy ready-to-run snippets (`OpenAI Python/TS/Go`, `Anthropic/Gemini Native`, `curl`, `MCP Client` config); download a `support-bundle` scrubbed of all secrets and PII.
- **Validations:** `Playground` never persists sensitive prompt history to server storage automatically (retaining state solely inside ephemeral browser tab memory and wiping immediately on tab close or `Clear`).
- **Errors:** Renders raw `CanonicalErrorEnvelope` payloads side-by-side with an actionable `Error Resolution Card`.
- **Confirmation:** None required for routine interactive testing; `support-bundle` export previews the exact manifest of collected files and confirms `0` raw secrets are included.
- **Audit:** `diagnostics.support_bundle_generated`.
- **Success Feedback:** `Toast` confirming SDK snippet copy to clipboard.
- **Accessibility & Responsive:** Built on `ai-chat` (with resizable metadata side panel) for `Playground` and `documentation-technical` for SDK snippets; stacks vertically cleanly on mobile viewports.

#### `SCR-16`: Instance Operations, Backups, Upgrades & Privacy Settings (`/ops/*`, `/settings/*`)
- **Objective:** Provide a self-contained operations console for `OPERATOR` and `ORG_OWNER`: inspect subsystem health (`/healthz`, `/readyz`, cluster epoch sync lag), run `Upgrade Preflight` compatibility gates, trigger encrypted backups, configure retention `TTL` windows, and execute tenant/project deletion (`Crypto-Shredding < 60s`).
- **Actor & RBAC:** `/ops/*`: `OPERATOR` (or `Personal Admin`) exclusively; `/settings/residency-retention`: `ORG_OWNER`.
- **Data Contracts:** `GET /admin/v1/ops/cluster-status`, `POST /admin/v1/ops/backups:trigger`, `POST /admin/v1/ops/upgrades:preflight`, `PUT /admin/v1/settings/retention` (`If-Match`), `DELETE /admin/v1/projects/{id}` / `DELETE /admin/v1/organizations/{id}`.
- **Actions:** Monitor per-node Data Plane posture (`ConfigSnapshot Epoch`, DB/Valkey/KMS connectivity, Wasm pool memory footprint); trigger hot encrypted backups; run contract-compatibility preflight checks ahead of Core version upgrades (`SM-08`); configure automatic purge `TTL` windows for technical traces (`7..30 days` default, up to `90 days`) and audit ledgers (`>= 365 days`); permanently close and crypto-shred a project or organization.
- **Validations:** Prohibits setting opt-in debug payload `TTL` above `72 hours`; blocks lowering audit ledger retention below the organization's compliance floor; verifies active streams are drained/aborted prior to destroying tenant encryption keys.
- **Errors:** Explicitly lists upgrade-blocking extensions (`BLOCKED_INCOMPATIBLE` — naming each package and mismatched contract version); surfaces low-disk-space warnings before initiating hot backups.
- **Confirmation:** Permanent deletion of a `Project` or `Organization` invokes the highest-severity `AlertDialog`: requiring exact entry of `DELETE <slug>` and stating clearly that the tenant's `DEK` encryption key will be irreversibly destroyed within `< 60 seconds`.
- **Audit:** `ops.backup_created`, `ops.upgrade_preflight_ran`, `settings.retention_updated`, `tenant.crypto_shredded`.
- **Success Feedback:** Completion receipt displaying the backup archive `SHA-256` digest or anonymized cryptographic key-destruction certificate.
- **Accessibility & Responsive:** Built on `settings-sidebar` + cluster node `Table`; exposes explicit text health labels per node to screen readers without relying on colored status dots.
