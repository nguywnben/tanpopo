# Canonical Product Glossary — Tanpopo

> **Business Analysis Document (Phase ASTRA A02 — `DRAFT / UNLOCKED SCOPE`)**
> - **Version:** `0.1.0-a02-draft`
> - **Last Updated:** `2026-11-11`
> - **Authored By:** Lead Business Analyst & Product Architect
> - **Governance Status:** **Unlocked-scope draft (`DRAFT`)** because **Gate G1** in [`docs/governance/decisions.md`](../governance/decisions.md) is currently in `READY_FOR_REVIEW` status (awaiting formal Product Owner approval). Core definitions originating from the foundational vision retain the `CONFIRMED_BY_OWNER` tag; detailed domain-modeling concepts carry the `PROPOSED` tag.

---

## 1. Immutable Terminology Principles (`CONFIRMED_BY_OWNER`)

1. **Four independent conceptual layers (`DEC-003`):** `Provider`, `Plugin (Extension)`, `MCP`, and `A2A` are four distinct entities with completely separate natures, lifecycles, and authorization boundaries — using them interchangeably as synonyms in documentation, UI, or APIs is strictly prohibited.
2. **Four decoupled entities in the model connectivity chain (`PROPOSED`):** `Provider Adapter` (integration software package) ≠ `Provider Connection` (specific account/credential binding) ≠ `Model Deployment` (concrete operational serving target of a model on a specific connection/region) ≠ `Model` (logical model capability specification).
3. **Technical mechanism ≠ Legal usage right (`DEC-004`):** Whether an authentication method or plugin *can technically execute* (Technical Capability) is completely separate from whether the upstream service provider *legally/contractually authorizes* that behavior under its terms (Provider Entitlement).

---

## 2. Actor Terminology

| Canonical Term | Business Definition in Tanpopo |
|---|---|
| **Personal Admin** | The sole administrator in a single-user self-hosted deployment, holding full operational authority over the server, plugin installation, provider connections, and personal budget management. |
| **Organization Owner** | The highest-level owner of an Organization (`Organization / Tenant`), bearing ultimate responsibility for compliance policies, aggregate budget ceilings, plugin trust rules, data retention/erasure, and organization-wide emergency kill-switches. |
| **Team Admin** | A Project/Team-level administrator (`Project / Team`) delegated to manage team members, virtual keys (`Virtual Key`), routing rules (`Route`), team budgets, and connections within an assigned project scope. |
| **Developer** | An engineer building applications or configuring AI agents; consumes authorized connections, routes, and MCP tools via virtual keys, runs experiments in the Playground, and inspects error/trace logs for their assigned project. |
| **API Client** | Application software, backend services, IDEs, or CLI tools invoking Tanpopo API endpoints programmatically using machine credentials (`Virtual Key` or service identity). |
| **Extension Publisher** | An individual, partner AI provider, official Tanpopo maintainer team, or internal enterprise platform team that packages, declares permissions for, and publishes extensions (`Extension Package`). |
| **Reviewer** | An independent review actor in two contexts: (1) *Marketplace Reviewer*, auditing compliance and compatibility for public `verified` packages, and (2) *Internal Governance Approver*, approving new plugin permissions or sensitive operations (such as invoking destructive MCP tools) inside an organization. |
| **Operator** | The infrastructure administrator operating the Tanpopo instance (`Instance Operator / SRE`), responsible for installation, version upgrades, backups, disaster recovery, system health monitoring, and root secret vault protection. |
| **MCP Client** | An application, IDE, or agent framework acting as a Model Context Protocol consumer, sending discovery and invocation requests for tools, resources, or prompt templates through Tanpopo's MCP Plane. |
| **MCP Server** | An upstream target service exposing tool execution (`Tools`), data resources (`Resources`), or prompt templates (`Prompts`) conforming to the Model Context Protocol behind Tanpopo. |
| **Agent** | An autonomous or semi-autonomous software actor capable of multi-step planning (combining model inference and MCP tool calls) or delegating/accepting asynchronous tasks across the A2A Plane. |
| **External Identity Provider (IdP)** | An external authentication system (such as enterprise OIDC/SAML or an official OAuth 2.1 authorization server) that verifies user or service identities or issues authorized delegated grants. |
| **External AI Provider** | An external commercial vendor or self-hosted model inference cluster providing AI model services subject to its own terms of service, rate limits, and pricing schedules. |

---

## 3. Business Architecture & Connectivity Chain Terminology

