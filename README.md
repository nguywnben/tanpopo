<div align="center">

<img src="./docs/assets/tanpopo-logo.svg" alt="Tanpopo Logo" width="96" height="96" />

# Tanpopo

**Self-hosted, extension-first AI gateway, MCP hub, and A2A coordinator.**

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![CI (Stage 0-2)](https://github.com/nguywnben/tanpopo/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/nguywnben/tanpopo/actions/workflows/ci.yml)
[![Release Artifact Dry Run](https://github.com/nguywnben/tanpopo/actions/workflows/release-dry-run.yml/badge.svg?branch=main)](https://github.com/nguywnben/tanpopo/actions/workflows/release-dry-run.yml)
[![Go Version](https://img.shields.io/badge/Go-1.26.11%2B_(CGO__ENABLED%3D0)-00ADD8.svg)](./go.mod)
[![M8ven Score](https://m8ven.ai/badge/mcp/nguywnben-tanpopo-4xf4w2?v=aa7f45efff8dc06d26b665eff741f809)](https://m8ven.ai/mcp/nguywnben-tanpopo-4xf4w2?s=readme)

[Quickstart](#quickstart) · [Capabilities](#why-tanpopo) · [Docs by Role](#documentation) · [Benchmarks & Scope](#performance-and-scope) · [Security](./SECURITY.md)

</div>

---

Tanpopo is a single-binary pure-Go gateway that unifies LLM routing, provider extension sandboxing, Model Context Protocol (`MCP`) tool governance, Agent-to-Agent (`A2A`) delegation, and integer `micro-USD` FinOps in one self-hosted control and data plane.

Run a single `SQLite WAL` instance locally with an embedded 15-language **Astryx Neutral** Web Console, or scale horizontally with **PostgreSQL 16+ (`FORCE RLS`)**, **Valkey Cluster**, and zero-egress **Air-Gap `.tpbundle` enclaves**.

<p align="center">
  <img src="./docs/assets/console-overview.png" alt="Tanpopo Web Console overview displaying Acme AI Platform telemetry and navigation" width="960" />
</p>

---

## Why Tanpopo

- **Extension-First Provider Mesh (`.tpkg`):** Add or upgrade provider adapters without recompiling Core across **Tier-1 Declarative Packs**, **Tier-2 `wazero` Wasm Sandboxes** (`<= 16 MiB` guest heap), and **Tier-3 Isolated mTLS/UDS Sidecars**.
- **Blind Host Credential Broker & `SSRFGuard`:** Secrets stay sealed under 3-tier `AES-256-GCM` envelope encryption (`Master KEK -> Org DEK -> Project DEK` or Cloud KMS/Vault). Plugins never read plaintext keys, and connect-time IP inspection blocks DNS rebinding and metadata SSRF.
- **Four Strictly Separated Planes:**
  - **Provider Ingress (`/v1/*` & `/native/{dialect}/*`):** Unified OpenAI-compatible endpoints plus lossless native dialect passthrough.
  - **Extension Governance:** Ed25519-signed `tanpopo.lock`, permission-diff four-eyes approval, and rollback-safe upgrades (`tanpopo plugin migrate-check`).
  - **MCP Hub (`/mcp`):** `MCP 2025-03-26` Streamable HTTP with per-key tool ACLs, `RFC 8785 JCS` schema pinning, and interactive human approvals.
  - **A2A Coordinator (`/a2a/*`):** Default-disabled inter-agent delegation with cycle checks, `hop_count <= 5`, and atomic sub-budget carving.
- **Exact Integer `micro-USD` FinOps:** Pre-request reservations (`429 project_budget_exceeded`), metadata-only logging defaults (`0 bytes` prompt/completion persistence), tamper-evident audit hash chains, and emergency kill-switches.
- **Air-Gap Ready:** Fonts (`Google Sans`), SVG assets, and 15 Web Console/CLI locales are embedded directly into the Go binary; signed `.tpbundle` archives (`tanpopo bundle export`, `tanpopo bundle verify`, `tanpopo bundle import`) operate with `--offline`.

> **Compliance Policy:** Tanpopo supports official commercial Provider APIs, authorized workload/OAuth 2.1 identities, and self-hosted open-weight engines (`vLLM`, `SGLang`, `Ollama`). Connectors that repurpose consumer chat subscriptions are prohibited ([Policy Details](./docs/governance/project-charter.md)).

---

## Quickstart

### Prerequisites & Build from Source

- **Go `>= 1.26.11`** (`CGO_ENABLED=0` static build; no C compiler required).
- Pre-built Web Console static assets are committed in [`internal/webui/dist/`](./internal/webui/dist/) for direct Go builds. To modify or rebuild the frontend bundle (requires **Node.js `>= 22.13`**):

```bash
cd web/console
npm ci --ignore-scripts
npm run build
npm run sync:embed
cd ../..
```

### Option A: Single Binary — Linux / macOS (`bash`)

```bash
# 1. Clone and build the static pure-Go binary
git clone https://github.com/nguywnben/tanpopo.git
cd tanpopo
CGO_ENABLED=0 go build -o ./bin/tanpopo ./cmd/tanpopo

# 2. Generate a 256-bit (64 hex chars) Master KEK for local use
export TANPOPO_MASTER_KEY="$(openssl rand -hex 32)"

# 3. Start Tanpopo (prints the one-time bootstrap token and saves it to data/bootstrap.token)
./bin/tanpopo serve --role all --listen 127.0.0.1:8080
# Inspect the saved token anytime before setup completes:
cat data/bootstrap.token
```

### Option B: Single Binary — Windows (`PowerShell`)

```powershell
# 1. Clone and build the static pure-Go executable
git clone https://github.com/nguywnben/tanpopo.git
cd tanpopo
$env:CGO_ENABLED = '0'
go build -o .\bin\tanpopo.exe .\cmd\tanpopo

# 2. Generate a 256-bit (64 hex chars) Master KEK (PowerShell 5.1 & 7+ compatible)
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
$env:TANPOPO_MASTER_KEY = ([System.BitConverter]::ToString($bytes) -replace '-', '').ToLowerInvariant()

# 3. Start Tanpopo (prints the one-time bootstrap token and saves it to data\bootstrap.token)
.\bin\tanpopo.exe serve --role all --listen 127.0.0.1:8080
# From another PowerShell window, read the saved token (default location under TANPOPO_DATA_DIR=data):
Get-Content .\data\bootstrap.token
```

Then open **`http://127.0.0.1:8080/setup`** in your browser, paste the token from terminal output or `data/bootstrap.token` (Windows: `data\bootstrap.token`), create the owner account, activate a bundled official adapter under **Extensions → Marketplace**, create and test a connection under **Providers**, and mint a Virtual Key (`vk_tp_...`).

### Option C: Standalone Docker Compose

Store your 64-hex Master KEK with strict permissions (`umask 077` before write, owned by container UID `65532`):

```bash
(umask 077 && openssl rand -hex 32 > .tanpopo-master-kek)
chmod 0400 .tanpopo-master-kek
sudo chown 65532:65532 .tanpopo-master-kek
export TANPOPO_MASTER_KEY_FILE="$PWD/.tanpopo-master-kek"
docker compose -f deploy/compose/docker-compose.standalone.yml up -d --build
```

### Send Your First Request

```bash
curl -fsS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer vk_tp_test_example_replace_me" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Say hello from Tanpopo!"}]
  }'
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8080/v1", api_key="vk_tp_test_example_replace_me")
resp = client.chat.completions.create(model="gpt-4o-mini", messages=[{"role": "user", "content": "Hello!"}])
print(resp.choices[0].message.content)
```

---

## Bundled Adapters & Deployment Modes

| Surface | Built-In Support | Notes |
|---|---|---|
| **Official Provider Adapters** | `openai`, `anthropic`, `gemini`, `aws-bedrock`, `azure-openai`, `cohere-rerank`, `openai-compatible`, `ollama` | [`extensions/official/`](./extensions/official/) |
| **Standalone Mode** | `tanpopo serve --role all` on `SQLite WAL` | [`docker-compose.standalone.yml`](./deploy/compose/docker-compose.standalone.yml) · [`tanpopo.service`](./deploy/systemd/tanpopo.service) |
| **Clustered HA Mode** | `tanpopo serve --role control-plane` & `--role data-plane` with `PostgreSQL 16+` (`FORCE RLS`) + 3-shard `Valkey Cluster` | [`deploy/helm/tanpopo/`](./deploy/helm/tanpopo/Chart.yaml) · [HA Guide](./docs/guides/production-ha-and-airgap.md) |
| **Air-Gap Enclave** | `--offline` runtime with `tanpopo bundle export`, `tanpopo bundle verify`, `tanpopo bundle import` | [Air-Gap Workflow](./docs/guides/production-ha-and-airgap.md#4-disconnected-air-gap-workflow-tp-imp-0045) |

---

## Performance and Scope

- **Single-host `2 vCPU / 2 GiB` container baseline:** Cold start in **`839 ms`**, idle RSS **`75.16 MiB`**, **`2,346 / 2,346` (`100%`)** concurrent SSE completions, and overload shedding **`2,735 / 2,735`** checks (`med=1.48 ms`). Single-node SQLite (`SetMaxOpenConns(1)`) reached **`109.48 req/s`** (below a `500 RPS` target on 2 vCPUs).
- **Single-VM Docker Compose HA & Quality:** Staggered HA rolling restart completed **`574 / 575` (`99.82%`)** SSE streams and `DR-04` DB rollback in **`4.07 s` (`< 60 s`)**; Console E2E passed across `Chromium`, `Firefox`, and `WebKit` over HTTPS (`0` serious/critical Axe issues, `15` native locales).
- **Explicit unverified boundaries:** Continuous `>= 4h` fuzz soak, offline `lighthouse` CLI composite scoring, physical multi-host Kubernetes HA networking, and macOS/Windows OS kernel network isolation remain `UNVERIFIED`. See full evidence in [`docs/quality/benchmark-evidence.md`](./docs/quality/benchmark-evidence.md) and [`docs/releases/v1.0.0-readiness-dossier.md`](./docs/releases/v1.0.0-readiness-dossier.md).

---

## Documentation

| What You Need | Guide | Highlights |
|---|---|---|
| **Run Tanpopo & call `/v1/*`** | [**Quickstart Guide**](./docs/guides/quickstart.md) | Clean setup, `/setup` onboarding, OpenAI SDK & `/native/*` passthrough |
| **Build or test a `.tpkg` adapter** | [**Extension SDK Guide**](./docs/guides/plugin-authoring-sdk.md) | `tanpopo plugin init`, `validate`, `test`, `pack`, `migrate-check` |
| **Deploy HA clusters or air-gap enclaves** | [**Production HA & Air-Gap**](./docs/guides/production-ha-and-airgap.md) | PG16 `FORCE RLS`, Valkey Cluster, `.tpop.enc` backups, `INC-01..07`, `.tpbundle` |
| **Audit Vault, MCP & A2A controls** | [**Security & MCP/A2A**](./docs/guides/security-and-mcp-governance.md) | KMS/Vault envelope keys, `JCS` schema pinning, Four-Eyes & Crypto-Shred |
| **Read end-user docs in 15 languages** | [**Guides Portal (15 Locales)**](./docs/guides/README.md) | Localized `user-guide.md` handbooks across all supported languages |
| **Inspect specs, ADRs & release status** | [**Architecture & Governance Hub**](./docs/README.md) | System design, OpenAPI 3.1 contracts, `v1.0.0` readiness dossier & unit ledger |

---

## Security & Contributing

- **Vulnerability Disclosure:** Report security vulnerabilities privately via [GitHub Private Vulnerability Reporting](https://github.com/nguywnben/tanpopo/security/advisories/new) as documented in [`SECURITY.md`](./SECURITY.md). Please do not file public issues for security bugs.
- **Contributing:** See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for local Go/Console verification (`go run ./scripts/ci/check.go --all`, `go run ./scripts/docs/check_drift.go`) and [`CONSTRAINTS.md`](./CONSTRAINTS.md).
- **Changelog & Notices:** See [`CHANGELOG.md`](./CHANGELOG.md) and [`NOTICE`](./NOTICE).

---

## License

Licensed under the **[Apache License, Version 2.0](./LICENSE)**.
