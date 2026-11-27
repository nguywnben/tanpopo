# CP103 — Alternate-runtime private CIDR allow and deny controls

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`<br>
**Runtime:** isolated QA runtime `18086`<br>
**Role/browser:** authorized synthetic Org Owner, real Chromium at 1440 × 900<br>
**Fixture:** local-only synthetic Streamable HTTP endpoint at `127.0.0.1:19202`; no external endpoint was contacted<br>
**Product implementation:** unchanged

## Reproduction and results

Opened MCP Hub in the actual Console. The UI exposed the explicit private/loopback toggle and required CIDR field. A first exploratory submit with the CIDR field empty was rejected in the browser with “Private upstream access requires explicit non-wildcard CIDRs”; it made no API call and the fixture received zero traffic. This required-field validation is not counted as a backend SSRF result.

Registered two isolated synthetic server records pointing to the same local fixture:

1. `qa002-cp103-cidr-deny`, allowlist `127.0.0.2/32` while the fixture resolves to `127.0.0.1`. The Console sent the exact CIDR in `POST /admin/v1/mcp/servers`; API returned 201 and persisted the configuration. On **Sync tools**, API returned HTTP 502 `mcp_sync_failed`; the UI showed “upstream MCP tool synchronization failed.” The fixture request count remained zero and the record had zero tools.
2. `qa002-cp103-cidr-allow`, allowlist `127.0.0.1/32` for that same fixture. Registration returned 201. On **Sync tools**, API returned 200 and one synthetic tool. The fixture recorded exactly `initialize`, `notifications/initialized`, and `tools/list`.

This same-runtime, same-fixture positive control demonstrates that the mismatch failure was not caused by the runtime being unable to reach the fixture. The rejected target received no request; the explicitly allowed target was contacted. This is **PASS for the tested private CIDR boundary on alternate isolated runtime 18086** and supports treating the rejection as the intended security restriction, not a product defect. The UI’s generic 502 diagnosis repeats the existing [QA-OBS-004](../reports/QA-OBS-004-mcp-allowlist-failure-diagnostic.md); no new defect is assigned.

After a hard refresh, the authenticated API returned both persisted records. The allowed record remained `SCHEMA_DRIFT_LOCKED` with one tool; the mismatched record remained `ACTIVE` with zero tools. The real refreshed screen was visually inspected. No schema was approved and no tool was granted to a key. The isolated runtime provides no supported MCP delete action, so the two QA-only records remain in its test database; the mock listener is stopped after this phase.

## Evidence

- `../evidence/CP103-18086-mcp-private-cidr-allow-form.png` — actual private endpoint form with explicit matching `/32`, visually inspected.
- `../evidence/CP103-18086-mcp-cidr-allow-created.json` — real registration request payload/201 and persisted list summary.
- `../evidence/CP103-18086-mcp-cidr-denied.json` — mismatched CIDR registration request payload/201, zero tools/persistence summary, and no fixture call before sync.
- `../evidence/CP103-18086-mcp-cidr-sync-denied.json` — mismatch sync HTTP 502 and zero fixture requests; request response is sanitized.
- `../evidence/CP103-18086-mcp-cidr-allow-created.png` and `CP103-18086-mcp-cidr-denied.png` — both registration results before sync, visually inspected; each shows zero tools.
- `../evidence/CP103-18086-mcp-register-form.png` and `CP103-18086-mcp-private-cidr-form.png` — initial and empty-CIDR form states; the empty-field attempt was client-side required-field validation only, with no API call or fixture traffic. Visually inspected.
- `../evidence/CP103-18086-mcp-cidr-allow-synced.json` — matching sync HTTP 200 and fixture call sequence.
- `../evidence/CP103-18086-mcp-cidr-allow-synced.png` — successful sync and schema lock, visually inspected.
- `../evidence/CP103-18086-mcp-cidr-sync-denied.png` — rejected sync, zero-tool state and generic error, visually inspected.
- `../evidence/CP103-18086-mcp-persistence-after-refresh.json` and `.png` — both server states after full page refresh; screenshot visually inspected.

All evidence uses synthetic local-only data, contains no credentials, and is retained under the QA record.

## Consolidated status summary

- **Historical defects verified fixed:** DEF-001–004, DEF-006 and DEF-008–015 retain only the scoped QA-002 results already recorded; no QA-001 PASS is inherited. DEF-005 remains PARTIAL overall; DEF-007 failure atomicity and populated erasure remain UNVERIFIED.
- **Historical defects failing or unverified:** DEF-016–036 remain open; CP101 most recently reconfirmed DEF-023. CP103 does not change their disposition.
- **New defects:** none. The allowed-versus-mismatched result supports the security boundary. Generic sync diagnostics remain the existing QA-OBS-004 observation.
- **Coverage completed:** actual alternate-runtime MCP form, explicit CIDR submission, mismatched-address rejection before fixture contact, matching-address positive control, 201/502/200 API outcomes, tool persistence, refresh recovery, and screenshot inspection.
- **Coverage blocked / remaining:** DEF-005 still needs DNS rebinding, mixed/multiple answers, redirect chains and status coverage, alternate hostname/resolver races, other transports, other role/browser/platform combinations, and additional integration paths. No A2A CIDR negative/positive pair was run in this phase.
- **Recommended engineering handoffs:** retain the DEF-005 rule that enforcement must reject addresses outside explicit CIDRs; improve sync diagnostics only through the existing QA-OBS-004/product contract review. Do not broaden CIDRs or weaken authorization/SSRF checks.
- **Release gate:** `GATE-REL-07` remains **BLOCKED**.
