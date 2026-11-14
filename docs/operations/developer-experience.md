# Developer Experience: CLI, Management API, SDKs, Error Handling & Diagnostics — Tanpopo

> **Developer Experience & Integration Ecosystem Specification (Phase ASTRA A06 — `DRAFT / READY_FOR_REVIEW`)**
> - **Document ID:** `SPEC-OPS-04` | **Version:** `0.1.0-step06-draft` | **Last Updated:** `2026-11-11`
> - **Authored By:** Developer Experience (DX) Lead & SDK Architect
> - **Governance Status:** **Proposed Design Specification (`PROPOSED — NO CODE`)** — synchronized 100% with [`api-contracts.md`](../specifications/api-contracts.md) (`SPEC-API-01`), [`extension-contract.md`](../specifications/extension-contract.md) (`SPEC-EXT-01`), [`api-protocol-compatibility.md`](../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), and [`contract-test-matrix.md`](../quality/contract-test-matrix.md) (`SPEC-QA-01`).
> - **Requirements Traceability:** `OPS-07`, `OPS-09`, `TP-FR-0101`..`0105`, `TP-FR-0305`, `TP-FR-0604`, `TP-FR-0901`, `TP-FR-0903`, `TP-FR-0904`, `TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`.

---

## 1. Developer Experience Philosophy (`Zero-Lock-In & Self-Diagnosing DX`)

Tanpopo serves three distinct engineering personas with sharply different workflow expectations:
1. **AI Application Engineer (`API Consumer`):** Refuses proprietary heavyweight SDK lock-in; expects changing `base_url` and `api_key` in official OpenAI / Anthropic / Gemini / MCP SDKs to work immediately, and expects any failure envelope to pinpoint the exact fix within 10 seconds.
2. **Platform & DevOps Engineer (`Operator`):** Requires `100%` parity between Web Console actions, scriptable CLI commands (`--output json`), and `ETag`-guarded Management APIs suitable for Terraform and GitOps pipelines.
3. **Plugin Developer (`Extension Author`):** Scaffolds, locally contract-tests (`CT-01`..`24`), and packages a new `Provider Adapter` in under `30 minutes` using Tier-1 declarative manifests or the Tier-2 Wasm SDK without cloning or compiling Tanpopo Core.

---

## 2. `tanpopo` CLI Design (`Operator & Plugin Author CLI`)

### 2.1. Global CLI Conventions (`UNIX & Automation Friendly`)
- **Output Formatting:** Prints clean aligned human-readable tables by default (honoring `NO_COLOR=1`); pass `--output json` or `--output yaml` for deterministic machine-readable schemas consumable by `jq` and CI/CD pipelines; progress spinners and warnings always write to `stderr` so `stdout` JSON remains uncorrupted.
- **Standard Environment Variables:** `TANPOPO_URL` (Control Plane base URL), `TANPOPO_ADMIN_TOKEN` (CI service token), `TANPOPO_ORG` / `TANPOPO_PROJECT` / `TANPOPO_ENV`.
- **Deterministic Exit Codes:**
  - `0`: Complete success.
  - `2`: Invalid CLI flag syntax or malformed local input argument.
  - `10`: Authentication failure or insufficient `RBAC` permission (`401` / `403`).
  - `14`: Optimistic concurrency conflict (`409 version_conflict`) or expired approval token.
  - `22`: **Preflight / Contract / Hash Verification Gate Blocked** — e.g., extension `SHA-256` mismatch, unauthorized permission request, or incompatible target upgrade contract.
  - `30`: Upstream provider connectivity validation or `Smoke Test` failure.

### 2.2. Standardized `tanpopo` CLI Command Tree

