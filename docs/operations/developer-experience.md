# Developer Experience: CLI, Management API, SDKs, Error Handling & Diagnostics — Tanpopo

> **Canonical Developer Experience & Unified CLI Specification (Synchronized in `TP-IMP-0002` — Spec Closure Batch B)**
> - **Document ID:** `SPEC-OPS-04` | **Version:** `0.2.0-m0-closure` | **Last Updated:** `2026-11-14`
> - **Authored By:** Developer Experience (DX) Lead & SDK Architect
> - **Single Binary & Contract Parity (`ADR-0001`, `CR-10`):** Tanpopo ships as **1 single unified executable binary named `tanpopo`** covering server execution (`tanpopo serve --role=all|data-plane|control-plane`), operator administration, database engine migration (`tanpopo db migrate-engine`), air-gap bundling, and extension SDK authoring — synchronized `100%` with [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml) and [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml).
> - **Measurement Honesty (`COND-PO-01`):** All latency, overhead, and turnaround targets in this document are **`TARGET / UNVERIFIED`** until empirical verification completes.
> - **Requirements Traceability:** `OPS-07`, `OPS-09`, `TP-FR-0101`..`0105`, `TP-FR-0305`, `TP-FR-0604`, `TP-FR-0901`, `TP-FR-0903`, `TP-FR-0904`, `TP-NFR-0902`, `TP-NFR-1001`, `TP-NFR-1002`, `CR-09`, `CR-10`.

---

## 1. Developer Experience Philosophy (`Zero-Lock-In & Self-Diagnosing DX`)

Tanpopo serves three distinct engineering personas:
1. **AI Application Engineer (`API Consumer`):** Connects official OpenAI, Anthropic, Google GenAI, and MCP SDKs by changing only `base_url` and `api_key`, receiving actionable `CanonicalErrorEnvelope` diagnostics with `x-tanpopo-request-id`.
2. **Platform & SRE Engineer (`Operator`):** Operates `100%` of gateway capabilities identically across the Astryx Web Console, scriptable `tanpopo` CLI (`--output json|yaml`), and `ETag`-guarded `/admin/v1/*` APIs.
3. **Plugin Developer (`Extension Author`):** Scaffolds, contract-tests offline (`CT-01`..`18`), and packs `.tpkg` adapters (`Tier-1 Declarative` or `Tier-2 Wasm`) using `tanpopo plugin ...` without cloning or recompiling `tanpopo` Core.

---

## 2. Unified `tanpopo` CLI Contract (`Single Binary — Zero Multi-Binary Drift`)

### 2.1. Global CLI Conventions & Canonical Exit Codes (`execution-baseline.md`)
- **Output Formatting & Terminal Safety (`CT-27`):** Human-readable aligned tables by default (honoring `NO_COLOR=1`, stripping hazardous `ANSI` / `OSC-52` terminal escape sequences from untrusted strings); pass `--output json` or `--output yaml` for deterministic `stdout` schemas while progress logs write exclusively to `stderr`.
- **Standard Environment Variables:** `TANPOPO_URL`, `TANPOPO_ADMIN_TOKEN`, `TANPOPO_ORG`, `TANPOPO_PROJECT`, `TANPOPO_ENV`, `TANPOPO_MASTER_KEY`.
- **Canonical Deterministic Exit Codes:**
  - `0`: Complete success.
  - `2`: Invalid CLI flag syntax, validation error, or malformed local input argument.
  - `10`: Network / upstream connectivity failure or request timeout.
  - `14`: Authentication failure, `RBAC` permission denial (`401`/`403`), `409 optimistic_lock_conflict`, or canary secret leak detection.
  - `22`: **Contract / Hash / Signature / Integrity / Upgrade Preflight Gate Failed** — e.g., `.tpkg` `SHA-256` mismatch, unauthorized permission escalation, expired `revocations.json` in an air-gap bundle, or `BLOCKED_INCOMPATIBLE` active extensions during `tanpopo upgrade preflight`.
  - `30`: Unexpected internal error.
  - `31`: **Dirty Database Migration State (`FMEA-06`)** — schema migration halted with `dirty = true`; `tanpopo serve` refuses to boot against half-migrated state and prints recovery steps.

### 2.2. Canonical Unified `tanpopo` CLI Command Tree

