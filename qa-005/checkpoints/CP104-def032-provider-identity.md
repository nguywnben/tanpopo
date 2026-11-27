# CP104 — DEF-032 provider connection identity

**Date:** 2026-11-27  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b` (unsigned RC7)  
**Runtime:** exact-source Linux binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite at `18145`, viewed through TLS reverse proxy `https://127.0.0.1:19444`  
**Browser:** Chromium 156, real Playwright interaction, Owner role, 1440×1000  
**Disposition:** **PARTIAL**; no duplicate display names existed in the current inventory, so historical same-name ambiguity was not recreated.

## Reproduction and observations

1. Sign in as the isolated QA Owner and open Providers → Connections.
2. Read the rendered inventory and authenticated `GET /admin/v1/connections?limit=100` response. Both returned HTTP 200. The inventory contained five records and displayed each connection name followed by an 8-character stable ID suffix. The API IDs were distinct.
3. Open the workspace selector and inspect the inventory under both `QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev` and `QA005 Org / Initial Project / dev`. This Owner list still contained the same five organization-visible entries; no scope contract is inferred from this behavior.
4. Open Edit for `QA005 budgetless Ollama (shr38yc9)`. The drawer heading retained the same stable suffix. Cancel the drawer.

The current fixture contains two records with the same adapter and loopback host (`127.0.0.1:19096`) but distinct names, short IDs, and project IDs. It contains no pair with identical display names. No connection was probed, disabled, deleted, or changed; no provider request was sent and no secret was exposed. Real screenshots were visually inspected.

## Result and limits

The list and edit drawer now expose stable connection identity, addressing the historical list/edit ambiguity for the tested records. Retain **PARTIAL** because the QA-002 same-name pair could not be reproduced and the Probe, Disable, Delete action menus/confirmation dialogs and post-action target identity were not exercised. Do not close DEF-032 until those cases are tested with two disposable records that share a display name or the exact historical fixture is restored.

Evidence: [sanitized result](../evidence/CP104-def032-provider-identity.json), [inventory](../evidence/screenshots/CP104-provider-connections-inventory.png), [scope picker](../evidence/screenshots/CP104-provider-scope-picker.png), [edit drawer](../evidence/screenshots/CP104-provider-edit-identity.png), [initial project view](../evidence/screenshots/CP104-provider-connections-initial-project.png).
