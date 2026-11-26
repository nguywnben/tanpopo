# CP78 — DEF-013 Ollama connection defaults

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated A2A-enabled RC6 test clone `127.0.0.1:18128`  
**Role/browser:** synthetic Org Owner, real Chromium at 1440×1000  
**Product source modified:** no

## Result

**PASS (scoped) — DEF-013 did not reproduce on exact RC6.** In Providers → New connection, with the sole enabled adapter set to `@tanpopo/ollama`, the form initialized Billing mode to **Self-hosted infra** and Data region to **On-prem**. This agrees with the bundled Ollama manifest description, which explicitly says the adapter defaults to `SELF_HOSTED_INFRA`. The form's scoped project connection list contained two synthetic Ollama connections already classified On-prem/Self-hosted. No connection was created, edited, probed, or persisted during this check.

The private/loopback option was visibly checked with an explicit `127.0.0.1/32` CIDR in the currently selected canary scope. The form disclosed the CIDR requirement and metadata endpoint restriction; no endpoint request occurred. This does not weaken or challenge SSRF controls.

## Reproduction

1. Sign in as the isolated RC6 Org Owner and open **Providers → New connection**.
2. Inspect the initial form with `@tanpopo/ollama` selected, before changing any field.
3. Read the default Billing mode and Data region; compare the billing default with the bundled manifest contract.
4. Cancel without submitting.

The real form screenshot was visually inspected. [Sanitized UI state](../evidence/CP78-provider-registration-state.json) records the form text and the two existing synthetic connection classifications; [screenshot](../evidence/CP78-provider-new-connection.png) shows the defaults. Product source was read only at `extensions/official/ollama/manifest.yaml`.

## Expected versus actual

- **Expected:** Ollama starts with `SELF_HOSTED_INFRA` billing and a local/self-hosted region or explicitly requires a deliberate choice.
- **Actual:** billing defaults to **Self-hosted infra** and region defaults to **On-prem**; the historical commercial API/US default did not appear.
- **Limits:** this verifies the fresh form defaults only. Persisted values after create, alternate regions, reopening/editing, provider adapter switching, and additional self-hosted adapters remain unverified. No new defect is assigned.

QA continues; **GATE-REL-07 remains BLOCKED**.
