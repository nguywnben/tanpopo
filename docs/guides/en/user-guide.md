# Tanpopo User Guide

**Tanpopo** is a self-hosted, single-binary AI Gateway and Model Context Protocol (MCP) Hub built for deterministic routing, write-only credential security, exact micro-USD FinOps accounting, and offline-ready operation.

---

## 1. Quick Start & First-Run Setup

1. Start the Tanpopo server:
   ```bash
   tanpopo serve --role all-in-one --listen 127.0.0.1:8080
   ```
2. On first launch, Tanpopo prints a one-time **bootstrap token** to the server log.
3. Open `http://127.0.0.1:8080/setup` in your browser and complete the 4-step setup wizard:
   - **Owner account:** Enter the bootstrap token, administrator email, password (minimum 12 characters), organization name, and first project name.
   - **First connection (optional):** Connect a commercial API provider (OpenAI, Anthropic, Google Gemini, or OpenAI-compatible) or a local self-hosted engine (vLLM, Ollama). Local endpoints require explicitly enabling private network access and listing exact non-wildcard CIDRs (for example, `127.0.0.1/32`).
   - **Key custody:** Confirm that you have backed up your instance master encryption key (KEK). Losing the master key makes encrypted vault secrets unrecoverable.
   - **Review & initialize:** Complete setup, store your one-time Virtual Key securely, and verify the first streaming request.

---

## 2. Language & Appearance

- **Web Console:** Use the language selector in the top bar (or on the Setup and Sign-In pages). **Auto** is selected by default and picks the first supported language from your browser preferences, falling back to English. Manual selections persist across reloads, and you can return to **Auto** at any time.
- **CLI:** Pass `--lang <code>` (for example, `tanpopo --lang ja --help`) or set `TANPOPO_LANG` / `LC_ALL` / `LC_MESSAGES` / `LANG`. Command names, flag names, and machine-readable output remain stable across languages.
- **API Errors & Alerts:** Control-plane endpoints under `/admin/v1/*` honor the `Accept-Language` header for human-readable `error.message` text and operational alert summaries while preserving stable `error.code`, `error.type`, and JSON schemas.
- **Offline Operation:** All 15 languages, fonts, and icons are bundled into the binary—zero external CDN requests are made at runtime.

---

## 3. Core Console Workflows

### Overview & Traces
- **Overview** displays live requests, error rate, p95 proxy overhead, settled micro-USD spend, active holds, circuit-open deployments, and active alerts with direct remediation links.
- **Traces** records metadata-only execution telemetry (zero prompt or completion payloads are stored). Click any request to inspect its stage-by-stage waterfall and metering accuracy (`Exact`, `Estimated`, or `Unmetered`).

### Providers, Models & Routing
- **Providers:** Seal API keys, AWS IAM credentials, webhook HMAC secrets, or TLS certificates in the write-only vault—plaintext is sent once and never returned. Bind adapters to upstream endpoints and run live probes.
- **Models & Deployments:** Declare logical models with capabilities and bind them to provider connections using exact non-negative integer **nano-USD** rate cards.
- **Routes & Policies:** Configure deterministic routing (`Priority failover`, `Weighted`, `Weighted round robin`, `Lowest cost`, `Lowest latency (EWMA)`). Use **Route Explain** to simulate routing decisions in dry-run mode without upstream egress or budget holds. Configure monotonic policies at Organization, Project, and Environment scopes—child scopes can only tighten parent bounds.

### Extensions, MCP Hub & Playground
- **Extensions:** Stage packages from the offline-cached Marketplace or by digest-pinned URI + 64-character SHA-256. Review explicit permission diffs before approving activation. Quarantine compromised packages immediately to sever active streams.
- **MCP Hub:** Register Streamable HTTP MCP servers, synchronize tools, pin RFC 8785 JCS schema hashes, assign risk tiers (`read_only`, `mutating`, `destructive`), and grant tools to specific Virtual Keys.
- **Playground:** Test prompts interactively with an ephemeral, budget-capped session key that stays only in the current tab. Copy ready-to-run cURL, Python, and TypeScript OpenAI SDK snippets.

### Access, FinOps, Security & Operations
- **Access & Keys:** Issue environment-scoped Virtual Keys, rotate keys with a 24-hour grace window, or revoke them via graceful drain or emergency stream abort. Manage 6-role RBAC assignments (`Operator`, `Org Owner`, `Team Admin`, `Security Reviewer`, `FinOps Viewer`, `Developer`).
- **FinOps:** Monitor exact settled spend, estimated spend, active pre-egress holds, and unmetered requests. Set hard integer micro-USD budget ceilings and export CSV/JSON chargeback reports.
- **Security:** Review human-in-the-loop (HITL) approvals for destructive MCP calls, trigger or clear 4-level emergency Kill Switches, and verify the cryptographic RFC 8785 JCS audit hash chain.
- **Operations (`Operator` role):** Monitor `/readyz` readiness and cluster snapshot epochs, create encrypted `.tpop.enc` backups, run upgrade compatibility preflight checks, and register HMAC-signed webhooks.

---

## 4. CLI Reference Summary

| Command | Purpose |
| --- | --- |
| `tanpopo serve` | Start the Tanpopo server (`--role all-in-one`, `control-plane`, `data-plane`, or `worker`). |
| `tanpopo doctor` | Verify binary, database connectivity, and storage permissions (`--json` supported). |
| `tanpopo support bundle` | Collect a sanitized diagnostic archive (never includes vault plaintext or prompts). |
| `tanpopo admin reset-owner` | Reset the local organization owner password offline (requires `--yes-i-know-this-resets-owner-password`). |
| `tanpopo backup create / restore` | Create or restore an encrypted `.tpop.enc` backup archive. |
| `tanpopo plugin install / upgrade / rollback / quarantine` | Manage digest-pinned extensions from the command line. |
| `tanpopo upgrade preflight` | Validate installed extension ABI compatibility before upgrading Tanpopo Core. |
| `tanpopo keys / routes / audit / kill-switch` | Operate remote control-plane resources over `/admin/v1`. |