```text
tanpopo
├── serve                                # Start gateway process (--role=all|data-plane|control-plane, --config, --bootstrap-file)
├── doctor                               # Run 8 preflight checks across clock, ports, Master KEK, storage lock, TLS & SSRFGuard
├── login / logout / whoami              # Authenticate via OIDC Device/Browser PKCE flow or Admin Bearer Token
├── admin                                # Instance break-glass & state integrity commands (OPERATOR):
│   ├── reset-local-owner                # Break-glass local owner password / OIDC lockout recovery on host (CR-09)
│   ├── reset-bootstrap-token            # Mint fresh 15m Bootstrap Token if initial /setup token expired unclaimed
│   ├── verify-state --check-audit-chain # Verify Per-Org + SYSTEM JCS audit hash chains, ledger sums & AEAD AAD (OPS-AC-08)
│   ├── freeze-writes                    # Pause Control Plane config writes ahead of engine migration (Data Plane stays live)
│   └── unfreeze-writes                  # Lift Control Plane write freeze cleanly after verification
├── db
│   └── migrate-engine                   # Checksum-verified SQLite WAL -> PostgreSQL 16+ migration (pre-creates monthly partitions)
├── upgrade
│   └── preflight --target-version <ver> # Verify compatibility.contractVersion across all ENABLED_ACTIVE extensions (exit 22 on block)
├── org / project / env                  # Manage Organizations (OPERATOR), Projects & Environments
├── key                                  # Manage Virtual Keys (list, create, rotate --grace-period, revoke --mode, test)
├── connection                           # Manage Provider Connections (list, create, oauth-login, test, disable)
├── model / route                        # Inspect models & routes (route list, route apply, route explain)
├── policy / budget                      # Sync hierarchical policies & run `tanpopo budget inspect --project <slug>` / `budget set`
├── approval                             # Inspect, approve, or reject pending MCP destructive / extension escalation requests
├── killswitch                           # Trigger 4-level emergency kill-switches (trigger --level --target --abort-streams --reason)
├── incident
│   └── blast-radius                     # Query keys/projects/connections traversing a quarantined extension or compromised connection
├── plugin                               # Extension SDK & lifecycle toolchain (.tpkg):
│   ├── init <name> --tier 1|2           # Scaffold a Tier-1 Declarative Pack or Tier-2 Wasm SDK project
│   ├── validate                         # Validate Manifest JSON Schema, ^@tanpopo(-[a-z0-9]+)?/ rule, allowedHosts & 16 MiB cap
│   ├── test                             # Run 100% offline deterministic cassette suite (CT-01..18)
│   ├── pack / sign                      # Build reproducible .tpkg archive & sign with Cosign / Ed25519
│   ├── migrate-check                    # Check extension compatibility against upcoming N+1 contract schemas
│   └── install / upgrade / rollback / quarantine / lock
├── bundle                               # Air-Gap bundle workflow: `bundle export`, `bundle verify`, `bundle import` (.tpbundle / .tar.zst)
├── backup                               # Encrypted backup workflow: `backup create`, `backup verify`, `backup restore` (.tpop.enc)
├── vault                                # Envelope encryption management (`vault status`, `vault rotate-kek`)
└── support-bundle                       # Export multi-pass secret- and PII-scrubbed diagnostic archive (OPS-AC-10)
```

---

## 3. Management API Ergonomics & Copy-Pasteable SDK Integration Snippets

### 3.1. Management API Conventions (`/admin/v1/*` — [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml))
- **Optimistic Concurrency (`ETag` / `If-Match`):** Mutable resources emit `ETag: "v7"`; stale `If-Match` headers return `409 optimistic_lock_conflict` carrying `error.current_resource`.
- **HMAC-Signed Cursor Pagination:** Collection endpoints accept `?limit=50&cursor=...` (`limit <= 200`) and return `{ "data": [...], "next_cursor": "...", "has_more": true }`.
- **CSRF & Plane Firewall:** Cookie-authenticated mutations require `X-CSRF-Token` (`403 csrf_validation_failed`); `Virtual Keys` (`vk_tp_*`) are barred from `/admin/v1/*` (`401 invalid_credential_plane`).

### 3.2. Drop-In Client SDK Snippets (Zero Custom SDK Required)

#### A. Python (`openai` SDK — Unified `/v1/*`)
```python
from openai import OpenAI

client = OpenAI(
    base_url="https://ai-gateway.corp.example.com/v1",
    api_key="vk_tp_prod_example_only_not_real_000001",
    default_headers={
        "X-Tanpopo-Data-Region": "EU",
    },
)

stream = client.chat.completions.create(
    model="prod-assistant-default",
    messages=[{"role": "user", "content": "Summarize this week's operations report."}],
    stream=True,
)
for chunk in stream:
    if chunk.choices and chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

#### B. TypeScript / Node.js (`openai` SDK + `Idempotency-Key` & `X-Tanpopo-Timeout-Ms`)
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
      'Idempotency-Key': 'job-check-20261114-001',
      'X-Tanpopo-Timeout-Ms': '30000',
    },
  },
);
```

