# Engineering handoff — DEF-010 A2A Virtual Key scope cannot be granted

## Summary

The A2A management UI can register agents, and the data plane can be enabled with `TANPOPO_A2A_ENABLED=true`, but the shipped Virtual Key issue path creates provider-only keys. The UI and documented create request expose neither the A2A plane nor an A2A route/grant allowlist. With A2A enabled, a normally issued key receives HTTP 403 `scope_or_plane_denied` on `GET /a2a/agents`. This blocks tenant users from using the A2A data plane through supported issuance flows.

## Reproduction

1. Start the isolated Tanpopo server with `TANPOPO_A2A_ENABLED=true`.
2. Sign in as Org Owner, open Access & Keys → Issue key. The form exposes a key name, “Allow all models”, and an MCP tool allowlist; it has no A2A plane or route controls.
3. Issue a QA key and call `GET /a2a/agents` with `Authorization: Bearer <issued key>` in a cookie-free client.
4. Observe HTTP 403 `scope_or_plane_denied`, message `Virtual Key is not permitted on A2A plane.`
5. Read-only QA database inspection shows the issued key's `allowed_planes` was `PROVIDER`; revoke the key after the probe.

## Expected behavior

An authorized operator should be able to issue a Virtual Key with an explicit A2A plane grant and narrowly scoped agent/discovery/task routes, consistent with the documented A2A permissions. The default key should remain denied. The server must continue enforcing the selected scope and route allowlist.

## Evidence

- `evidence/phase32-vkey-issue-form.png` and `.json`
- `evidence/phase34-a2a-vkey-bearer-probe.json`
- `evidence/phase34-vkey-a2a-scope-readback.json`
- `evidence/phase34-a2a-enabled-agent-list.png`

The earlier `TANPOPO_A2A_ENABLED`-unset response was 404 `A2A plane is disabled`; this is an instance configuration condition, not the confirmed key-scope failure.

## Acceptance checks

- Default/provider-only key remains denied on A2A routes.
- A deliberately A2A-scoped key can discover only authorized agents and can delegate only to explicitly permitted agents/routes.
- Revocation blocks subsequent A2A requests, and the UI/API never returns key plaintext after the initial create response.
- A2A scope changes are auditable and tenant/project/environment boundaries remain enforced.

## CP39 positive-path separation (2026-11-23)

A separate isolated 8084 test used a QA-only synthetic key explicitly granted the A2A plane and an authorized loopback agent. Discovery, one completed task/artifact/SSE, and one async cancellation path succeeded. The caller bearer was not forwarded to the fixture. The synthetic key was revoked and subsequent bearer use returned 401. This confirms authorized A2A flows can execute when the grant exists; it does not change the supported-issuance finding above. Preserve default denial and backend scope enforcement. Never solve DEF-010 by weakening A2A authorization. Evidence: `../evidence/phase39-a2a-lifecycle.json`, `phase41-a2a-input-required.json`, and `phase41-a2a-fixture-cleanup.json`.
