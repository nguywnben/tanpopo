# CP54 — Fresh bootstrap route state and setup copy

**Status:** scoped RC5 result recorded; QA-003 continues.
**Baseline:** `3423d4f523ef0ac77469a1201b8b753d1d31c902`
**Runtime:** isolated SQLite, `--role all`, `127.0.0.1:18118`; Chromium.
**GATE-REL-07:** BLOCKED.

## Build provenance

The test service executable `/tmp/tanpopo-qa003` reports `version=0.1.0-dev`, `commit=3423d4f523ef0ac77469a1201b8b753d1d31c902`, and `built=2026-11-25T00:10:06+07:00`; SHA-256 `c04227449313e9e9befaa9e02952f9dd15edbc8e24a368fe678e4720e467cec1`. A reproducible build was also produced from the unchanged RC5 product source using Go 1.26.10 in official `golang:1.26.0` image digest `sha256:fb612b7831d53a89cbc0aaa7855b69ad7b0caf603715860cf538df854d047b84`, with host trust roots mounted read-only for Go module TLS validation. It reports the same commit/version/date; SHA-256 `e10e1da45c25c65a41f527fcf58c9a080b94e6ec9609008fc29bf75926eb12be`. The binary hash differs between build outputs; version metadata and source commit match. No product source was changed.

An unrelated `/tmp/qa-tools/tanpopo` helper reports `commit=none` and failed to open this database due to a migration checksum mismatch. That attempt did not write/migrate the database and is not product evidence. CP53 was captured while the matching RC5 process was running; the process metadata and commit above confirm its provenance.

## Route state journey

1. Sign in as the synthetic owner provisioned in CP53.
2. Read `GET /admin/v1/routes?limit=100`: HTTP 200, empty `data`, `has_more:false`.
3. Open Routes & Policies. The visible workspace is the bootstrapped `QA003 Fresh Audit Bootstrap / default / dev`; the page shows `Configured routes 0` and “No routes in this environment”.
4. Select **Create route**. The real drawer has an empty deployment selector, explains “Add at least one deployment target before saving”, and keeps the final Create route button disabled. No route or other data was written.

The visually inspected evidence is [empty Routes state](../evidence/CP54-fresh-bootstrap-routes.png), [Create route prerequisites](../evidence/CP54-create-route-prerequisites.png), and [sanitized API/UI result](../evidence/CP54-fresh-bootstrap-routes.json).

## Finding DEF-049 — setup copy promises an initial route, but creates none

**Severity: Low (first-install UX/business-contract clarity).** The Instance setup headline says “Claim this self-hosted gateway and provision its initial owner and route.” In the same run, provider setup explains no adapter is enabled and instructs the operator to finish setup and activate one later; the Review step says `Initial connection: Skipped`. After completion, no route exists and Create route cannot proceed without a deployment target. No inference traffic or provider spend occurred.

- **Expected:** setup copy and completion state accurately distinguish tenant bootstrap from route readiness; either provision a usable route when prerequisites exist or clearly explain that route creation follows adapter activation, provider setup, model/deployment creation.
- **Actual:** the headline promises a route as part of provisioning, while the successful path creates no route and the route screen remains empty. The later skipped-connection copy partly mitigates but does not remove the contradiction.
- **Affected workflow/business impact:** first-time self-hosted install; operators may believe the gateway is routable after setup and only discover the missing provider/model/deployment prerequisite when opening Routes.
- **Reproduction:** use the fresh-install wizard with no adapters enabled; continue without an initial connection; complete setup; inspect Routes & Policies and open Create route. This was performed in CP53/CP54 on the exact RC5 source baseline.
- **Acceptance:** revised wizard text/review makes route readiness explicit; after completion the UI accurately reports whether a route is configured, and offers the next required setup steps. If product contract requires a route during bootstrap, create one with a valid target or fail setup clearly. No empty or misleading “ready” route should be implied.
- **Cause:** not established. The no-route state is consistent with having no provider/deployment; classify the discrepancy as copy/business contract, not routing implementation failure.

## Scope and remaining work

This confirms the empty initial route state only. It does not test a wizard path with an enabled adapter or connection, and does not establish whether initial setup should create a usable route by contract. DEF-049 is a UX finding; FT-10, setup, and remaining route coverage stay PARTIAL. No implementation changes, secrets, database files, or provider credentials are included.