```text
tanpopo
├── serve                       # Start gateway daemon (--role=all|data-plane|control-plane, --config)
├── doctor                      # Run 8 preflight checks across OS, network, vault & lockfile integrity
├── login / whoami              # Authenticate via OIDC Device/Browser flow or admin service token
├── key                         # Manage Virtual Keys (list, create, rotate --grace-period, revoke, test)
├── connection                  # Manage Provider Connections (list, create, oauth-login, test, disable)
├── model / route               # Inspect models & routing (route list, route apply, route explain)
├── policy / budget             # Sync hierarchical policies & inspect budget ceilings / active holds
├── approval                    # Approve or reject MCP destructive calls & permission escalations via CLI
├── killswitch                  # Trigger 4-level emergency kill-switches (--level, --target, --abort-streams)
├── plugin                      # Shared toolchain for Operators & Extension Authors:
│   ├── init <name> --tier 1|2  # Scaffold a Declarative Adapter (YAML) or Wasm SDK project
│   ├── validate                # Statically validate Manifest schema, network hosts & secret slots
│   ├── test                    # Run local contract test suite (CT-01..24) against mock harness
│   ├── pack / sign             # Build reproducible .tpkg archive & sign with Cosign
│   └── install / upgrade / rollback / quarantine / lock
├── bundle                      # Export/verify/import offline bundles for Air-Gapped deployments
├── backup / storage / vault    # Encrypted backups, SQLite->Postgres migration, Master KEK rotation
└── support-bundle              # Collect diagnostic archive with 100% automated secret & PII redaction
```

---

## 3. Management API & Copy-Pasteable SDK Integration Snippets (`SDK & Wire Snippets`)

### 3.1. Management API Ergonomics (`/admin/v1/*`)
- **Zero Blind Overwrites:** Every `GET` response on a mutable configuration resource returns `ETag: "v7"`. Subsequent `PUT`/`PATCH` mutations supply `If-Match: "v7"`, guaranteeing safety when multiple engineers or GitOps pipelines run concurrently.
- **Constant-Time Cursor Pagination:** Every high-cardinality collection uses `?limit=100&cursor=eyJ...` returning `{ "data": [...], "next_cursor": "...", "has_more": true }` (avoiding `OFFSET` degradation on deep pages).
- **Safe Retries:** Mutating `POST` endpoints for key issuance, connection creation, and task triggers honor the `Idempotency-Key` header.

### 3.2. Unified Inference Integration (Zero Custom SDK Required)

#### A. Python (Standard `openai` SDK — Swap Only `base_url` and `api_key`)
```python
from openai import OpenAI

client = OpenAI(
    base_url="https://ai-gateway.corp.example.com/v1",
    api_key="vk_tp_live_example_only_not_real",  # Virtual Key hard-bound to 1 Environment
    default_headers={
        "X-Tanpopo-Data-Region": "EU",           # Optional: enforce fail-closed data residency
    },
)

stream = client.chat.completions.create(
    model="prod-assistant-default",              # Target a smart Route or Logical Model slug
    messages=[{"role": "user", "content": "Summarize this week's operations report."}],
    stream=True,
)
for chunk in stream:
    if chunk.choices and chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

#### B. TypeScript / Node.js (Standard `openai` SDK + Idempotency Header)
```typescript
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'https://ai-gateway.corp.example.com/v1',
  apiKey: process.env.TANPOPO_VIRTUAL_KEY,
});

const response = await client.chat.completions.create(
  {
    model: 'claude-sonnet-4',
    messages: [{ role: 'user', content: 'Validate network configuration syntax.' }],
  },
  {
    headers: {
      'Idempotency-Key': 'job-check-20261111-001',
      'X-Tanpopo-Timeout-Ms': '30000',
    },
  },
);
```

#### C. Lossless Vendor-Specific Dialect Routing (`Lossless Native Passthrough`)
For deep vendor-native capabilities (such as signed `thinking` blocks or granular Anthropic / Gemini `cache_control`) while retaining full Tanpopo virtual-key scoping, `micro-USD` budget enforcement, and audit logging:
```python
import anthropic

client = anthropic.Anthropic(
    base_url="https://ai-gateway.corp.example.com/native/anthropic",
    api_key="vk_tp_live_example_only_not_real",  # Tanpopo authenticates Virtual Key & injects real credential in Blind Broker!
)

msg = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=2048,
    messages=[{"role": "user", "content": "Analyze distributed consensus trade-offs."}],
)
```

#### D. Governed MCP Gateway Client Configuration (`Dedicated MCP Plane` — Wave `v0.2`)
```json
{
  "mcpServers": {
    "tanpopo-governed-hub": {
      "type": "streamable-http",
      "url": "https://ai-gateway.corp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${TANPOPO_VIRTUAL_KEY}"
      }
    }
  }
}
```

#### E. Rapid CLI Smoke Diagnostics (`Smoke Testing Key, Connection & Route`)
```bash
# 1. Verify Virtual Key validity, remaining budget, and model scope (0 billable upstream calls):
tanpopo key test --key "$TANPOPO_VIRTUAL_KEY"