#### C. Lossless Vendor-Native Passthrough (`/native/{dialect}/{upstreamPath}`)
```python
import anthropic

client = anthropic.Anthropic(
    base_url="https://ai-gateway.corp.example.com/native/anthropic",
    api_key="vk_tp_prod_example_only_not_real_000001",
)

msg = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=2048,
    messages=[{"role": "user", "content": "Analyze distributed consensus trade-offs."}],
)
```

#### D. Governed MCP Hub Configuration (`MCP Specification 2025-03-26 Streamable HTTP` — `v0.2`)
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
*(Per `COND-PO-04`, end-to-end IDE client compatibility for async `destructive` HITL approvals is empirically gated on `CT-36` in `M3`.)*

#### E. Rapid CLI Diagnostics (`Key`, `Connection`, `Budget` & `Route Explain`)
```bash
# 1. Verify Virtual Key validity, remaining budget, and model scope (0 billable upstream calls):
tanpopo key test --key "$TANPOPO_VIRTUAL_KEY"

# 2. Run 2-phase blind connectivity probe through BlindBroker + SSRFGuard:
tanpopo connection test prod-vertex-eu-01

# 3. Inspect real-time settled spend and active pre-request holds:
tanpopo budget inspect --project core-assistant

# 4. Dry-run 6-step routing decision tree in terminal (POST /admin/v1/routes/explain):
tanpopo route explain prod-assistant-default --capability vision,tool_calling,strict_json_schema --region EU
```

---

## 4. Documentation Architecture, Offline API Explorer & `snake_case` Structured Logs

1. **Four-Journey Documentation IA:** (1) `5-Minute Quickstart`, (2) `Application Developer Guide`, (3) `Self-Host Operator & SRE Guide`, and (4) `Extension Author SDK Reference`.
2. **Air-Gap Built-In API Explorer (`SCR-15`):** `/devtools/api-explorer` renders binary-embedded [`openapi/dataplane-v1.yaml`](../../openapi/dataplane-v1.yaml) and [`openapi/admin-v1.yaml`](../../openapi/admin-v1.yaml) via `GET /admin/v1/openapi.json` with `0` external network calls.
3. **Canonical `snake_case` Structured JSON Log Contract (`SPEC-CORE-02` Parity):**
   Every log record emitted to `stdout` uses flat **`snake_case`** keys matching `usage-billing-observability.md` `100%`:

```json
{
  "ts": "2026-11-14T16:45:12.304Z",
  "level": "WARN",
  "event": "gateway.routing.pre_stream_failover",
  "entry_point": "unified_http",
  "request_id": "req_01jcd8k9p2v4x7m1n6q8r0s2t4",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "org_id": "org_01jc8001",
  "project_id": "prj_01jc8009",
  "env_id": "env_01jc8010",
  "route_slug": "prod-assistant-default",
  "failed_deployment_id": "dep_openai_eu_01",
  "upstream_status": 429,
  "next_deployment_id": "dep_vertex_eu_02",
  "attempt": 1,
  "overhead_ms": 1.84
}
```

---

## 5. Canonical Error Resolution Guide (`Synchronized with SPEC-EXT-06`)

