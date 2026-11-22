# Journey 4: Security, Vault/KMS & MCP/A2A Governance

Tanpopo enforces **defense-in-depth**, **strict Data/Control Plane isolation**, **write-only secret handling**, and **protocol-level governance** across LLM inference, Model Context Protocol (`MCP 2025-03-26`) tools, and Agent-to-Agent (`A2A`) workflows.

---

## 1. Zero-Plaintext Data Plane & Vault / KMS Envelope Hierarchy

- **3-Tier Encryption (`Master KEK -> Org DEK -> Project DEK`):** Secret material stays encrypted at rest and never appears in admin HTTP responses (`UX-GR-01`), support bundles (`tanpopo support-bundle`), or extension manifests.
- **Enterprise KMS & Vault Transit (`TP-IMP-0041`):** Supports local Master KEK (`TANPOPO_MASTER_KEY`), AWS/cloud KMS adapters, and HashiCorp Vault Transit key wrapping plus workload identity federation.
- **Blind Credential Injection (`BlindHostCredentialBroker`):** Data Plane workers resolve encrypted connection credentials into outbound upstream requests only inside policy-evaluated request contexts — extensions cannot read raw provider API keys.
- **SSRF & Data-Residency Fences:** Outbound HTTP dials re-resolve DNS right before socket connection and block RFC 1918, loopback, link-local metadata (`169.254.169.254`), and disallowed egress CIDRs unless explicitly permitted by policy.

---

## 2. Authentication, Four-Eyes Governance & Multi-Org Isolation

- **Enterprise OIDC SSO + CSRF + Concurrency (`TP-IMP-0041`, `TP-IMP-0045`):** Control Plane sessions require `X-CSRF-Token` on mutations and `ETag`/`If-Match` optimistic locking on policy edits (`openapi/admin-v1.yaml`). Virtual Keys (`vk_tp_*`) are always rejected on `/admin/v1/*`.
- **Dual Approval (`Four-Eyes`):** High-impact mutations (KEK rotation, global emergency kill switches, crypto-shredding, elevated organization policies) can require second-operator cryptographic approval before execution.
- **PostgreSQL `FORCE ROW LEVEL SECURITY`:** Organization-scoped queries and connection pools bind session tenant claims inside PostgreSQL transactions so cross-organization reads fail closed.
- **Tamper-Evident Audit Chain & GDPR Crypto-Shredding:**

```bash
# Verify RFC 8785 JCS SHA-256 prev_hash -> entry_hash chain integrity
tanpopo audit verify --output json

# Irreversibly crypto-shred one tenant project's DEK envelopes & secrets
tanpopo tenant shred --org <org_id> --project <project_id> --generation 1
```

---

## 3. MCP (`2025-03-26`) Governance & Tool Pinning

- **JCS Schema Pinning (`INC-06` Protection):** Tool schemas from upstream MCP servers are canonicalized (`RFC 8785`) and SHA-256 pinned at registration. Any unexpected tool description or schema mutation triggers a rug-pull quarantine alert ([monitoring-incidents.md#inc-06](../operations/monitoring-incidents.md#inc-06)) and blocks unapproved execution.
- **Human-in-the-Loop (`HITL`) Approvals:** Destructive or privileged MCP tool calls pause execution for operator approval in the Console **Approvals & Security** workspace before forwarding upstream.
- **Authorized OAuth 2.1 (`PKCE`):** Per-user downstream OAuth tokens remain isolated and transition cleanly to `AUTH_EXPIRED_REAUTH_REQUIRED` (`INC-05`) on revocation rather than falling back to shared static credentials.

---

## 4. A2A Coordinator & Sidecar Isolation (`TP-IMP-0042`)

- **Agent Cards & Task Routing:** Exposes `/.well-known/agent.json` discovery, task queues, and artifact budgets governed by Virtual Keys and project nano-USD limits (`tanpopo budget inspect`).
- **Isolated Sidecars:** External agent workloads run with explicit egress allowlists, per-task budget holds, and 4-level emergency kill switches (`Global`, `Org`, `Project/Env`, `Key/Extension`) inspectable via `tanpopo killswitch list`.

---

## 5. Security Reporting & Compliance (`DEC-004`)

- Vulnerability disclosure policy & severity SLAs: [SECURITY.md](../../SECURITY.md)
- Red-team / static-analysis remediation evidence (`0` High/Critical unresolved): [pentest-v1.0-remediation-report.md](../security/pentest-v1.0-remediation-report.md)
- Hard prohibition on private-session reverse engineering or residential-proxy scraping extensions (`DEC-004`)
