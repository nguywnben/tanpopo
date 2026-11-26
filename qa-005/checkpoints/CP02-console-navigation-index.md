# CP02 — Primary Console navigation evidence index

Exact source commit: `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.

Real Chromium session: owner login at `http://127.0.0.1:18145/login`, then sidebar link clicks. Every screenshot captured at viewport 1440×1000 with full page. Observed Admin API calls were 2xx (all specifically 200 for GET), with no `pageerror` events.

| Page | Evidence screenshot | Rendered state |
|---|---|---|
| Overview | `../evidence/screenshots/CP02-02-overview.png` | Empty tenant metrics, no alerts |
| Traces | `../evidence/screenshots/CP02-03-traces.png` | Metadata-only empty trace state |
| FinOps | `../evidence/screenshots/CP02-04-finops.png` | Zero accounting, no budgets |
| Providers | `../evidence/screenshots/CP02-05-providers.png` | No connections/secrets |
| Models | `../evidence/screenshots/CP02-06-models.png` | No models/deployments |
| Routes & Policies | `../evidence/screenshots/CP02-07-routes-policies.png` | No routes; policy ceiling shown |
| Extensions | `../evidence/screenshots/CP02-08-extensions.png` | 8 Marketplace catalog entries, none installed |
| MCP Hub | `../evidence/screenshots/CP02-09-mcp-hub.png` | No MCP server or A2A agent/task |
| Playground | `../evidence/screenshots/CP02-10-playground.png` | No turns; model/route selectors |
| Access & Keys | `../evidence/screenshots/CP02-11-access-keys.png` | Owner member; no Virtual Keys |
| Security | `../evidence/screenshots/CP02-12-security.png` | No pending approvals; audit surface present |
| Operations | `../evidence/screenshots/CP02-13-operations.png` | SQLite readiness and one local node; additional tabs not exercised |

This index records only route rendering, not feature acceptance. See CP01–02 for limitations and environment details.