| Error Code (`error.code`) | HTTP | `retryable` | Root Cause | Concrete Engineer / Administrator Remediation |
|---|---|---|---|---|
| `capability_not_supported` | `400` | `false` | Target deployments lack a required `snake_case` capability (`strict_json_schema`, `vision`, `audio_input`, `document_input`, `tool_calling`, `reasoning`). | Inspect `/models` (`SCR-05`), target a capable `Route`, or use `/native/{dialect}/{upstreamPath}`. |
| `dialect_mismatch` | `400` | `false` | `/native/{dialect}/*` called against a route resolving to another wire dialect. | Point `/native/{dialect}/*` at a route/model matching `{dialect}`. |
| `invalid_virtual_key` | `401` | `false` | Virtual key is malformed, expired, or `REVOKED` (`SM-05`). | Verify `TANPOPO_VIRTUAL_KEY` or issue/rotate a key at `SCR-13`. |
| `invalid_credential_plane` | `401` | `false` | Virtual Key used on `/admin/v1/*`, or admin session cookie used directly on `/v1/*`. | Use Admin Bearer/Session on `/admin/v1/*` and `POST /admin/v1/playground/sessions` for Playground (`AUTH-07`). |
| `scope_or_plane_denied` | `403` | `false` | Key lacks environment, model, MCP tool, or plane permission. | Inspect grants via `tanpopo key test` or update key allowlists at `SCR-13`. |
| `schema_hash_drift_blocked` | `403` | `false` | Upstream MCP tool mutated away from its pinned JCS `schema_hash`. | Inspect Schema Diff on `SCR-10` and re-pin via `POST /admin/v1/mcp/tools/{toolId}/approve-schema` (`INC-06`). |
| `parameter_hash_mismatch` | `403` | `false` | Replay arguments for an approved `destructive` MCP tool differ from the approved 5-tuple JCS hash. | Replay exact approved arguments or request a fresh approval (`UJ-06`). |
| `optimistic_lock_conflict` | `409` | `true` *(after re-read)* | Stale `If-Match` ETag on configuration write. | Re-read resource (or use `error.current_resource` in Diff modal) and retry. |
| `bootstrap_already_completed` | `410` | `false` | `/admin/v1/bootstrap/complete` called on an already initialized instance. | Sign in via `/login`, or run host break-glass `tanpopo admin reset-local-owner` if locked out. |
| `project_budget_exceeded` | `429` | `false` | Settled spend + carryover + active pre-holds reached hard `micro-USD` ceiling. | Run `tanpopo budget inspect` or adjust ceiling at `SCR-12` (`INC-01`). |
| `rate_limit_exceeded` | `429` | `true` *(honor `Retry-After`)* | Caller hit `RPM`, `TPM`, or `Max Concurrent Streams` ceiling. | Back off per `Retry-After` with jitter. |
| `extension_execution_fault` | `502` | `true` *(pre-stream)* | Tier-2 Wasm / Tier-3 Sidecar trapped, timed out (`> 15 ms` / `> 5 ms`), or hit `16 MiB` OOM (`FMEA-05`). | Inspect `tanpopo_sandbox_traps_total` and quarantine/rollback faulty `.tpkg` at `SCR-07` (`INC-03`). |
| `no_compliant_deployment_available` | `503` | `true` *(with backoff)* | Zero healthy deployments satisfy `Data Residency` and policy filters. | Run `tanpopo route explain` to separate `CIRCUIT_OPEN_UNHEALTHY` targets from region-filtered targets (`INC-04`). |
| `mid_stream_abort` *(SSE event)* | `200` *(open stream)* | `false` *(never splice)* | Upstream stream dropped after `>= 1` chunk flushed to caller (`FMEA-01`). | Surface incomplete-stream notice and start a fresh request. |
| `ambiguous_side_effect_timeout` | `504` | **`false`** | `mutating` / `destructive` MCP tool timed out post-dispatch (`0` auto-retries). | Reconcile upstream state before manually retrying with `Idempotency-Key`. |

---

## 6. Secret-Redacted Diagnostics (`tanpopo support-bundle`) & Contract SLA

1. **Redacted Diagnostics Archive (`OPS-AC-10`):**
   ```bash
   tanpopo support-bundle --since 2h --output ./tanpopo-diag-20261114.tar.gz
   ```
   Produces a multi-pass scrubbed archive containing:
   - `doctor-report.json`: Results of all 8 preflight checks, OS/kernel version, **`tanpopo`** build stamp, free disk capacity, and clock skew.
   - `config-redacted.yaml` & `tanpopo.lock`: Routes, policies, and pinned `.tpkg` `SHA-256` digests with all secret slots and private URLs replaced by `[REDACTED_BY_TANPOPO]`.
   - `cluster-epochs.json` & `metrics-snapshot.prom`: Bounded Prometheus counters (`tanpopo_http_requests_total`, `tanpopo_proxy_overhead_seconds`, `tanpopo_config_snapshot_age_seconds`, `tanpopo_telemetry_dropped_total`) and `SM-04` circuit states.
   - `recent-errors-redacted.jsonl`: Up to `500` `snake_case` error metadata records (`0` headers, `0` cookies, `0` prompt/completion text).
2. **Extension Contract Compatibility Window (`TP-NFR-0902`):** Preserves `N-1` minor compatibility during `0.x` and `>= 12 months` across `1.x`, enforced prior to upgrades by `tanpopo upgrade preflight`.
