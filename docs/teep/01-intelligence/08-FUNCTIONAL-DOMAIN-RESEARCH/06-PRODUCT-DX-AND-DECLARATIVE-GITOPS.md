# 08.06 — Functional Domain Research: Product/Developer Experience & Declarative GitOps (`DOM-20`, `DOM-25`)

- **Document ID:** `TEEP-01-DOC-08-06`
- **Domains Covered:** `DOM-20` (Product & Developer Experience — `Founder Requirement 6.E`), `DOM-25` (GitOps, Terraform/IaC & Portable Declarative Solution Packs — `Founder Requirement 6.F`)
- **Candidate Capabilities:** `CAP-130`..`CAP-136`, `CAP-157`..`CAP-161`
- **Design Invariants Preserved (`ADR-0007`):** Astryx Neutral (`light` & `dark` parity, zero beige/ochre surfaces, zero nested cards, zero decorative colored badges), `0` external runtime/CDN requests, `<= 250 KB` gzip initial JS budget, `0` page-level horizontal scroll across `320`–`1920` px without `overflow-x: hidden/clip`, `WCAG 2.2 AA` (`0` `axe-core` violations), and 15-locale offline localization.

---

## 1. `DOM-20` — Product and Developer Experience (`CAP-130` .. `CAP-136`)

### 1.1 Eliminating Day-0 Onboarding & Day-2 Administration Friction
As proven in [`04-CURRENT-TANPOPO-PRODUCT-AUDIT.md`](../04-CURRENT-TANPOPO-PRODUCT-AUDIT.md) and [`07-BUSINESS-WORKFLOW-ANALYSIS.md`](../07-BUSINESS-WORKFLOW-ANALYSIS.md), Tanpopo RC9's visual foundation is clean and fast, while its **workflow completeness** suffers from 5 concrete gaps: (a) broken unsigned Day-0 onboarding (`403 org_trust_policy_denied` + skipped `/setup` quickstart), (b) `17` unexposed OpenAPI operations + `4` `404` backend routes, (c) create-only resource traps, (d) `100%` broken remote CLI (`DEF-004` `TokenVerifier: nil`) + `25+` undocumented `TANPOPO_*` env vars, and (e) raw English enum leaks (`PG-003`) & docs split-brain.

