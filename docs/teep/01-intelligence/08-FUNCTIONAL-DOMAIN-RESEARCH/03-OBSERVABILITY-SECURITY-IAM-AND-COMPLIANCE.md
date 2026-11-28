# 08.03 — Functional Domain Research: Observability, AI Security/Guardrails, IAM & Compliance/Privacy (`DOM-08` .. `DOM-11`, `DOM-23`)

- **Document ID:** `TEEP-01-DOC-08-03`
- **Domains Covered:** `DOM-08` (Observability), `DOM-09` (AI Security & Guardrails), `DOM-10` (Identity & Access Management), `DOM-11` (Governance & Compliance), `DOM-23` (Privacy Engineering, Data Sovereignty & Cryptographic Custody)
- **Candidate Capabilities:** `CAP-053`..`CAP-082`, `CAP-147`..`CAP-151`

---

## 1. `DOM-08` — Observability (`CAP-053` .. `CAP-059`)

- **RC9 Strengths (`KEEP`/`IMPROVE`):** Privacy-first `request_traces_meta` (`0` prompt bytes by default) with 12-stage waterfall timings (`CAP-053`), startup-enforced bounded Prometheus metric cardinality (`CAP-055`), and `<= 32 MiB` Drop-Oldest OTLP ring buffers (`CAP-056`).
- **Critical Gaps & Expansions:**
  1. **Server-Side Trace Filtering, Search & Export (`CAP-054`):** Fix `TracesPage.tsx`'s misleading client-only `error_code` filter (`<= 200` in-memory rows) by adding server-side indexed filtering on `status_code`, `error_code`, `virtual_key_id`, `route_slug`, `usage_accuracy`, `causal_root_id`, and latency percentiles + JSONL/CSV trace metadata export.
  2. **CLI/Docs Parity for Metrics & OTLP Egress (`CAP-055`, `CAP-056`, `PG-008`, `PG-009`):** Add `--metrics-scrape-token` and `--otlp-allowed-cidrs` flags, plus optional OTel `gen_ai.*` (`semantic-conventions-genai`) attribute mapping in the outward OTLP exporter (`Arize Phoenix`, `Langfuse`, `Grafana Tempo`, `Datadog`, `Dynatrace`) without coupling internal SQL columns to unstable `development` attribute names.
  3. **Wire `ttl.Cleaner` + `PartitionManager.DetachAndDropExpired` (`CAP-057`):** Start `ttl.Cleaner` in `cmd/tanpopo/serve.go`, implement `PUT /admin/v1/settings/retention` (`404` in RC9!) + dedicated Console Retention UI (`ORG_OWNER` + `OPERATOR`), and use instant PostgreSQL partition detachment for expired monthly `request_traces_meta` partitions.
  4. **Analytical Warehouse Offload (`CAP-058`) & Live `tanpopo traces tail` (`CAP-059`):** Async `TELEMETRY_EXPORTER` streaming to ClickHouse / S3 Parquet / Splunk HEC (`Zero-ClickHouse Day-1, Zero-Lock-In Day-100`), plus authenticated SSE/CLI real-time trace tailing.

---

## 2. `DOM-09` — AI Security and Guardrails (`CAP-060` .. `CAP-067`)

