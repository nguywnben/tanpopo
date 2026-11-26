# CP39 — Org Owner model creation and persistent display-name check

**Baseline:** exact frozen RC6 artifact, isolated fresh Owner runtime `127.0.0.1:18121`.  
**Result:** Org Owner model create PASS; display-name persistence FAIL (DEF-057).  
**Product code:** unchanged.

## Journey

- Authenticated against the separate fresh-install runtime as `ORG_OWNER` (also holds `OPERATOR`). That database had zero installed provider adapters and zero provider connections.
- In the real Models screen, registered `qa004-owner-embedding-catalog` with display name `QA004 Owner Embeddings Catalog`, 128,000 context tokens, 8,192 max output tokens, and declared `embeddings` capability. The API returned HTTP 201 and the persisted logical-model ID is `lmd_01m89p7txne009dt31cfdvz7t1`.
- Immediately after create, the Console's in-memory model row showed the supplied human display name and the declared embedding capability.
- After full page reload, the Console row showed the canonical slug in both model-name and display-name positions. Authenticated `GET /admin/v1/models` independently returned `display_name` equal to the slug, although the capability remained `embeddings: true`.
- Source inspection corroborates the runtime result: create returns the supplied display name, but the logical-model table has no display-name field and list constructs the DTO with `slug` for both values. Recorded as DEF-057.
- This Owner create also supplies an independent positive control for DEF-056: the Org Owner role can create a model. In the separate 18128 Team Admin runtime, the same documented workflow returns HTTP 403 `insufficient_role_permission`. No provider was installed in 18121, so this checkpoint does not complete embeddings inference.

## Evidence

- `evidence/CP39-owner-model-results.json`
- `evidence/CP39-owner-model-form.png` — pre-submit display name/capability form.
- `evidence/CP39-owner-model-readback.png` — immediate Console state shows the human label.
- `evidence/CP39-owner-model-after-refresh.png` — refreshed Console shows only the slug twice.

## Coverage status

- **PASS, scoped:** Org Owner can create an embeddings-capable logical-model record and the capability persists.
- **FAIL:** logical-model display name does not persist across GET/reload (DEF-057).
- **UNVERIFIED:** Owner deployment creation, adapter installation, provider connection, successful embeddings dispatch, and exact embedding usage settlement.

QA-004 is ongoing. GATE-REL-07 remains BLOCKED.

