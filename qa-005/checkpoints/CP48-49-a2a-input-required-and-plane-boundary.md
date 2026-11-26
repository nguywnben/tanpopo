# CP48–49 — A2A input-required lifecycle and Virtual Key plane boundary

**Baseline:** exact unsigned source `737079ddb847d97dbcf832f610ed0048556cb65b`; compiled Linux amd64 binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`. **Environment:** isolated PostgreSQL 16, three-node Valkey 8.1 Cluster, exact-source Control Plane `127.0.0.1:18151` and Data Planes `:18152`/`:18153`; `TANPOPO_A2A_ENABLED=true`; Chromium; deterministic loopback fixture `127.0.0.1:19186`. All credentials and database material stayed in `/tmp`; no real or paid provider was used.

## CP48 — input-required task and follow-up

An Owner registered the local synthetic A2A agent through MCP Hub → A2A Agents with explicit loopback permission (`127.0.0.1/32`). The Console issued the test Virtual Key and the cookie-free Data Plane created a task for the registered agent. The real Console rendered the task as **Input required** with the synthetic prompt, US region, one of five hops, and a $0.010000 sub-budget hold. The Operator resumed it with `APPROVED — synthetic QA follow-up`; the row changed to **Completed**, the artifact was rendered, and $0.003000 was settled from the $0.010000 hold. The fixture observed the initial dispatch and follow-up, a trace root and hop count 1 on both, and no forwarded caller Authorization header. The key was revoked through Access & Keys; the visually inspected inventory shows the tested key and the other QA keys as Revoked.

**Credential-scope correction:** the CP48 key had both PROVIDER and A2A planes enabled. The inspected issue form proves that selecting A2A added it to the default PROVIDER grant. Therefore CP48 is not evidence of an A2A-only credential. The separate CP49 checks below cover a provider-only key's A2A denial. This corrects the scope characterization without invalidating CP48's task lifecycle observation.

## CP49 — provider-only authorization boundary

Twice, the real Access & Keys UI issued a test key with **only PROVIDER** selected. With each one-time secret held only in process memory, the test called both `GET /a2a/agents` and `POST /a2a/tasks` on Data Plane `:18152`. Each call returned HTTP 403 `scope_or_plane_denied`. The loopback fixture call count remained 2 before/after both attempts, proving that neither discovery nor task dispatch reached the agent. Both test keys were revoked through the UI; final inventory and response showed `REVOKED` version 2. No task, trace or downstream side effect was created by these denied requests.

## Evidence and scope

- Sanitized task, fixture, UI-state and key-revocation evidence: [`CP48-49-a2a-lifecycle-and-plane-boundary.json`](../evidence/CP48-49-a2a-lifecycle-and-plane-boundary.json).
- Visually inspected screenshots: [`input-required`](../evidence/screenshots/CP48-a2a-input-required.png), [`resume form`](../evidence/screenshots/CP48-a2a-resume-form.png), [`completed`](../evidence/screenshots/CP48-a2a-completed.png), [`CP48 key form`](../evidence/screenshots/CP48-a2a-key-form.png), [`provider-only issue form`](../evidence/screenshots/CP49-provider-only-key-form.png), [`provider-only revoked inventory`](../evidence/screenshots/CP49-provider-only-key-revoked.png), [`CP48 revoked inventory`](../evidence/screenshots/CP48-a2a-key-revoked.png).
- The CP49 synthetic key last-four value on the screenshot is metadata only; no plaintext secrets were captured.

These are scoped passes for one synchronous local fixture and owner workflow. They do **not** close historical DEF-069: CP48 did not cancel active work. CP28 remains the fresh reproducer showing downstream work continued after cancellation. Nonzero-hold settlement/refund on failure or cancellation, parent/child budgets, concurrent resumes, task failure/retry, task expiration, loop/hop and residency enforcement, non-Owner roles, multiple tenants, and real partner interoperability remain UNVERIFIED. The agent accepts the test-only loopback endpoint; no external A2A service was contacted.
