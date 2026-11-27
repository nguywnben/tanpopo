# CP89 — Trace explorer metadata-only inspection

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated QA runtime `http://127.0.0.1:18084`  
**Role/browser:** Organization Owner, real Chromium at 1440×900  
**Gate:** `GATE-REL-07 BLOCKED`

## Journey and evidence

1. Signed in through the real Console and selected Monitor → Traces.
2. Inspected the recent trace table and opened a real trace row to view its waterfall.
3. Captured the authenticated `GET /admin/v1/traces?limit=100` response and compared its visible fields with the UI.
4. Read the local runtime config for an OTel/OTLP exporter setting and checked for a local collector listener on 4317/4318. No matching YAML setting or local listener was found. The process environment could not be read in this environment; this is not proof that external collector configuration is absent.

The API returned HTTP 200 and two rows, each with `has_debug_payload=false`. The API objects contained operational metadata (plane, environment/key IDs, route/deployment, status/error, usage classification/cost, timings and waterfall stages) and no prompt/completion payload field. The Console showed a Metadata-Only badge and rendered only IDs, route/status/error, metering, duration, timestamp, and stage metadata. The selected detail waterfall also showed no prompt/completion content. The initial list and selected detail screenshots were visually inspected.

A first exploratory navigation attempted `/traces`, which resolved to Overview; the actual UI route is `/observability/traces`. This harness navigation mistake is not a product finding and was corrected by clicking the visible Traces link.

## Result and limits

- **PASS (narrow):** the observed existing traces and selected detail exposed metadata only; both API rows reported `has_debug_payload=false`.
- **UNVERIFIED:** full `TP-AC-F0804` acceptance. No new marker-bearing inference was sent, no raw credential was used as a searchable test marker, no configured external collector was shut down, and no dropped-OTLP counter/warning or nonblocking behavior was measured. There is no local collector listener/configuration evidence, and runtime process environment access was denied.
- **No defect assigned:** this was a bounded read-only metadata observation, not a full export or privacy acceptance run.
- Existing DEF-017 lookup behavior is not retested or changed by CP89.

## Evidence

- `evidence/CP89-trace-explorer-list.png`
- `evidence/CP89-trace-detail.png`
- `evidence/CP89-trace-privacy-observation.json`

## Consolidated status

- **Historical DEF-001–015:** no new retests in CP89; prior scoped statuses remain, DEF-005 remains partial, and independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Confirmed open defects:** DEF-016–034 remain open; no status changed. Preserve CP86 DEF-034 and DEF-020 recurrence.
- **New findings:** no new defect. Narrow metadata-only observation supports the UI/API claim for currently stored traces; complete privacy/export criteria remain unverified.
- **Coverage completed:** real authenticated trace list/detail; metadata/API comparison; visual inspection; read-only local config/listener checks.
- **Blocked:** full OTel fail-open test awaits a configured OTLP collector endpoint or explicit collector configuration and access. Webhook delivery remains blocked by managed Cloud egress; runtime 18445 A2A remains disabled; RC4 artifact remains unverified in the separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance coverage; marker-bearing inference plus raw-credential log search and collector outage/recovery; other trace roles/filters and waterfall classes; all open DEF-016–034 remediation/regression; broader remaining product/browser/platform/integration/accessibility/operations coverage.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. No new defect handoff.

QA-002 remains active and incomplete.
