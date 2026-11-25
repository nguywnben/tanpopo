# CP03 — Virtual Key audit contract, revoke and persistence

**Result:** PASS for the tested key lifecycle and `virtual_key.created` filter path; broader audit/role and key rotation coverage remains UNVERIFIED.

1. From the real fresh Owner Console, create a `PROVIDER`-plane key scoped to default/dev. API returned 201 and the show-once secret was kept only in a mode-0600 `/tmp` file for the isolated test; it is excluded from this branch.
2. Probe native ingress first with an Admin cookie plus the key: 401 `invalid_credential_plane`. Repeat via cookie-free HTTP: structured 503 `no_compliant_deployment_available`. No active provider/deployment existed.
3. Issue key revocation with `EMERGENCY_ABORT_STREAMS`: API returned 200. Fresh API read returned state `REVOKED`, version 2, and `allowed_planes=[PROVIDER]`; later use of the secret returned 401 `invalid_virtual_key`.
4. Persistent audit records included `virtual_key.created` and `virtual_key.revoked`, both targeting the same key. Exact API action filtering returned the create event; UI Security → Audit Log action filtering returned one create row. Current OpenAPI documents `virtual_key.created`; therefore the historical DEF-037 mismatch was not observed on this RC6 path.

Evidence: `evidence/CP02-native-authenticated-probe.json`, `evidence/CP03-key-audit-revocation.json`, `evidence/CP03-key-persistent-state.json`, `evidence/CP03-audit-filter-virtual-key-created.png`, and `evidence/CP03-vkey-revoked-ui.png`. This does not constitute full native inference acceptance or complete audit verification for every write path.
