# CP40 — Instance-only Operator and hostname-resolved private MCP addresses

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Result:** PASS for the scoped instance-Operator read and `localhost` CIDR boundary; other RBAC/SSRF cases remain UNVERIFIED.  
**Source changes:** none. Real Chromium interactions; screenshots were inspected. Disposable SQLite and synthetic local MCP fixtures only.

## Instance-only Operator

- Created a synthetic local HUMAN principal with only the INSTANCE-scoped `OPERATOR` role and authenticated through the real Sign in form in a fresh browser context. `/admin/v1/auth/session` returned `roles: ["OPERATOR"]`.
- The runtime selected the existing CP39 synthetic organization/project/environment as active workspace. The Operator opened MCP Hub and Access & Keys → Members & Roles. The member page displayed the one synthetic organization owner. The UI request included `X-Tanpopo-Org-ID` and returned 200. The separate direct GET without selected-organization context returned 403, so it is not evidence of an Operator role denial.
- Visually inspected `../evidence/CP40-operator-dashboard.png`, `CP40-operator-members.png`, and `CP40-operator-mcp-hub.png`. The MCP Hub showed an empty registry before the new test records. This tests read presentation only; Operator mutation boundaries and other organizations remain UNVERIFIED.

## Hostname and resolved-address CIDR cases

- UI-registered synthetic Streamable HTTP MCP endpoints with URL `http://localhost:<port>/mcp`, private/loopback enabled, and explicit CIDR lists. The local system resolver returned `::1` and `127.0.0.1` for `localhost`.
- A controlled dual-stack HTTP fixture on port 19115 accepted both loopbacks and recorded the actual peer and protocol methods. An IPv6 fixture on `::1` confirmed `initialize`, `notifications/initialized`, and `tools/list` on the selected address.
- **Mismatched control:** allowed CIDR `127.0.0.2/32` against the localhost endpoint. UI sync returned HTTP 502; the v4 fixture logged zero requests; SQLite persisted zero discovered tools.
- **Both addresses allowed:** `127.0.0.1/32, ::1/128`. UI sync returned 200, discovered one tool and recorded all three protocol requests from peer `::1`.
- **IPv4-only CIDR:** `127.0.0.1/32`. Although that address was in the resolved set, the selected resolver peer was `::1`; sync returned 502 and the fixture logged zero requests. This is consistent with fail-closed checking of the selected address.
- **IPv6-only CIDR:** `::1/128`. Sync returned 200; the fixture observed peer `::1` and the three expected MCP negotiation requests. The UI showed the first-sync schema as `Schema Drift Locked`, which is the expected review gate before pinning, not a defect.
- Persisted rows and sanitized results are in `../evidence/CP40-operator-and-dns-allowlist.json`. `CP40-mcp-host-deny-sync.png`, `CP40-mcp-host-allow-sync-retry.png`, `CP40-mcp-host-v4-only-sync.png`, and `CP40-mcp-host-v6-sync.png` were visually inspected.

## Classification and limits

- **DEF-005:** extended PASS for one hostname resolving to IPv4 and IPv6 loopbacks. A CIDR mismatch did not contact the fixture; an explicitly permitted selected IPv6 address did. DEF-005 remains partial overall: DNS rebinding/address changes, redirects, mixed public/private records, IPv4-mapped forms, non-loopback networks, other roles, and other runtimes remain UNVERIFIED.
- Failed syncs still produce a generic `upstream MCP tool synchronization failed` banner and leave the registry row Active. This repeats the unresolved QA-OBS-004 behavior; no new defect classification is made from this test.
- The fixture and test account are synthetic. The Operator principal is suspended and its password hash cleared after testing; its temporary plaintext password and fixture scripts are removed. The registered synthetic MCP rows remain only in the isolated QA database for evidence traceability.

GATE-REL-07 remains **BLOCKED**. QA-002 continues.