### 1.2 Seven Core Product & Developer Experience Capabilities (`CAP-130` .. `CAP-136`)
1. **Self-Activating `< 90s` (`[HYPOTHESIS]`) Guided Setup Wizard & Deterministic `tanpopo doctor --interactive` (`CAP-130`, `DEF-049`, `INN-09`):**
   - Populate `/setup` Step 1 (and headless SSH `tanpopo init`) directly from `builtin.Catalog` (`OpenAI`, `Anthropic`, `Gemini`, `Ollama`, `vLLM`/`OpenAI-Compatible`).
   - One atomic submission activates the signed adapter, seals the API key (or configures SSRF-safe local `Ollama`/`vLLM` CIDR consent while keeping `169.254.169.254` IMDS hard-blocked), probes the endpoint, auto-imports entitled models with signed `$ / 1M` rate cards (or `self_hosted_zero_rate: true` — fixing `bootstrap.go:509`'s latent `UNMETERED_FLAGGED` bug!), creates a default route, and opens `FirstKeyDialog` with live smoke-stream verification.
   - Extend `tanpopo doctor` with structured `CheckDiagnosticCode` decision trees mapped to 15-locale offline remediation commands (`0` external LLM dependency).
2. **100% Admin OpenAPI ↔ Web Console Lifecycle Parity (`CAP-131`):**
   - Expose all `17` unexposed Admin API operations in the Console: Organization Extension Trust Policy (`SCR-07`), Four-Eyes Change Requests (`SCR-14`), `SYSTEM` vs `ORG` Audit Log partition selector (`PG-010`), Retention TTL Settings (`/settings/residency-retention` decoupled from `/settings/webhooks` and unlocked for `ORG_OWNER`), Master KEK Retirement (`SCR-16`), OIDC/SAML SSO Login & OAuth Device Code flows, Debug/HPKE Capsule Viewer (`SCR-11`), MCP Tool Grant View/Revoke (`listMcpToolGrants`/`revokeMcpToolGrant`), and DevTools OpenAPI Explorer / Smoke Test / Support Bundle (`SCR-15`).
   - Eliminate all create-only traps across Logical Models, Deployment rate cards, Connections, Secrets, Virtual Keys, Policies, Budgets, Orgs, Projects, Environments, Members, MCP Servers, and A2A Agents, plus consistent cursor pagination (`> 200` rows) and server-side search.
3. **Enterprise Astryx Neutral Shell, Role-Tailored Developer Workspace, Scope Persistence & `403`-Safe UX (`CAP-132`, `DEF-020`, `PG-005`, `PG-013`, `PG-014`):**
   - Persist `(orgId, projectId, envId)` in URL/sessionStorage across reloads (`PG-005`), unify real-time shell badge invalidation across all security/MCP queues (`DEF-020`), enforce exclusive permission-denied states (`0` fabricated `"0 items"` empty states on `403`, `PG-014`), pair status labels with semantic icons or typographic contrast distinguishing `Active` vs `Disabled` (`WCAG 1.4.1`), provide clean role-tailored `DEVELOPER` self-service views without operator clutter, reconcile `AGENTS.md` CSS/font rules (`Google Sans` vs `Figtree`), and add `Cmd/Ctrl+K` `CommandPalette` navigation across tenants, routes, models, and docs.
4. **15-Language Offline Localization & AST Enum Gate (`CAP-133`, `PG-003`):**
   - Route all DTO enum values (`scope_tier`, `period`, `data_region`, `billing_mode`) through `i18n` helpers, extend `check-i18n.mjs` with an AST check rejecting raw SCREAMING_SNAKE_CASE enum rendering, and add 15-locale metadata support to `.tpkg` manifests.
5. **Complete Remote Read/Write CLI & `TANPOPO_*` Flag/Docs Parity (`CAP-134`, `DEF-004`, `PG-008`, `PG-009`):**
   - Wire `tanpopo login` (PAT / Service Account / OIDC Device Code) + full remote CLI CRUD (`key`, `provider`, `model`, `route`, `policy`, `budget`, `mcp`, `extension`, `killswitch`), expose first-class `tanpopo serve` flags for all `41` `TANPOPO_*` env vars, and eliminate stale checkpoint drift in `docs/README.md` and `AGENTS.md`.
6. **Multi-Dialect Studio Playground, SDK Snippets & Clean Session Teardown (`CAP-135`, `PG-006`):**
   - Support `/v1/chat/completions`, `/v1/responses`, `/v1/messages`, `/v1beta/models/*`, and MCP tool testing side-by-side in `PlaygroundPage`, include `LogicalModel` slugs in `RouteTypeahead`, generate copy-ready Python/TS/Go/`curl`/Claude Code/Cursor snippets, and **immediately revoke `pg-...` ephemeral Virtual Keys when clicking `End session` (`PG-006`)**.
7. **Obsolete / Ghost Configuration, Unused Font Dependency & Stale Governance Banner Cleanup (`CAP-136` — `REMOVE`):**
   - Align or remove `doctor.go:154`'s ghost `TANPOPO_ADMIN_LISTEN_ADDR` check (`OQ-14`), drop unused `@fontsource-variable/inter` from `web/console/package.json`, and clean up stale unit-checkpoint banners in `AGENTS.md` and `docs/README.md`.

---

## 2. `DOM-25` — GitOps, Terraform/IaC & Portable Declarative Solution Packs *(Discovered Domain — `CAP-157` .. `CAP-161`)*

1. **True Database-Reconciling Declarative GitOps (`tanpopo config export | diff | plan | apply`) (`CAP-157`):**
   - Replaces RC9's file-copy-only `gitops.Reconcile` (`reconciler.go:54-86`) with full idempotent Control-Plane database reconciliation across Organizations, `org_units`, Connections (`${VAULT:slot}` / `${ENV:...}` secret references — `0` plaintext secrets in Git), Logical Models, Deployments, Routes, Cedar/Structural Policies, Guardrails, Budgets, and MCP Servers, guarded by `ETag`/`If-Match` drift detection.
2. **Official OpenTofu & Terraform Provider (`terraform-provider-tanpopo`) (`CAP-158`):**
   - Generated from `openapi/admin-v1.yaml` and authenticated via scoped `sak_tp_*` Service Account keys (`CAP-073`) for enterprise infrastructure-as-code pipelines.
3. **Kubernetes Operator & CRD Controller (`tanpopo-operator`, Optional) (`CAP-159`):**
   - Translates Kubernetes-native `TanpopoRoute`, `TanpopoPolicy`, `TanpopoVirtualKey`, and `TanpopoMcpServer` CRDs into Control-Plane API reconciliations for platform teams standardized on ArgoCD / Flux — **without** making Kubernetes mandatory for standalone or VM users (avoiding Agent Router's K8s-only lock-in).
4. **Portable Signed `SOLUTION_PACK` Blueprints (`CAP-160`, `INN-04`):**
   - Signed `.tpkg` / OCI declarative architecture packs (e.g., `@tanpopo/eu-gdpr-support-gateway`, `@tanpopo/hipaa-clinical-rag`, `@tanpopo/coding-agent-mcp-firewall`) deployable via `tanpopo pack plan | apply` in `< 60s` (`HYPOTHESIS / UNVERIFIED`) across SQLite Mode A and PG16 Mode C.
5. **CI/CD Pull-Request What-If & Policy Gate Action (`CAP-161`, `INN-01`):**
   - GitHub Action / GitLab CI runner executing `tanpopo config plan --what-if 7d` on pull requests touching gateway configs, posting deterministic capability-breakage and `micro-USD` cost-delta comments before merge.
