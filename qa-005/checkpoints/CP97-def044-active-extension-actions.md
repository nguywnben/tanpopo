# CP97 — DEF-044 active extension action menu

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite `127.0.0.1:18162`; Chromium 156; Owner `qa005-owner@example.invalid`; QA organization/project/environment selected.

## Result

**PARTIAL (fresh read-only DEF-044 retest).** The actual Installed screen listed two active official declarative adapters, `@tanpopo/openai-compatible` and `@tanpopo/ollama`. The Ollama row's real Actions menu offered Review permissions, Upgrade package, a disabled Rollback version item with “No previous pin exists. Use Quarantine for emergency cutoff,” and Quarantine package. It did **not** offer Uninstall package. This means the previously reported active-state dead-end action is no longer presented. The screenshot was visually inspected.

## Reproduction

1. Sign in to the exact-source Console and open Extensions → Installed.
2. Verify the active extension rows against `GET /admin/v1/extensions/installations` (HTTP 200, two rows, both `ENABLED_ACTIVE`, signature verified, declarative execution tier).
3. Open Actions on `@tanpopo/ollama` without choosing a lifecycle action.
4. Verify the menu contains Review permissions, Upgrade package, disabled Rollback with no-previous-pin guidance, and Quarantine package, but no Uninstall action.

Visually inspected screenshots: [active inventory](../evidence/screenshots/CP97-extension-inventory.png) and [active action menu](../evidence/screenshots/CP97-def044-active-extension-actions.png). Sanitized record: [CP97 JSON](../evidence/CP97-def044-active-extension-actions.json).

## Limits and reconciliation

This is a read-only Owner/SQLite/Chromium observation. No quarantine, uninstall, API write, provider call or paid usage occurred. The required transition is not explicit: the menu offers Quarantine but does not explain that quarantine is a prerequisite to uninstall or point to the resulting removal action. This runtime contains only active official adapters; taking one through quarantine to verify the subsequent uninstall menu would disable a shared QA provider path. A purpose-built isolated extension-installation clone is currently blocked by the full container overlay; clone startup failed with `ENOSPC` and only 0.6 MB was available before the attempt. A subsequent cache cleanup could not remove files owned by `nobody`; it recovered 128 MB but the clone was not retried. Therefore the exact post-quarantine flow, state/audit persistence, kill-switch cleanup and other lifecycle states remain UNVERIFIED. Historical QA-003 failure remains preserved; do not close DEF-044. `GATE-REL-07` remains BLOCKED.
