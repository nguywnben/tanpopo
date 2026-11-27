# CP34 — Traces empty state and persisted inventory

**Baseline:** frozen RC5 source `3423d4f523ef0ac77469a1201b8b753d1d31c902`  
**Runtime:** isolated SQLite at `127.0.0.1:18116`; real Chromium 156; synthetic Org Owner  
**Disposition:** SCR-11 **PARTIAL** for the `default/dev` empty state. FT-14 remains **UNVERIFIED** because telemetry privacy and OTel export were not exercised. No defect confirmed. `GATE-REL-07` remains **BLOCKED**.

## Reproduction and evidence

1. Signed in as the synthetic Org Owner, selected `Default Organization / default / dev`, and opened **Traces**.
2. Visually inspected [the Traces screen](../evidence/CP34-traces-inventory.png). It identifies itself as metadata-only, displays Request ID, Model and Error code lookup/filter controls, and presents “No matching traces” with guidance to send gateway traffic. No table rows, details, or error banner appeared.
3. Resolved the `default` project’s environment using `GET /admin/v1/projects/{projectId}/environments`, then queried `GET /admin/v1/traces?env_id={dev-id}&limit=100` with `cache: no-store`. The response was HTTP 200, `count: 0`, `has_more: false`. Sanitized inventory is in [CP34 trace inventory](../evidence/CP34-traces-inventory.json).

## Result and limits

- The real UI empty state agrees with the persisted trace inventory for this environment.
- No gateway request was generated in this phase, so there was no trace record to inspect, correlate, or test against prompt/completion leakage. The metadata-only label is observed UI copy, not proof of payload privacy.
- OTel export, trace detail/waterfall, filtering against existing records, paging, role boundaries, cross-scope stale responses, and error/recovery states remain **UNVERIFIED**. FT-14 remains **UNVERIFIED**; SCR-11 is **PARTIAL** for this named empty state only.
- No new defect or change to the synthetic database was made. QA-003 remains incomplete; `GATE-REL-07` remains **BLOCKED**.
