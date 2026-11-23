# Journey 1: Quickstart & First Inference (`< 3 Minutes`)

Get a self-hosted Tanpopo AI Gateway running locally, complete initial bootstrap, create a Virtual Key, and route your first OpenAI-compatible and native inference requests.

> **Offline & Single-Binary Guarantee:** Tanpopo embeds its entire React Web Console inside the Go binary via `go:embed` with **zero runtime external CDN or font fetches**.

---

## 1. Choose Your Startup Path

### Option A — Local Single Binary (`Mode A: Standalone`)

```bash
# 1. Build the unified binary from source (pure Go, CGO_ENABLED=0)
CGO_ENABLED=0 go build -o ./bin/tanpopo ./cmd/tanpopo

# 2. Configure the local Master KEK (32-byte hex or base64 for development)
export TANPOPO_MASTER_KEY="000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

# 3. Start Control Plane + Data Plane + Web Console on 127.0.0.1:8080
#    Prints the one-time bootstrap token and writes it to data/bootstrap.token (Windows: data\bootstrap.token)
./bin/tanpopo serve --listen 127.0.0.1:8080 --role all
```

By default, Tanpopo stores runtime state under `TANPOPO_DATA_DIR=data` and writes the one-time bootstrap token to `data/bootstrap.token` (`data\bootstrap.token` on Windows PowerShell, overrideable via `TANPOPO_BOOTSTRAP_TOKEN_FILE`):

- **Linux / macOS:** `cat data/bootstrap.token`
- **Windows PowerShell:** `Get-Content .\data\bootstrap.token`

To verify storage permissions and service readiness before or after boot:

```bash
./bin/tanpopo doctor --output json
```

### Option B — Docker Compose Standalone

Run the pinned standalone stack from [deploy/compose/docker-compose.standalone.yml](../../deploy/compose/docker-compose.standalone.yml):

```bash
docker compose -f deploy/compose/docker-compose.standalone.yml up -d
```

Check container readiness (returns `200 OK` only when snapshot epochs and storage are synchronized):

```bash
curl -fsS http://127.0.0.1:8080/readyz
```

---

## 2. Bootstrap, Activate an Adapter & Issue Your First Virtual Key

1. Open **`http://127.0.0.1:8080/setup`** in your browser, paste the one-time bootstrap token from terminal output or `data/bootstrap.token` (`data\bootstrap.token` on Windows), and create the initial `ORG_OWNER` account.
2. Open **Extensions → Marketplace**, choose a bundled official adapter (for example `@tanpopo/openai` or `@tanpopo/openai-compatible`), click **Stage for review**, and click **Approve & activate**. Activated adapters register automatically under **Providers → Adapters**.
3. Under **Providers**, seal your upstream credential (if required), create a connection using the activated adapter, and run **Test connection** to promote it to `ACTIVE`. Then add a logical model, deployment, and route under **Models** and **Routing**.
4. Under **Access → Virtual Keys**, issue an environment-scoped Virtual Key (`vk_tp_...`) and set optional soft/hard budget ceilings in integer nano-USD / micro-USD.
   - **Strict Plane Separation (`AUTH-07`):** Virtual Keys (`vk_tp_*`) authenticate Data Plane inference (`/v1/*`, `/native/*`, `/mcp`, `/a2a/*`) and are rejected with `401 invalid_credential_plane` on `/admin/v1/*`.

> **Credential Placeholder Convention:** Examples below use synthetic placeholder tokens (`vk_tp_test_example_...`) that have no access to any real environment.

---

## 3. Send Your First Inference Request

### OpenAI-Compatible Endpoint (`POST /v1/chat/completions`)

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer vk_tp_test_example_replace_me" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "user", "content": "Reply with OK if gateway routing is healthy."}
    ],
    "stream": false
  }'
```

Inspect available routed models (`GET /v1/models`):

```bash
curl -sS http://127.0.0.1:8080/v1/models \
  -H "Authorization: Bearer vk_tp_test_example_replace_me"
```

### Python OpenAI SDK (`base_url` override)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="vk_tp_test_example_replace_me",
)

resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello from Tanpopo"}],
    stream=True,
)
for chunk in resp:
    delta = chunk.choices[0].delta.content
    if delta:
        print(delta, end="", flush=True)
```

### Native Provider Pass-Through (`/native/*`)

For provider-native features (such as Anthropic prompt caching or Gemini extended schemas) while retaining Tanpopo Virtual Key auth, budget holds, SSRF egress guards, and JCS audit trails, call `/native/{dialect}/{upstreamPath}` as specified in [openapi/dataplane-v1.yaml](../../openapi/dataplane-v1.yaml).

---

## 4. Next Steps by Persona

- **Extension Authors:** [Journey 2 — Plugin Authoring SDK](./plugin-authoring-sdk.md)
- **Platform & SRE Engineers:** [Journey 3 — Production HA, Backups & Air-Gap](./production-ha-and-airgap.md)
- **Security & AI Governance Teams:** [Journey 4 — Security, Vault/KMS & MCP/A2A Governance](./security-and-mcp-governance.md)
- **Role Guides (15 Locales):** [User Guide Index](./README.md) · [English Role Guide](./en/user-guide.md)