| Canonical Term | Business Definition & Core Distinction |
|---|---|
| **Instance** | An independently operated self-hosted installation of Tanpopo (hosting either a single organization or multiple isolated organizations depending on administrative configuration). |
| **Organization (Tenant)** | The highest-level isolation boundary for data, financial ledgers, credentials, and audit trails belonging to a legal entity or autonomous business unit. Data and secrets are never shared automatically across two `Organization` boundaries. |
| **Project (Team / Workspace)** | A business organizational unit within an `Organization` that groups applications, members, virtual keys, routes, and budget quotas for a specific product or department. |
| **Environment** | An operational lifecycle partition within a `Project` (typically `development`, `staging`, `production`) with isolated virtual keys, safety policies, and cost ceilings. |
| **Principal** | An authenticated identity recognized by the system when performing administrative actions or invoking APIs (a human user, service account, or identified agent). |
| **Virtual Key** | An access token issued by Tanpopo to an `API Client` / `Developer` / `Agent`, strictly bound to an `Environment`/`Project` with explicit permissions and quotas, completely shielding upstream provider credentials from exposure. |
| **Credential (Secret Binding)** | Secret material used to authenticate against an upstream target system (commercial API keys, authorized OAuth token sets, cloud IAM/OIDC identities, mTLS certificates) vaulted and purpose-enforced by Tanpopo. |
| **Provider** | A service entity delivering AI model inference capabilities (text, image, audio, embeddings, reranking, etc.). |
| **Provider Adapter** | A specialized `Extension` containing protocol translation rules/code, model catalog mappings, error normalization, and usage extraction logic for a specific `Provider` family. **An Adapter is a static software package and never contains customer secret keys.** |
| **Provider Connection** | A live operational connection configuration binding a `Provider Adapter` to a specific `Credential` and network endpoint (e.g., *"Product Team A Commercial OpenAI Account"*). |
| **Model** | A logical catalog-level AI model concept accompanied by a `Capability Profile` (context length, tool-calling support, vision, reasoning, etc.), independent of which cloud or local provider serves it. |
| **Model Deployment** | A concrete model serving target bound to a `Provider Connection`, geographic `Data Region`, pricing schedule, and independent health state (e.g., the same logical `Claude Sonnet` model backed by two distinct `Deployment` targets on `Bedrock EU` and `Anthropic US`). |
| **Route** | A traffic orchestration rule mapping a virtual model alias (or request condition) to a prioritized list of `Model Deployment` targets that satisfy capability and security policy constraints. |
| **Policy** | A set of governance constraints enforced over runtime traffic or administrative operations (access control, data residency, rate limiting, MCP tool filtering, manual approvals, fail-closed/fail-open behavior). |

---

## 4. Extension Ecosystem Terminology (Plugins / Extensions)

| Canonical Term | Business Definition |
|---|---|
| **Extension (Plugin)** | An independently packaged extensibility unit governed by an explicit `Contract`, declared `Permissions`, and a managed lifecycle to add a `Provider Adapter`, policy filter, authentication mechanism, or telemetry exporter without updating Tanpopo Core. |
| **Extension Manifest** | An immutable metadata declaration bundled with every plugin version specifying its identifier, compatible contract version, provided capabilities, and minimum required permissions (such as allowed target network hosts and credential binding types). |
| **Trust Tier (Provenance Trust Level)** | A transparent provenance classification label assigned to an extension:<br>- `official`: Directly developed, verified for official API compliance, and maintained by the Tanpopo project.<br>- `verified`: Developed by partners or the community, having passed automated compatibility tests and terms-of-service compliance review on the official catalog.<br>- `unverified-external`: Installed directly by an operator from Git/URL/archive/private registry outside Tanpopo's verification scope. |
| **Permission Escalation** | A condition in which a newer version of an extension requests broader permissions (additional target network hosts, access to new credential types, etc.) than the grant previously approved by an administrator. |
| **Quarantine (Emergency Isolation)** | An immediate forced isolation state revoking execution and network privileges of an installed plugin upon detecting a security vulnerability, privilege violation, or emergency revocation advisory, while preserving forensic audit trails. |

---

## 5. MCP, A2A, Financial Operations (FinOps) & Compliance Terminology

| Canonical Term | Business Definition |
|---|---|
| **MCP Plane** | The dedicated governance plane responsible for registering `MCP Server` endpoints, filtering tool catalogs by caller permissions, verifying tool description integrity, enforcing human approval on sensitive operations, and recording tool execution audit logs. |
| **Tool Risk Class** | The side-effect classification of an MCP tool: `read-only` (pure reads without mutating external state), `mutating` (standard data creation/modification), and `destructive` (data deletion, financial transfers, system privilege changes, or irreversible operations). |
| **Schema Hash Pinning** | A cryptographic fingerprinting mechanism that locks an MCP tool's description and parameter schema at approval time to detect and block unauthorized silent upstream modifications (*MCP Rug-Pull*). |
| **A2A Plane** | The dedicated governance plane responsible for agent capability profile discovery (`Agent Card`), bounded multi-step asynchronous task delegation, and cross-agent state/budget tracking. |
| **Provider Entitlement** | The lawful usage right granted by an upstream service provider under a commercial contract or API terms of service (e.g., using pay-as-you-go commercial API keys for server-side applications). |
| **Consumer Subscription** | A flat-rate personal subscription plan intended for end users interacting directly through first-party interfaces (e.g., ChatGPT Plus/Pro, personal Codex, Claude Pro/Max, Google Antigravity...). **Not a `Provider Entitlement` for a shared API gateway.** |
| **Pre-Request Budget Reservation** | The financial control operation that places a temporary hold for the estimated maximum request cost prior to forwarding traffic upstream, preventing concurrent streams from breaching hard budget ceilings. |
| **Settlement (Usage Settlement)** | The final cost reconciliation and ledger posting step once a request completes or terminates early: releasing unused held quota and recording accounting precision (`EXACT`, `ESTIMATED`, or `UNMETERED_FLAGGED`). |
| **Partial Stream** | A real-time streaming response state in which at least one content or tool-call chunk has already been flushed to the `API Client` prior to an upstream interruption or failure. |
| **Fail-Closed vs Fail-Open** | Two error-handling modes when a policy evaluation component fails or finds no compliant target: `Fail-Closed` rejects the request to strictly preserve security/compliance; `Fail-Open` allows the request to proceed with a warning to prioritize availability on non-critical auxiliary checks. |