### 2.1 Closing RC9's Unwired `GuardrailHooks` Gap Safely (`[VERIFIED_FACT]`)
In RC9, `EffectivePolicy.GuardrailHooks` (`merge.go:226`) is never executed in `internal/ingress/*`, and `protectai/llm-guard` was archived upstream (`2026-07-09`). Tanpopo must make runtime guardrails real without repeating LiteLLM's `CVE-2026-40217` Python RCE or heavy multi-GB worker bloat:
1. **Live Multi-Stage Guardrail Execution Engine + Speculative Parallel Input Rails (`CAP-060`):** Wire `EvaluateGuardrail` across `PRE_REQUEST`, **`STREAM_WINDOW`** (bounded UTF-8 sliding holdback `128–512` chars before SSE client flush), `POST_RESPONSE`, `MCP_TOOL_CALL/RESULT`, and `A2A_TASK`. Support **speculative parallel input execution** (running input rails concurrently with upstream prefill while holding back byte #1 until `VERDICT_ALLOW`, masking guardrail TTFT latency!).
2. **Built-In Pure-Go/Wasm Deterministic DLP, Secret & Unicode Scanner (`CAP-061`):** Ship zero-dependency Tier-1/Tier-2 scanners for API keys/JWTs/PEMs, SSN/CC (`Luhn`)/IBAN/email/phone PII, and **`GlassWorm`-style invisible Unicode tag/bidirectional injection (`U+E0000..E007F`, `U+202A..202E`)** at `< 1 ms` CPU.
3. **Tier-3 / External Neural Safety Connectors (`CAP-062`):** First-class `GUARDRAIL_PROCESSOR` adapters for local ONNX (`GLiNER` PII, `PromptGuard 2`), NVIDIA NeMo Guardrails, AWS Bedrock `ApplyGuardrail`, Azure AI Content Safety / Prompt Shield, Google `ModelArmor`, Lakera Guard v2, and F5 AI Guardrails.
4. **Prompt-Injection, Indirect Tool-Output & Remote-MCP Firewall (`CAP-063`):** Scan untrusted RAG/MCP tool outputs before LLM re-ingestion, and block/govern vendor-hosted remote `mcp_servers` (`OpenAI`/`Anthropic`) that would otherwise bypass `/mcp` schema pinning ([`05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md`](../05-PROTOCOL-AND-TECHNOLOGY-INTELLIGENCE.md) §1.1).
5. **Crown-Jewel Isolation Kernel (`CAP-064`, `CAP-065`, `CAP-066` — `KEEP`/`IMPROVE`):** Preserve `BlindHostCredentialBroker` (`DEC-004` Cookie ban + chunk secret scrubber), `SSRFGuardDialer` (adding `RR-022` socket-level telemetry), and **single-TX `O(1)` 4-tier Kill-Switch & Quarantine** (fixing RC9's `TX1+TX2` split and missing Valkey `publishChange` in `extensions.go:817`).
6. **Vault, Cloud KMS BYOK & RLS-Safe KEK Rotation (`CAP-067`, `DEF-041`, `DEF-072`):** Wire `internal/security/vault/kms` (`AWS KMS`, `GCP KMS`, `Vault Transit` / `OpenBao`, plus Azure KV / PKCS#11 via `T3` `AUTH_PROVIDER`) into `serve.go`, reproduce and remediate `handleRetireMasterKEK`'s `[SOURCE_VERIFIED]` PG `FORCE RLS` visibility hazard (`ops_lifecycle.go:196`), fix `TANPOPO_MASTER_KEY_PREVIOUS` `<id>=<key>` AAD binding (`serve.go:359`), broadcast `KEY_RETIRE` cluster-wide, and add PostgreSQL support to `tanpopo vault rotate-kek`.

---

## 3. `DOM-10` — Identity and Access Management (`CAP-068` .. `CAP-075`)

1. **Cluster-Shared Persistent Admin Sessions, Local MFA/Passkeys & Account Self-Service (`CAP-068`):** Move Admin sessions from single-node Go RAM (`auth/local.go`) to encrypted SQL/Valkey (`HA`-safe), add **WebAuthn/Passkeys + TOTP MFA**, honor `requires_password_rotation`, add org-slug disambiguation on `LoginPage.tsx` + `O(1)` `iam_login_directory` blind HMAC lookup (`ARCH-OBS-01`), and decouple `org_id IS NULL` `SYSTEM` operators from `org_1` (`O(1)` `loadBindings`).
2. **Multi-Tenant OIDC & Hardened SAML 2.0 Enterprise SSO (`CAP-069`):** Wire per-org OIDC (`serve.go:1008`) + pure-Go `crewjam/saml` (`XSW 1–8` & XXE hardened) + Console SSO login & IdP group-to-role mapping UI.
3. **SCIM 2.0 Automated Provisioning & `< 5s` (`[HYPOTHESIS]`) Offboarding Kill-Chain (`CAP-070`):** Pure-Go `elimity-com/scim` revoking sessions, PATs, personal MCP OAuth grants, and active SSE streams atomically on `active=false` / `DELETE`.
4. **Two-Stage Policy-as-Code (`Native` + Candidate ABAC `[DESIGN_PROPOSAL — TEEP-03]`) & Policy Reset (`CAP-071`):** Pre-compiled ABAC evaluation (candidate `cedar-policy/cedar-go` `forbid > permit` ordering) + `GET /policies` & `DELETE /policies/{id}` reset-to-parent lifecycle.
5. **Secretless Workload Identity Federation & `/v1/sts/token` (`CAP-072`):** Inbound Data-Plane auth for K8s Projected SA JWTs, `go-spiffe/v2` `X.509-SVID`/`JWT-SVID`, AWS/GCP/Azure workload tokens, and GitHub/GitLab CI OIDC tokens (`0` static `vk_tp_*` secrets).
6. **Scoped Admin Service Accounts & PATs (`CAP-073`, `DEF-004`):** `sak_tp_*` & `pat_tp_*` wired into `TokenVerifier` with CIDR allowlists, expiry caps, and RBAC subset scoping.
7. **7-Tier Organizational Hierarchy & Delegated RBAC (`CAP-074`, `PG-001`, `PG-011`):** `enterprises` + `org_units` (`BU`/`DEPT`/`TEAM`) + full `GET`/`PATCH`/`DELETE`/suspend on Orgs, Projects, Environments, and Members + empty-default role picker (`PG-001`).
8. **Full Virtual Key Lifecycle & Attribution (`CAP-075`, `PG-002`, `PG-012`):** Full-field `PATCH` (limits, models, expiry, CIDR allowlists, FinOps tags), configurable rotation grace window in UI, `EXPIRED` status projection (`PG-002`), and explicit un-preselected plane checkboxes (`PG-012`).

---

## 4. `DOM-11` — Governance & Compliance (`CAP-076` .. `CAP-082`) and `DOM-23` — Privacy Engineering & Sovereign Custody (`CAP-147` .. `CAP-151`)

### 4.1 `DOM-11` — Governance & Compliance (`CAP-076` .. `CAP-082`)
- **Four-Eyes Dual Approval Workflow & Console UI (`CAP-076`):** Expose `/admin/v1/change-requests` in `SecurityPage.tsx`, persist `staged_by_principal_id` on `extension_installations` (`O(1)` anti-self-approval check replacing `ARCH-OBS-01` Loop #11), and add positive 2-operator PG E2E coverage.
- **Strict Data-Residency, BAA & ZDR Enforcement (`CAP-077`):** Block Bedrock `global.*` and Ollama `:cloud` tags when regional/on-prem residency is active; enforce `require_hipaa_baa` and `require_upstream_zdr` (`store: false`) on routes.
- **`O(1)` JCS Audit Chain Heads, Partition Seals, External Witness & `SYSTEM` UI (`CAP-078`, `PG-010`, `ARCH-OBS-01`):** Add `audit_chain_heads` (eliminating `O(M_partitions)` tip scans & `pg_advisory_xact_lock` bottleneck), `audit_partition_seals` (enabling partition archival without false `ALT-AUDIT-TAMPER`), hourly KMS/WORM chain-head witnesses, and `SYSTEM`/`ORG` selector in `SecurityPage.tsx`.
- **EU AI Act (`Art. 12/14/19/50`), ISO 42001, SOC 2 & HIPAA Evidence Packs (`CAP-079`, `CAP-080`):** Automated `X-Tanpopo-AI-Generated` provenance headers, `180d` retention profile floor, and 1-command signed `tanpopo admin compliance-pack export`.
- **Model License & Jurisdiction Policy Gate (`CAP-081`) & Fenced Tenant Erasure (`CAP-082`, `DEF-041`):** Block EU-restricted weights (e.g., Llama Guard 4 EU clause) for EU orgs; immediately fence VKeys/sessions on `DELETING` transition and run a background erasure finalizer.

### 4.2 `DOM-23` — Privacy Engineering, Data Sovereignty & Cryptographic Custody (`CAP-147` .. `CAP-151`)
1. **Recipient-Key-Encrypted (`RFC 9180 HPKE`) Failure Flight Recorder (`CAP-147`, `INN-08`):** Replaces uncalled `CreateDebugCaptureSession` with public-key-only `HPKE` sealing to offline Project Triage X25519 keys (`TTL <= 72h`, `0` private triage decryption keys on gateway nodes), alongside optional Vault-sealed interactive debug sessions for authorized reviewers.
2. **GDPR Art. 17 Per-Subject HMAC Salt-Shredding (`CAP-148`):** `subject_privacy_keys` pseudonymization (`psu_...`) enabling `< 15s` (`HYPOTHESIS / UNVERIFIED`) DSAR erasure with `0` broken JCS audit or settlement hashes.
3. **Ephemeral In-RAM Reversible PII Tokenization (`CAP-149`):** Redacts `<PERSON_01>` / `<IBAN_01>` pre-egress and rehydrates into SSE chunks in Host RAM (`0` disk storage).
4. **Dual-Custody `HPKE` WORM Legal-Hold Archiver (`CAP-150`):** `SEC 17a-4` / FINRA compliance sink encrypting directly to external WORM S3 Object Lock public keys.
5. **Sovereign Multi-Region Data-Plane Cells (`CAP-151`):** Zero cross-border prompt/trace replication with regional budget envelopes.
