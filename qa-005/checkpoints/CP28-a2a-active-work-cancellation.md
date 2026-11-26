# CP28 — DEF-069 A2A active-work cancellation on RC7

**Baseline:** `737079ddb847d97dbcf832f610ed0048556cb65b`; local exact-source binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`. **Runtime:** isolated SQLite clone `/tmp/qa005-cp28a2a/data/tanpopo.db`, port 18148, `TANPOPO_A2A_ENABLED=true`; loopback delayed synthetic endpoint on port 19094; Chromium 1440×1000. No real providers, product source, production credentials, or production data used.

## Result: DEF-069 remains FAIL (fresh RC7 scoped reproduction)

In the real MCP Hub → A2A Agents & Active Tasks Console, an Operator/Org Owner registered `qa005-delayed-cancel` to `http://127.0.0.1:19094/a2a` with explicit `127.0.0.1/32` permission. Access & Keys issued a key restricted to the A2A plane and `/a2a/*`. The plaintext key was held only under `/tmp` with mode 0600, omitted from screenshots/evidence, and revoked through the Console after the test.

A cookie-free `POST /a2a/tasks` dispatched synthetic text to the endpoint, which intentionally takes 15 seconds to finish. While the task was visibly **Working**, the operator clicked **Cancel** in the Console. The cancel API returned HTTP 200, task state became `canceled` version 3, and the UI announced “Canceled A2A task and released held budget.” The original create call returned HTTP 200 with that same canceled task representation; unlike historical CP82/CP108, this run did not return `canceled -> completed` HTTP 400.

The downstream fixture nevertheless ran its full 15-second synthetic job. It logged the incoming `/a2a/tasks` request and, only after completing its delay/work, failed to write the result because the response connection had been interrupted (`BrokenPipeError`). No separate cancellation operation reached the fixture. This confirms that the caller-visible state and response are improved from the historical 400 outcome, while the delegated work itself is not stopped or promptly signaled; therefore the major acceptance requirement remains failed. The test used zero-cost loopback work and no external service.

Screenshots of the **Working** task and post-cancel task list were visually inspected: `CP28-a2a-working-before-cancel.png`, `CP28-a2a-canceled-after-action.png`. Agent registration, key form, and revoked-key inventory are captured in `CP28-a2a-agent-registered.png`, `CP28-a2a-only-key-form.png`, and `CP28-a2a-key-revoked.png`. Sanitized request, state, fixture, and audit details are in [`CP28-a2a-cancellation.json`](../evidence/CP28-a2a-cancellation.json). The exact-source `admin verify-state --check-audit-chain --reconcile-orphaned-holds=false` reported a valid 253-event chain and zero orphaned holds.

## Expected versus actual and acceptance

- **Expected:** per the historical SM-09 acceptance, cancellation of `working` promptly signals the active downstream operation through a supported protocol or aborts it so the work stops; the create caller receives an unambiguous correlated cancellation result; state, hold release, trace and audit remain consistent.
- **Actual:** the UI and cancel API transition successfully, and the create caller receives HTTP 200 `canceled`, but the downstream endpoint continues its full work interval and only encounters a broken pipe when attempting its eventual response. The run had zero hold/cost, so it does not prove nonzero-budget release or financial idempotency.
- **Impact/severity:** Major. Users can be told a task is canceled while delegated sensitive work and resource consumption continue. This can cause unexpected work/cost and duplicate retries.
- **Root-cause hypothesis (not fact):** the cancel action updates durable task state but does not signal or interrupt the in-flight dispatch; later result handling observes terminal state. The fact that the delayed endpoint performed its full work after cancellation supports investigation of active dispatch coordination, but engineering tracing is required to establish the precise implementation cause.

## Scope, harness notes, and remaining work

This is one synthetic SQLite/Chromium synchronous HTTP dispatch. Two early automation attempts did not execute a valid Console cancellation because the page route/load wait was wrong; their outcomes are excluded. A later valid journey completed as described above. No paid usage or nonzero hold was used. PostgreSQL/Valkey active cancellation, direct Virtual Key cancellation, explicit partner cancellation protocol support, client disconnects, streaming/async workers, restart/races, nonzero hold release, trace linkage and multi-tenant/role boundaries remain UNVERIFIED. Prior QA-004 SQLite and PostgreSQL FAILs remain preserved; this fresh result does not close DEF-069.
