<div align="center">

<img src="./docs/assets/tanpopo-logo.svg" alt="Tanpopo Logo" width="96" height="96" />

# Tanpopo

**Self-hosted, single-binary, extension-first AI Gateway, MCP Hub, and A2A Coordinator.**

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![CI (Stage 0-2)](https://github.com/nguywnben/tanpopo/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/nguywnben/tanpopo/actions/workflows/ci.yml)
[![Release Artifact Dry Run](https://github.com/nguywnben/tanpopo/actions/workflows/release-dry-run.yml/badge.svg?branch=main)](https://github.com/nguywnben/tanpopo/actions/workflows/release-dry-run.yml)
[![Go Reference](https://img.shields.io/badge/Go-1.26%2B_(CGO__ENABLED%3D0)-00ADD8.svg)](./go.mod)

[Quickstart](#quickstart-under-3-minutes) · [Documentation Portal](./docs/guides/README.md) · [Architecture](./docs/architecture/system-design.md) · [Benchmarks & Limits](#empirical-benchmarks--known-limitations) · [Security](./SECURITY.md)

</div>

---

Tanpopo decouples AI infrastructure from vendor lock-in. Deploy a single pure-Go binary embedded with a 15-language **Astryx Neutral** Web Console on a laptop (`SQLite WAL`), or scale horizontally to stateless clusters backed by **PostgreSQL 16+ (`FORCE RLS`)**, **Valkey Cluster**, and zero-egress **Air-Gap `.tpbundle` enclaves**.

## What Makes Tanpopo Different

- **Extension-First Architecture (`.tpkg`):** Add or upgrade provider adapters without recompiling Core across three isolation tiers—**Tier-1 Declarative Packs**, **Tier-2 Pure-Go `wazero` Wasm Sandboxes** (`<= 16 MiB` guest heap), and **Tier-3 Isolated mTLS/UDS Sidecars**.
- **Blind Host Credential Broker & `SSRFGuard`:** Secrets (`API keys`, `AWS SigV4`, `OAuth 2.1`) stay sealed under a 3-tier `AES-256-GCM` envelope hierarchy (`Master KEK -> Org DEK -> Project DEK` or Cloud KMS/Vault). Plugins never see plaintext credentials, and `net.Dialer.Control` inspects resolved IPs at connect time to defeat DNS rebinding (`TTL=0`) and metadata SSRF.
- **Four Strictly Separated Planes:**
  - **Provider Ingress (`/v1/*` & `/native/{dialect}/*`):** OpenAI-compatible unified endpoints plus lossless native dialect passthrough.
  - **Extension Runtime (`CR-02`):** Signed lockfile (`tanpopo.lock`), Ed25519 trust roots, permission-diff four-eyes approval, and `< 5s` rollback.
  - **MCP Governance Hub (`/mcp`):** `MCP 2025-03-26` Streamable HTTP with per-key tool ACLs, `RFC 8785 JCS` schema-pinning against rug-pulls, zero blind retries, and interactive HITL approvals (`SM-06`).
  - **A2A Federation Plane (`/a2a/*`):** Default-disabled inter-agent delegation (`SM-09`) with cycle detection, `hop_count <= 5`, and atomic parent-child sub-budget carving.
- **Exact Integer `micro-USD` FinOps & Privacy-First SRE:** Pre-request budget holds (`429 project_budget_exceeded`) prevent concurrent overruns without mid-stream truncation. Prompt/completion logging defaults to `0 bytes`, paired with tamper-evident `RFC 8785 JCS` audit hash chains and 4-level emergency kill-switches.
- **True Zero-Egress Air-Gap Ready:** All 15 Web Console translations, `Figtree` fonts, and SVG icons embed directly into the `tanpopo` binary; `tanpopo bundle export|verify|import` moves signed `.tar.zst` `.tpbundle` archives across offline enclaves.

---

## Quickstart (Under 3 Minutes)

### Option A: Single-Binary Local Run (Linux / macOS / Windows)

```bash
# 1. Build the unified pure-Go binary (requires Go 1.26+)
CGO_ENABLED=0 go build -o ./bin/tanpopo ./cmd/tanpopo

# 2. Generate a 256-bit Master KEK for local dev
export TANPOPO_MASTER_KEY=$(openssl rand -hex 32)

# 3. Start Tanpopo (prints one-time bootstrap token on stdout/stderr)
./bin/tanpopo serve --role all --listen 127.0.0.1:8080
```

Open **`http://127.0.0.1:8080/setup`** in your browser to create the owner account, configure your first provider (`OpenAI`, `Anthropic`, `Gemini`, `AWS Bedrock`, `Azure OpenAI`, or local `Ollama` / `vLLM`), and mint a Virtual Key (`vk_tp_...`).

### Option B: Hardened Standalone Docker Compose

```bash
openssl rand -hex 32 > /tmp/master-kek && chmod 0400 /tmp/master-kek
# Container runs rootless as UID/GID 65532 (deploy/compose/docker-compose.standalone.yml)
sudo chown 65532:65532 /tmp/master-kek
export TANPOPO_MASTER_KEY_FILE=/tmp/master-kek
docker compose -f deploy/compose/docker-compose.standalone.yml up -d --build
```

### Send Your First Inference Request

```bash
curl -fsS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer vk_tp_test_example_replace_me" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Say hello from Tanpopo!"}]
  }'
```

Or drop Tanpopo straight into any OpenAI SDK client:

```python
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8080/v1", api_key="vk_tp_test_example_replace_me")
resp = client.chat.completions.create(model="gpt-4o-mini", messages=[{"role": "user", "content": "Hello!"}])
print(resp.choices[0].message.content)
```

> Follow the full walkthrough in [`docs/guides/quickstart.md`](./docs/guides/quickstart.md).

---

## Bundled Official Adapters & `DEC-004` Compliance Boundary

Tanpopo exclusively supports **official commercial Provider APIs**, **self-hosted open-weights inference engines**, and **vendor-authorized OAuth 2.1 / Enterprise IAM flows**. Connectors or workarounds that scrape or repurpose personal consumer subscriptions (`ChatGPT Plus/Pro`, `Claude Pro/Max`, `Codex`, etc.) are strictly prohibited (`DEC-004`).

| Adapter / Extension (`extensions/official/`) | Protocol / Capability | Auth Modes Supported | Compliance Status |
|---|---|---|---|
| **OpenAI** | `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings` | `API Key` (`Bearer`) | Official API (`DEC-004` Compliant) |
| **Anthropic** | Unified + `/native/anthropic/*` passthrough | `API Key` (`x-api-key`) | Official API (`DEC-004` Compliant) |
| **Google Gemini** | Unified + `/native/gemini/*` passthrough | `API Key` (`x-goog-api-key`) | Official API (`DEC-004` Compliant) |
| **AWS Bedrock** | Converse / InvokeModel Streaming | `AWS SigV4` / `STS` Workload Identity | Official Cloud API (`DEC-004` Compliant) |
| **Azure OpenAI** | Azure Chat / Responses / Embeddings | `API Key` / Entra Workload Identity | Official Cloud API (`DEC-004` Compliant) |
| **Cohere Rerank** | `/v1/rerank` Tier-2 Wasm Adapter | `API Key` (`Bearer`) | Official API (`DEC-004` Compliant) |
| **OpenAI-Compatible & Ollama** | Self-hosted `vLLM`, `SGLang`, `Ollama` | None / `Bearer` + explicit CIDR `allow_private_ip` | Self-Hosted Infra (`DEC-004` Compliant) |

---

## Deployment Topologies

| Topology | State Backend | Coordination & Caching | Operational Guide |
|---|---|---|---|
| **Standalone (`--role all`)** | Single-node `SQLite WAL` | In-process memory | [`deploy/compose/docker-compose.standalone.yml`](./deploy/compose/docker-compose.standalone.yml) · [`deploy/systemd/tanpopo.service`](./deploy/systemd/tanpopo.service) |
| **Clustered HA (`--role control-plane\|data-plane`)** | `PostgreSQL 16+` (`FORCE RLS` + monthly range partitions) | 3-shard `Valkey Cluster` (`tp:v1:{<org_id>}:*` Lua holds & `<5s` revocation Pub/Sub) | [`deploy/helm/tanpopo/`](./deploy/helm/tanpopo/Chart.yaml) · [`docs/guides/production-ha-and-airgap.md`](./docs/guides/production-ha-and-airgap.md) |
| **Zero-Egress Enclave (`--offline`)** | `SQLite WAL` or `PostgreSQL 16+` | `tanpopo bundle export\|verify\|import` (`.tar.zst` + Ed25519) + embedded Console/fonts | [`docs/guides/production-ha-and-airgap.md`](./docs/guides/production-ha-and-airgap.md#4-disconnected-air-gap-workflow-tp-imp-0045) |

---

## Empirical Benchmarks & Known Limitations

Every number below comes directly from isolated loopback/container runs in [`docs/quality/benchmark-evidence.md`](./docs/quality/benchmark-evidence.md) (`Intel Xeon 2 vCPU @ 2.30GHz` host, Docker `--cpus=2 --memory=2g`) and [`docs/security/pentest-v1.0-remediation-report.md`](./docs/security/pentest-v1.0-remediation-report.md), preserving all unverified and partial boundaries transparently (`COND-PO-01`):

- **Verified Component & Container Baselines:**
  - **Cold start & Memory (`SLO-05`):** Cold start to `/readyz = 200` in **`1,045 ms`**; idle RSS **`74.71 MiB`**; **`1,715 / 1,715`** concurrent SSE streams completed `200 OK [DONE]` (`0` 5xx) at **`221.4 MiB` RSS**.
  - **Hot-Path Microbenchmarks:** `EvaluateEffective` **`256.2 ns/op` (`0 allocs`)**, `SnapshotLookup` **`475.7 ns/op` (`0 allocs`)**, Tier-1/Tier-2 transform **`14.6–18.8 µs/op`**.
  - **Chaos & Disaster Recovery (`FMEA-01..08` / `DR-01..04`):** All `8/8` FMEA drills and `4/4` DR drills passed under `-race` (`DR-04` rollback in `113.9 ms` `-race` / `4.50 ms` CGO=0; live PG16+Valkey `verify-state` in **`278 ms`**).
  - **5-Surface Pen-Test & SAST (`GATE-REL-03`):** `0` reachable vulnerabilities in `govulncheck` (Go `1.26.11+`) and `npm audit --omit=dev`; `0` Critical/High findings in [`pentest_5surfaces_test.go`](./tests/security/pentest_5surfaces_test.go); air-gap `.tpbundle` + Multi-Org Console verified inside `docker run --network=none`.
- **Transparent Current Limits (`COND-PO-01`):**
  - **Standalone SQLite E2E Saturation:** Full-stack `500 RPS` attempt on single-host `2 vCPU` SQLite capped at **`103.69 req/s`** (`0%` HTTP errors, elevated p95 latency) due to single-writer SQLite serialization (`SetMaxOpenConns(1)`) and SSRF connect-time `DisableKeepAlives: true` dialer isolation.
  - **Docker Overload / Rolling Race Edge:** `overload_503.json` cleanly shed `2,863` excess requests via `503 gateway_overloaded` (recovered `151/151`), with Docker `p95=27.86 ms` exceeding the `<5 ms` target under 2-vCPU contention; Docker LB rolling poll hit `4/1,410` pre-connect resets without Kubernetes `preStop` delay (in-process 3-node drain scored `100/100`).
  - **Pending Production/Multi-Host Scale & Soak Checks:** Multi-host physical HA failover, `>= 4h` fuzz/load soak, offline `lighthouse` CI score, `INP <= 150ms`, and macOS/Windows kernel-level `unshare` isolation remain `UNVERIFIED`; `axe-core` SC 2.5.3 `label-content-name-mismatch` (`Stepper`, desktop jump pill) and pre-existing `astryx doctor agent-docs` root check are tracked explicitly in `TP-IMP-0047`.

---

## Documentation Portal

| Guide / Document | Audience | Summary |
|---|---|---|
| **[4-Journey Docs Index](./docs/guides/README.md)** | All users | Multi-language User Guides (`15` languages) + Deep Technical Journeys |
| **[Journey 1: Quickstart](./docs/guides/quickstart.md)** | App Developers | Binary build/Compose setup, `/setup` wizard, OpenAI SDK & `/native/*` snippets |
| **[Journey 2: Extension Authoring SDK](./docs/guides/plugin-authoring-sdk.md)** | Extension Authors | `tanpopo plugin init|validate|test|pack|migrate-check`, Tier-1 vs Wasm vs Sidecar |
| **[Journey 3: Production HA, DR & Air-Gap](./docs/guides/production-ha-and-airgap.md)** | SRE & Platform Ops | PG16 `FORCE RLS`, Valkey HA Helm, `.tpop.enc` backups, `DR-01..04`, `INC-01..07`, `.tpbundle` |
| **[Journey 4: Security, MCP & A2A Governance](./docs/guides/security-and-mcp-governance.md)** | Security & Governance | Envelope Vault/KMS, OAuth 2.1, MCP `JCS` pinning + HITL, A2A budgets, `Four-Eyes` & Shred |
| **[Architecture & Governance Hub](./docs/README.md)** | Contributors & Architects | Full ASTRA specifications, ADRs, threat model, OpenAPI 3.1 contracts & 47-unit index |

---

## Security & Contributing

- **Security Advisories:** Follow coordinated disclosure guidelines in [`SECURITY.md`](./SECURITY.md). Never open public GitHub issues for suspected vulnerabilities.
- **Contributing:** See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for local Go/Console test workflows (`go run ./scripts/ci/check.go --all`, `go run ./scripts/docs/check_drift.go`), `CONSTRAINTS.md` quality gates, and `DEC-004` compliance rules.
- **Changelog & Attribution:** See [`CHANGELOG.md`](./CHANGELOG.md) and [`NOTICE`](./NOTICE).

---

## License

Licensed under the **[Apache License, Version 2.0](./LICENSE)**.
