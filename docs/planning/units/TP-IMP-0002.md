# `TP-IMP-0002` — Spec Closure Batch B: Extract `runtime.proto`, `blob_ref` Envelope, All `51` Missing Admin OpenAPI Endpoints & CLI/Metric Sync

| Property | Value |
|---|---|
| **Unit ID** | `TP-IMP-0002` |
| **Milestone / Epic** | `M0` (`Spec Closure & Spikes`) · `EP-00` |
| **Status** | `BLOCKED` *(Depends on `TP-IMP-0001` & Owner decisions on `CR-05, CR-10` — `GAP-01, 02, 03, 07`)* |
| **Size** | `L` (Extract canonical `proto/*`, `openapi/*` + synchronize `13` contract, UX, QA & ops spec files) |
| **Review Tier** | **`REQUIRES_STRONG_REVIEW`** *(Locks Wasm/Sidecar binary contracts, OpenAPI schemas & OOM prevention rules)* |
| **Technical Owner** | `Principal API/SDK Architect + UX/DX Lead` |

---

## 1. Intent
Make `100%` of Tanpopo's interface contracts machine-compilable and automatically verifiable without guesswork: extract and complete `proto/tanpopo/extension/v1/runtime.proto` + `openapi/{dataplane-v1,admin-v1}.yaml` in lockstep with Markdown docs, add out-of-band `blob_ref` multimodal streaming references to `NormalizedInferenceRequestEnvelope` outside Wasm linear memory, specify all `57` `/admin/v1/*` endpoints (including the `51` omitted in A08), and standardize on **1 single binary name `tanpopo`** (retiring `tanpopod`/`tanpopoctl` drift), 1 canonical CLI tree (`tanpopo db migrate-engine`), 1 bounded Prometheus metric label set, and local Vite `Figtree` font bundling for `Air-Gap` deployments.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - Machine-readable contracts (no executable code yet): `buf.yaml`, `proto/tanpopo/extension/v1/runtime.proto`, `openapi/dataplane-v1.yaml`, `openapi/admin-v1.yaml`.
  - Contract specifications: `docs/specifications/api-contracts.md`, `docs/specifications/extension-contract.md`, `docs/specifications/provider-adapter.md`, `docs/specifications/api-protocol-compatibility.md`, `docs/specifications/usage-billing-observability.md`, `docs/specifications/mcp-a2a-boundaries.md`.
  - UX, Operations & QA specifications requiring binary/flag/metric alignment: `docs/ux/screen-specifications.md`, `docs/ux/design-system-integration.md`, `docs/operations/developer-experience.md`, `docs/operations/installer-upgrades.md`, `docs/operations/monitoring-incidents.md`, `docs/quality/nonfunctional-slo.md`, `docs/quality/test-strategy.md`, `docs/reviews/unresolved-gaps.md`.
- **Out-of-Scope:** Physical SQL schemas and Valkey key structures (owned by `TP-IMP-0003`); zero executable `.go` or `.ts` code files.

