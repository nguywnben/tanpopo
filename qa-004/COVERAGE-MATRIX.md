# QA-004 product coverage matrix

Statuses describe QA-004 evidence only. `PARTIAL` means a named slice was exercised while acceptance remains incomplete. `UNVERIFIED` does not mean pass.

| Area / workflow | Status | Current evidence and gaps |
|---|---|---|
| RC6 artifact provenance and startup | PARTIAL | Run 45581179274 matches frozen SHA. Five archive checksums and two CycloneDX SBOM structures inspected; Linux amd64 executable startup/version and local readiness verified. Other OS/architecture execution and GitHub artifact attestation remain unverified. [CP01](checkpoints/CP01-artifact-install-provenance.md) |
| Fresh install / first owner and tenant | PARTIAL | Real Chromium setup created owner, org, default project and dev environment; setup review showed no route and marked initial connection skipped; completed state reached Overview. Refresh/login persistence and all bootstrap audit details remain to be independently recorded. [CP01](checkpoints/CP01-artifact-install-provenance.md) |
| Extension marketplace unsigned truth/trust | PARTIAL | Authenticated Extensions UI and catalog displayed offline mode, eight marketplace entries and zero installed packages. Detailed catalog signature/origin fields, install/approval, spoof/tamper and trust enforcement unverified. [CP02](checkpoints/CP02-marketplace-native-ingress.md) |
| Native Data Plane route and authorization | PARTIAL | A real Console owner issued a PROVIDER-plane Virtual Key (201). A cookie-bearing request was correctly rejected `invalid_credential_plane` (401); cookie-free bearer request reached Data Plane and returned `no_compliant_deployment_available` (503) on exact RC6. Upstream dispatch, fidelity, streaming, accounting, cancellation, role/deployment matrix remain unverified. [CP02](checkpoints/CP02-marketplace-native-ingress.md) |
| Native dialect coverage (Anthropic, Gemini, Ollama, other declared adapters) | UNVERIFIED | One Anthropic route was tested without a target deployment; successful native request and all supported dialects unverified. |
| Provider adapters / interoperability / fallback | UNVERIFIED | Fresh runtime is `--offline`, has no installed adapters, connections or deployments. Synthetic local provider and full provider lifecycles not yet exercised. |
| FinOps settlement, budgets, exports | UNVERIFIED | No inference settlement generated in QA-004. |
| RBAC and tenant/project isolation | UNVERIFIED | Only fresh Org Owner initial session explored. |
| Audit, crypto-shred, recovery | UNVERIFIED | Fresh bootstrap audit was not yet read back in QA-004; all historical defects reset. |
| MCP / A2A workflows and SSRF | UNVERIFIED | Not executed in QA-004. |
| PostgreSQL / Valkey / distributed deployment | UNVERIFIED | No external database/cache services provisioned yet. |
| OTel / operational reliability / backup restore | UNVERIFIED | Not executed in QA-004. |
| Console screens / async scope / accessibility | PARTIAL | Fresh Overview and Extensions screens visually inspected in Chromium desktop; remainder of Console and responsive/accessibility coverage open. |
| Localization (15 locales) | UNVERIFIED | English only. |
| Chromium / Firefox / WebKit / native mobile | PARTIAL | Real Chromium 1440×1000 exercised. Firefox, WebKit, physical mobile and other platforms not yet exercised in QA-004. |

| Virtual Key create/revoke, credential plane, audit persistence | PARTIAL | Real Owner created PROVIDER key; Admin cookie denied on `/native/*`, cookie-free request reached native handler, key revoked, post-revoke use denied, audit event persisted. API and UI action filter for `virtual_key.created` matched. Rotation, other roles and write paths remain open. [CP02](checkpoints/CP02-marketplace-native-ingress.md), [CP03](checkpoints/CP03-audit-key-state.md) |
| Unsigned extension staging, consent boundary and cleanup | PARTIAL | Unsigned RC6 package stayed pending until human consent, modal disclosed `unverified_external`, no activation occurred, candidate discarded and list returned to zero. Verifier/forgery/security tests remain open. [CP04](checkpoints/CP04-unsigned-package-policy.md) |

## Campaign status

| Status | Count / current interpretation |
|---|---|
| PASS | 1 historical defect retest (`DEF-037`, scoped current RC6 event name and API/UI filtering). |
| PARTIAL | 3 historical defect retests (`DEF-043`, `DEF-048`, `DEF-049`) with narrow evidence and material criteria still open. |
| FAIL | 0 confirmed QA-004 defects to date; this is not evidence of defect absence. |
| BLOCKED | 0 product areas classified blocked so far. Authentic production signing is blocked as a release prerequisite, not a substitute test result. |
| UNVERIFIED | 46 historical defects plus the broad majority of product capabilities. |

The next work prioritizes a complete deterministic provider/native journey, then regression of historical defects and remaining cross-product workflows. No QA-001/002/003 PASS was inherited.