# 2. Validate live upstream connectivity from Tanpopo through the Blind Broker:
tanpopo connection test prod-vertex-eu-01

# 3. Dry-run routing decision tree directly in the terminal:
tanpopo route explain prod-assistant-default --capability vision,tools --region EU
```

---

## 4. Documentation Architecture, API Explorer & Structured Logs (`Docs, Explorer & Logs`)

### 4.1. Documentation Information Architecture (`Documentation IA`)
Both public documentation and the embedded offline manual are structured around **4 distinct reader journeys** (aligned with Astryx `documentation` & `documentation-technical` templates):
1. **Quickstart (`5 Minutes`):** Single-command launch → complete Setup Wizard → send first verified request.
2. **Application Developer Guide:** Unified API vs Native Passthrough, streaming/cancellation semantics, canonical error handling, MCP Client & A2A integration.
3. **Self-Host Operator & SRE Guide:** Linux/macOS/Windows/Docker/Helm deployment, TLS & Nginx SSE tuning, `Master KEK` backup custody, zero-downtime rolling upgrades, and the 7 incident runbooks.
4. **Extension Author SDK Reference:** Building a Tier-1 Declarative Adapter in 15 minutes, authoring Tier-2 Wasm Plugins, `Blind Broker` secret-slot rules, `tanpopo plugin test` contract harness, and Cosign signing for Marketplace / Private OCI registries.

### 4.2. Interactive Built-In API Explorer (`Built-in API Explorer`)
- `/devtools/api-explorer` in the Web Console renders the binary-embedded `OpenAPI 3.1` specification (`100%` air-gap functional), pre-filling the active gateway URL and enabling safe read-only calls and dry-run `Route Explain` simulations directly in-browser.

### 4.3. Secret-Safe Structured JSON Log Contract (`Structured JSON Log Contract`)
Every log line written to `stdout` is a flat single-line JSON object adhering strictly to `observability-and-instrumentation` standards (always carrying `request_id` and `entry_point`, zero unstructured string interpolation, and `0` secrets or PII):
```json
{
  "ts": "2026-11-11T16:45:12.304Z",
  "level": "WARN",
  "event": "gateway.routing.pre_stream_failover",
  "entry_point": "http_data_plane",
  "request_id": "req_01jcd8k9p2v4x7m1n6q8r0s2t4",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "org_id": "org_01jc...",
  "project_id": "prj_01jc...",
  "env_id": "env_prod",
  "route": "prod-assistant-default",
  "failed_deployment": "dep_openai_eu_01",
  "upstream_status": 429,
  "selected_fallback": "dep_vertex_eu_02",
  "overhead_ms": 1.84
}
```

---

## 5. Canonical Error Resolution Guide (`Canonical Error Resolution Guide`)

Every error emitted by Tanpopo uses the deterministic `CanonicalErrorEnvelope` carrying `request_id` and an accurate `retryable` boolean so client SDKs know immediately whether automated retry is safe:

| Error Code (`error.code`) | HTTP | `retryable` | Root Cause | Concrete Engineer / Administrator Remediation |
|---|---|---|---|---|
| `capability_not_supported` | `400` | `false` | Request uses a capability (e.g., strict `json_schema` or `audio`) unsupported by the target model/route. | Check `/models` capability assurance, target a capable `Route`, or switch to `/native/{dialect}/...` if supported only natively. |
| `virtual_key_invalid_or_revoked` | `401` | `false` | Virtual key is malformed, expired, or revoked. | Check `TANPOPO_VIRTUAL_KEY` or issue a replacement key bound to the right `Environment` at `SCR-13`. |
| `model_or_tool_not_in_key_scope` | `403` | `false` | Valid virtual key lacks allowlist permission for the requested model or MCP tool. | Ask `TEAM_ADMIN` to widen the key's allowlist at `SCR-13`, or inspect scope via `tanpopo key test`. |
| `schema_hash_drift_blocked` | `403` | `false` | Upstream MCP server mutated tool schema away from its pinned `schema_hash`. | Administrator opens `SCR-10 (/mcp/tools)`, inspects the Schema Diff, and re-pins if legitimate (`INC-06`). |
| `parameter_hash_mismatch` | `403` | `false` | Arguments supplied alongside a `destructive` approval token differ from the `Reviewer`-approved payload. | Replay the exact approved JSON argument object or submit a fresh approval request (`UJ-06`). |
| `version_conflict` | `409` | `true` *(after re-read)* | `If-Match` header carries a stale `ETag` superseded by a newer server write. | `GET` the latest resource and `ETag`, merge changes, and resubmit. |
| `project_budget_exceeded` | `429` | `false` *(short-term)* | Settled spend plus active pre-request holds reached the hard ceiling for the project or key. | Inspect `SCR-12 (/finops/budgets)` for runaway agent loops or raise the budget ceiling (`INC-01`). |
| `rate_limit_exceeded` | `429` | `true` *(honor `Retry-After`)* | Virtual key hit its configured `RPM` / `TPM` / `Max Concurrent Streams` cap on Tanpopo. | Respect `Retry-After` using `Exponential Backoff + Jitter`. |
| `no_compliant_deployment_available` | `503` | `true` *(with backoff)* | Every deployment satisfying both required capabilities and `Data Residency` is currently `CIRCUIT_OPEN` or disabled. | Run `tanpopo route explain` to distinguish `CIRCUIT_OPEN` targets from region-eliminated targets (`INC-04`). |
| `mid_stream_abort` *(Terminal SSE event)* | `200` *(stream already open)* | `false` *(never splice)* | Upstream connection dropped after at least one content chunk was already flushed to the client (`TP-BR-0503`). | Surface clear interruption notice to the end user and initiate a fresh request (Tanpopo never splices output from another model mid-stream). |
| `ambiguous_side_effect_timeout` | `504` | **`false`** | Mutating/destructive MCP tool call timed out upstream, leaving execution state indeterminate (`TP-BR-0603`). | Verify state on the target system or supply an idempotency key before manually retrying. |

---

## 6. Automated Secret-Redacted Diagnostics (`tanpopo support-bundle`) & SDK Compatibility Policy

### 6.1. Redacted Diagnostics Archive Structure (`Redacted Diagnostics Bundle`)
Whenever operators share troubleshooting evidence with internal platform teams or attach diagnostics to open-source GitHub issues **without risking credential or customer-payload leakage**, they run:
```bash
tanpopo support-bundle --since 2h --output ./tanpopo-diag-20261111.tar.gz
```
The resulting archive contains strictly multi-pass scrubbed technical metadata (`OPS-AC-10`):
- `doctor-report.json`: Results of all 8 preflight checks, OS/kernel version, `tanpopod` build stamp, free disk capacity, and NTP clock skew.
- `config-redacted.yaml` & `tanpopo.lock`: Complete route trees, policy hierarchies, and pinned extension `SHA-256` hashes, with every secret slot and sensitive internal URL replaced by `[REDACTED_BY_TANPOPO]`.
- `cluster-epochs.json` & `metrics-snapshot.prom`: Point-in-time Prometheus counters, Wasm pool latencies, `SM-04` deployment circuit states, and `ConfigSnapshot` propagation lag.
- `recent-errors-redacted.jsonl`: Up to `500` recent structured error records (retaining only error code, latency, and `request_id`; never capturing `Authorization`, `Cookie`, or prompt/completion bodies).

### 6.2. SDK Versioning & Contract Compatibility Policy (`SDK & Contract Compatibility Policy`)
1. **Zero Client Application Lock-In:** Data Plane endpoints under `/v1/*` maintain compatibility with non-breaking updates of official OpenAI SDKs; all Tanpopo-specific governance extensions pass via optional `X-Tanpopo-*` headers.
2. **Semantic Versioning (`SemVer 2.0`) for Plugin SDK & `tanpopo.extension.v1` Contract:**
   - **Patch (`x.y.Z`):** Bug fixes only; zero changes to JSON Schemas or host ABI signatures.
   - **Minor (`x.Y.0`):** Strictly additive optional fields or host functions (`Additive-Only`); extensions compiled against `x.(Y-1)` continue running unmodified without repackaging.
   - **Backward-Compatibility Window & Deprecation Notice (`N-1` / `12 Months` — `TP-NFR-0902`, `BA-OQ-005`):** Prior to `v1.0`, Tanpopo preserves at least `1` preceding minor contract (`N-1 minor`); from `v1.0` onward, Tanpopo guarantees a minimum **`12-month`** backward-compatibility window accompanied by advance warnings in `tanpopo upgrade preflight` and `SCR-07` so ecosystem authors can invest in long-lived Adapters with confidence.
