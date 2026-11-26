# CP26 — MCP schema change re-locks a previously pinned tool

**Source/runtime:** exact RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`, local binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`, SQLite `127.0.0.1:18145`. Real Chromium/Playwright Console action. The only upstream was the loopback synthetic MCP fixture.

## Result: PASS (scoped schema drift guard)

After CP25 pinned a synthetic read-only tool schema, changed only the mock tool description, producing a different JCS schema hash, then clicked Sync tools in MCP Hub.

- Sync API returned HTTP 200, `synced_count: 1`, `drifted_count: 1`.
- Observed schema hash changed from `efc8ee5a…a9a28` to `6bd0f6a4…79e1d`.
- Persisted tool version advanced to 3 and `drift_locked` became true; server state showed `SCHEMA_DRIFT_LOCKED`.
- The actual screenshot displays “Schema Drift Locked” and “Drift locked”; it was visually inspected.

This is a scoped positive security regression: a post-pin upstream schema change is surfaced and locked instead of silently accepted. It does not cover attacker-controlled schema forms, drift during active calls, competing syncs, repin authorization separation, or data-plane call denial while an existing valid key/session remains active (the test key had already been revoked).

Sanitized API/UI evidence: `evidence/CP26-mcp-schema-drift.json`; screenshot `evidence/screenshots/CP26-mcp-schema-drift-locked.png`.
