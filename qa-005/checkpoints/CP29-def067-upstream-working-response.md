# CP29 — DEF-067 upstream `working` response

**Baseline:** frozen source `737079ddb847d97dbcf832f610ed0048556cb65b`; exact-source build SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`. Isolated A2A-enabled SQLite clone on `127.0.0.1:18148`; synthetic upstream on `127.0.0.1:19095`; real Chromium Console. No source change, public egress or paid provider.

## Result: PASS (scoped DEF-067 path)

Registered `QA005 Working State Fixture` through MCP Hub → A2A Agents & Active Tasks, with explicit loopback permission `127.0.0.1/32`. The endpoint returned HTTP 200 and JSON `{"state":"working"}` synchronously. Issued a temporary A2A-only key with `/a2a/*` routes via Access & Keys; the plaintext was held only in `/tmp`, omitted from evidence and screenshots, and revoked in the Console after use.

Cookie-free `POST /a2a/tasks` returned HTTP 201 with task state `working`, version 2 and a task ID. The real Console displayed that same Working task with its Cancel action. Clicking Cancel returned HTTP 200, persisted `canceled`, removed the action, and displayed the success notice. The task had zero hold and zero settled cost. Exact-source `tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds=false` returned a valid 265-event chain and zero orphaned holds.

The Working task, post-cancel state, agent registration, and revoked key were visually inspected in `CP29-working-task-inventory.png`, `CP29-working-task-canceled.png`, `CP29-working-agent-registered.png`, and `CP29-working-key-revoked.png`. Machine-readable detail is [`CP29-def067-working-response.json`](../evidence/CP29-def067-working-response.json).

This resolves only the historical synchronous `working` response rejection scenario for one synthetic HTTP endpoint on SQLite. No real async worker or continuation was attached to this upstream result. Cancellation of a `working` task remains a separate failure under DEF-069: CP28 showed downstream work continuing after the UI cancel. Nonzero-budget, reconnect/retry, PostgreSQL/Valkey and other agent response types remain UNVERIFIED.
