# CP111 — DEF-049 exact-RC7 fresh no-adapter setup

**Disposition:** No-adapter first-install contract is now aligned in the tested flow; DEF-049 moves from UNVERIFIED to **PARTIAL**, preserving the historical failures. The adapter-enabled route-creation path remains open. `GATE-REL-07` remains **BLOCKED**.

## Environment and exact scope

- Frozen source: `737079ddb847d97dbcf832f610ed0048556cb65b`.
- Exact-source Linux binary: SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`.
- Fresh empty SQLite data directory; loopback listener `127.0.0.1:18165`; real Chromium 156; newly bootstrapped synthetic Owner, Org, Project and `dev` environment.
- No adapter enabled, provider connection, provider call, production credential, or paid usage.

## Reproduction and results

1. Opened `/setup` before initialization. The page headline reads **“Claim this self-hosted gateway and initialize its primary organization and owner account.”** It no longer claims an initial route will be provisioned.
2. Entered a one-time bootstrap token, synthetic Owner account, Org and Project, then advanced to First connection. The screen explicitly stated **“No provider adapters enabled yet”** and instructs the user to finish setup, activate one of eight bundled official adapters in Extensions → Marketplace (or stage through CLI), and then add connections.
3. Continued with no adapter/connection, checked the Master key custody acknowledgement, and inspected Review. It said **“Review and initialize,” “Initialize instance,”** and **“Initial connection: Skipped.”**
4. Completed setup and reached the real workspace. The scope picker showed the new Org/Project/`dev`. `GET /admin/v1/routes?limit=100` returned HTTP 200 with zero rows. Routes & Policies displayed **“No routes in this environment.”**
5. Read-only SQLite counts showed one organization/project/environment and zero provider connections, logical models, model deployments, route policies or route targets. This matches the skipped upstream setup; no inference or route was attempted.

## Finding disposition

The old no-adapter mismatch (headline promises route, route list empty) does not reproduce. The current headline describes what bootstrap initializes; the provider step explains the necessary next actions; Review accurately says the initial connection was skipped; and the final route empty state is consistent with those disclosures. **PARTIAL** is retained because the accepted end-to-end case also requires an adapter-enabled setup and route readback; that path was not tested.

All five screenshots were visually inspected. The sanitized route/row-count record and hashes are in [evidence](../evidence/CP111-def049-fresh-no-adapter-setup.json): [initial setup](../evidence/screenshots/CP111-def049-fresh-setup-start.png), [First connection](../evidence/screenshots/CP111-def049-first-connection.png), [key custody](../evidence/screenshots/CP111-def049-key-custody.png), [Review](../evidence/screenshots/CP111-def049-review.png), and [final empty routes](../evidence/screenshots/CP111-def049-routes-empty.png).
