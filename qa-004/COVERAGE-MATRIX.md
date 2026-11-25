# QA-004 product coverage matrix

Statuses describe QA-004 evidence only. `PARTIAL` means a named slice was exercised while acceptance remains incomplete. `UNVERIFIED` does not mean pass.

| Area / workflow | Status | Current evidence and gaps |
|---|---|---|
| RC6 artifact provenance and startup | PARTIAL | Run 45581179274 matches frozen SHA. Five archive checksums and two CycloneDX SBOM structures inspected; Linux amd64 executable startup/version and local readiness verified. Other OS/architecture execution and GitHub artifact attestation remain unverified. [CP01](checkpoints/CP01-artifact-install-provenance.md) |
| Fresh install / first owner and tenant | PARTIAL | Real Chromium setup created owner, org, default project and dev environment; setup review showed no route and marked initial connection skipped; completed state reached Overview. Refresh/login persistence and all bootstrap audit details remain to be independently recorded. [CP01](checkpoints/CP01-artifact-install-provenance.md) |
| Extension marketplace unsigned truth/trust | PARTIAL | Authenticated Extensions UI and catalog displayed offline mode, eight marketplace entries and zero installed packages. Detailed catalog signature/origin fields, install/approval, spoof/tamper and trust enforcement unverified. [CP02](checkpoints/CP02-marketplace-native-ingress.md) |
| Native Data Plane route and authorization | PARTIAL | Real PROVIDER-plane key and cookie-free request reached a synthetic Ollama upstream on an isolated test-root runtime; native JSON and SSE success were verified, persistence and revoked-key boundary were exercised. One unauthorized Admin-cookie request was rejected. Other roles, dialects, cancellation, failure/retry, runtime roles and deployment modes remain open. [CP02](checkpoints/CP02-marketplace-native-ingress.md), [CP07](checkpoints/CP07-native-ollama-end-to-end.md) |
| Native dialect coverage (Anthropic, Gemini, Ollama, other declared adapters) | PARTIAL | Ollama native JSON plus OpenAI-compatible SSE path through the Ollama adapter succeeded with a synthetic endpoint. Ollama native counter extraction failed (DEF-051); other dialects, native streaming variants, multimodal and tool calling remain unverified. |
| Provider adapters / interoperability / fallback | PARTIAL | Test-only ephemeral Ed25519 root verified and approved the exact bundled Ollama package in a separate runtime; never treated as a production Tanpopo signature. Loopback provider create/probe -> model -> healthy deployment -> route -> inference succeeded. Multi-provider, fallback, retries, timeout, cancellation, embeddings, tool calls and external integrations remain unverified. |
| FinOps settlement, budgets, exports | FAIL / PARTIAL | Three synthetic native settlements persisted. DEF-051 exact Ollama counters ignored; DEF-052 trace projection disagrees with ledger/FinOps; DEF-053 rate-unit contract mismatch causes underbilling. Independently created and refreshed first monthly project budget; exact 1-micro cap blocked the next call before mock upstream; three actual CSV/JSON exports reconciled under Project/Model. Other budget tiers, period edges, duplicates, warning thresholds, exports across Environment/Virtual Key, pagination, and broad chargeback remain open. [CP07](checkpoints/CP07-native-ollama-end-to-end.md), [CP08](checkpoints/CP08-finops-budgets-and-exports.md) |
| RBAC and tenant/project isolation | UNVERIFIED | Only fresh Org Owner initial session explored. |
| Audit, crypto-shred, recovery | UNVERIFIED | Fresh bootstrap audit was not yet read back in QA-004; all historical defects reset. |
| MCP / A2A workflows and SSRF | UNVERIFIED | Not executed in QA-004. |
| PostgreSQL / Valkey / distributed deployment | UNVERIFIED | No external database/cache services provisioned yet. |
| OTel / operational reliability / backup restore | UNVERIFIED | Not executed in QA-004. |
| Console screens / async scope / accessibility | PARTIAL | Fresh Overview, Extensions, Providers, Models, Routes, Traces, and FinOps screens interacted with in Chromium desktop. Screenshots were visually inspected. Most Console screens, async races, RBAC, responsive, accessibility, localization and browser compatibility remain open. |
| Localization (15 locales) | UNVERIFIED | English only. |
| Chromium / Firefox / WebKit / native mobile | PARTIAL | Real Chromium 1440×1000 exercised. Firefox, WebKit, physical mobile and other platforms not yet exercised in QA-004. |

| Virtual Key create/revoke, credential plane, audit persistence | PARTIAL | Real Owner created PROVIDER key; Admin cookie denied on `/native/*`, cookie-free request reached native handler, key revoked, post-revoke use denied, audit event persisted. API and UI action filter for `virtual_key.created` matched. Rotation, other roles and write paths remain open. [CP02](checkpoints/CP02-marketplace-native-ingress.md), [CP03](checkpoints/CP03-audit-key-state.md) |
| Unsigned extension staging, consent boundary and cleanup | PARTIAL | Unsigned RC6 package stayed pending until human consent, modal disclosed `unverified_external`, no activation occurred, candidate discarded and list returned to zero. Verifier/forgery/security tests remain open. [CP04](checkpoints/CP04-unsigned-package-policy.md) |

## Campaign status

| Status | Count / current interpretation |
|---|---|
| PASS | 5 historical defect retests (`DEF-003`, `DEF-012`, `DEF-014`, `DEF-015`, `DEF-037`), each scoped to explicitly listed behavior. |
| PARTIAL | 3 historical defect retests (`DEF-043`, `DEF-048`, `DEF-049`) with narrow evidence and material criteria still open. |
| FAIL | 3 confirmed QA-004 defects (DEF-051 through DEF-053); see register. |
| BLOCKED | 0 product areas classified blocked so far. Authentic production signing is blocked as a release prerequisite, not a substitute test result. |
| UNVERIFIED | 42 historical defects plus the broad majority of product capabilities. |

QA-004 has completed a real-browser synthetic Ollama journey plus first-project-budget, limit enforcement, and selected chargeback-export slices. Three new defects remain confirmed. Five historical findings have narrow QA-004 PASS outcomes; none were inherited. This is a PARTIAL campaign, not product acceptance. Prior-usage values absent from a budget row created after those requests remain an unclassified observation pending a clear contract and fresh test. The next work prioritizes unresolved major historical findings and broader independent product workflows.
