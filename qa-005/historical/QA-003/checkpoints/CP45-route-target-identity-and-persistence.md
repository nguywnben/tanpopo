# CP45 — Duplicate deployment identity in route workflows

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`<br>
**Runtime:** isolated QA RC5 service on `18116`, synthetic Owner, Chromium 156/Linux at 1440×1000<br>
**Disposition:** DEF-027 is **PARTIAL**; route selection and persistence were exercised, but the historical three-healthy-target/failover acceptance is not reproduced. `FT-10` and `SCR-08` remain **PARTIAL**. QA-003 remains in progress; `GATE-REL-07` remains **BLOCKED**.

## Setup

The populated `default/dev` Models page started with one synthetic deployment and one logical model. Through the real Models UI, an Owner registered a second zero-rate deployment against the same logical model and same upstream model name (`qa003-synthetic-unauthorized`) using a separate synthetic `SELF_HOSTED_INFRA` connection. This was isolated QA data; no provider request or inference was sent. Both deployments remained `PROVISIONED`.

The authenticated deployment list returned HTTP 200 and persisted two distinct deployment IDs for the same logical model and environment. Sanitized IDs are `dpl_…rj9pcy` (connection “QA003 SSRF redirect boundary”) and `dpl_…0js4t4` (connection “QA003 RC5 Ollama 401”). See [result](../evidence/CP45-def027-result.json) and the visually inspected [deployment table](../evidence/CP45-duplicate-deployments-persisted.png).

## Route workflow and results

1. In Routes & Policies, the Owner opened Create route. The deployment combobox showed two choices with identical upstream model names but distinct connection names and short deployment IDs; each also displayed residency and `Provisioned` state. The visible labels were visually inspected in [route target options](../evidence/CP45-duplicate-route-target-options.png).
2. The Owner selected the `QA003 RC5 Ollama 401` target. The route draft repeated the selected connection, model and ID suffix. The form displayed a warning that the target did not declare `tool_calling`; this was advisory and did not block save. The actual Create route action succeeded.
3. The Console showed a success message and one saved route. Authenticated API readback returned the route and exact target `deployment_id`; the UI fallback chain displayed the connection, model and short ID. The [route workflow evidence](../evidence/CP45-route-lifecycle-result.json), [selected target screenshot](../evidence/CP45-route-target-selected.png), and [saved route screenshot](../evidence/CP45-route-created.png) were inspected.

No inference, provider call, health promotion, deployment mutation or route invocation was performed. This verified identity rendering and route persistence only.

## Defect and coverage disposition

QA-002 CP52 confirmed DEF-027 with three distinct healthy deployments sharing `qa002:latest`: target option labels and route/fallback summary were identical, persisted IDs mapped to different connections, and controlled failover showed first 503 then second 200. CP45 does **not** inherit or overturn that evidence. With two RC5 `PROVISIONED` deployments sharing a model name but using different connections, the current combobox, selected target card and saved route summary all visibly disambiguated connection and short ID. Therefore DEF-027 changes from **UNVERIFIED** to **PARTIAL — historical failure not reproduced in this scoped two-target case**. Three healthy duplicates, route editing/reordering, exact summary under multiple targets, real health/failover, role coverage and cross-scope behavior remain **UNVERIFIED**.

The `tool_calling` warning did not prevent this non-invoked route from being created. The warning's intended blocking/advisory contract is not established here; this is recorded as an observation, not a new defect. No product source was modified.

## QA status

- Historical defects: 17 verified fixed for scoped RC5 tests, 9 partial, 13 unverified. No prior-campaign PASS is inherited.
- New defects DEF-040–046 remain open; no new finding was confirmed in this phase.
- `FT-10` and `SCR-08` remain partial; actual healthy execution/failover and Route Explain are still open.
- `GATE-REL-07` remains **BLOCKED**.