## 3. Requirements, Dependencies & Design References
- **Requirements & Gaps Refs:** `GAP-02`, `GAP-03`, `GAP-07`, `GAP-10`, `GAP-12`, `GAP-13` (`AUD-001`, `002`, `005`, `009`, `015`..`022`, `027`, `028`); `TP-FR-0101`..`0105`, `0301`..`0305`, `0403`, `0901`..`0904`, `TP-NFR-1001`, `1002`.
- **Dependencies:** `TP-IMP-0001`.
- **Design / Audit Refs:** [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`, `CR-08`, `CR-10`), [`screen-specifications.md`](../../ux/screen-specifications.md) (`SCR-01`..`16`).

## 4. API, Wire & Data Contracts
- **Protobuf (`proto/tanpopo/extension/v1/runtime.proto`, `go_package = "github.com/nguywnben/tanpopo/gen/proto/extension/v1;extensionv1"`):**
  - Explicitly define `ExtractUsage(ExtractUsageInput) returns (NormalizedUsage)`, `TransformResponse(TransformResponseInput) returns (NormalizedInferenceResponse)`, `TransformStreamChunk`, `NormalizeError`, and `InspectNativeRequest(NativeRequestInput) returns (NativeRoutingHints)`.
  - Refactor `ValidateConnection` into a two-phase blind probe (`BuildValidationProbe` → `VerifyValidationResult`) and add `HostCryptoSignSpec` (`HMAC_SHA256`, `AWS_SIGV4`, `RSA_SHA256`); **hard-ban `Cookie`/`Set-Cookie` in `field_name` on both outbound injection and inbound response stripping**; remove `scoped_secret_unseal` from Tier-2 Wasm.
- **Multimodal `blob_ref` & JSON Envelope:** Add `tools`, `tool_choice`, `parallel_tool_calls`, `response_format`, `max_output_tokens`, `temperature`, `top_p`, `stop`, `reasoning_effort`, `dimensions`; replace inline base64 payloads `> 64 KiB` with `blob_ref://...` handles before crossing the `16 MiB` Wasm memory boundary; separate text token pre-hold estimation formulas from multimodal unit accounting (`media_usage_json`).
- **OpenAPI 3.1 (`openapi/dataplane-v1.yaml` & `openapi/admin-v1.yaml`):** Full schemas for `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`, `/v1/rerank`, `/native/{dialect}/{upstreamPath}`, `/mcp` (standard JSON-RPC responses for async HITL approval), `/a2a/*`, `/metrics`, `POST /admin/v1/playground/sessions`, `POST /admin/v1/routes/explain`, and all 57 CRUD endpoints backing `SCR-01..16`.
- **CLI, Metric, Error-Code & UI Normalization:**
  - Single unified binary **`tanpopo`** (`tanpopo serve --role=...`, `tanpopo db migrate-engine`, `tanpopo admin reset-local-owner`, `tanpopo admin verify-state`, `tanpopo budget inspect`, `tanpopo upgrade preflight`, `tanpopo bundle ...`); canonical extension archive **`.tpkg`** and encrypted backup archive **`.tpop.enc`**.
  - Bounded Prometheus metric label allowlist aligned with `monitoring-incidents.md`: permit only `{ org_id, project_id, adapter_id, logical_model, status_class, error_code, accuracy_tier }` (with an opt-out toggle for `org_id`/`project_id` on high-tenant clusters; **strictly ban** `request_id`, `virtual_key_id`, `principal_id`).
  - Configure Vite alias `@astryxdesign/theme-neutral/fonts.css` pointing to local `.woff2` `Figtree` font assets + enforce canonical semantic icon names from `@astryxdesign/core/Icon`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given `proto/tanpopo/extension/v1/runtime.proto` and `openapi/{dataplane-v1,admin-v1}.yaml`, When running contract linters and schema validators, Then `0` unresolved message/schema reference errors occur across both synchronous and streaming flows.
- **`AC-2`:** Given all 16 screens in `screen-specifications.md`, When cross-checking every route listed under `Data Contracts`, Then `100%` (`57/57`) match HTTP methods, parameter names, and schemas in `openapi/admin-v1.yaml` and `api-contracts.md`.
- **`AC-3`:** Given normative specification directories (`docs/specifications`, `docs/operations`, `docs/ux`, `docs/quality`, `docs/architecture`), When scanning binary names, DB migration commands, and `.tpkg`/`.tpop.enc` extensions, Then `100%` consistency holds with zero remaining `tanpopod` or `tanpopoctl` references.
- **`NEG-1`:** No execution path may pass a raw `32 MiB` multimodal byte array into Wasm's `16 MiB` linear memory.
- **`NEG-2`:** Never hand-edit generated `astryx-themes.js` or permit runtime requests to `https://fonts.googleapis.com`.
- **`NEG-3`:** Never permit `CredentialInjectInstruction` to inject `Cookie` headers to circumvent `DEC-004`.

## 6. Test Commands & Required Evidence
- Structural contract presence & legacy-binary-name drift check across normative specs (runnable via Node.js on every OS, plus `buf lint` in CI):
  - `node -e "const fs=require('fs'),p=require('path');['proto/tanpopo/extension/v1/runtime.proto','openapi/dataplane-v1.yaml','openapi/admin-v1.yaml'].forEach(f=>{if(!fs.existsSync(f))throw new Error('Missing '+f)});const dirs=['specifications','architecture','ux','operations','quality'];let bad=0;const walk=d=>fs.readdirSync(d,{withFileTypes:true}).forEach(e=>{const f=p.join(d,e.name);e.isDirectory()?walk(f):f.endsWith('.md')&&/\btanpopod\b|\btanpopoctl\b/.test(fs.readFileSync(f,'utf8'))&&(console.error('Legacy binary ref:',f),bad++)});dirs.forEach(d=>walk(p.join('docs',d)));process.exit(bad?1:0)"`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Strictly preserve *Zero Raw Secrets in Wasm Memory*, *No Outbound Cookie Injection*, and *Zero External Browser Requests in Air-Gap Mode*.
- **Rollback:** Atomic `git revert` on the contract/spec branch if schema conflicts arise with `TP-IMP-0003`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0001` is complete and Product Owner has approved `CR-05`, `CR-08`, and `CR-10`.
- **DoD:** `GAP-02`, `GAP-03`, `GAP-07`, `GAP-10`, `GAP-12`, and `GAP-13` closed in [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md); passes `REQUIRES_STRONG_REVIEW`.
