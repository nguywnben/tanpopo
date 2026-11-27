# CP50 — DEF-069 PostgreSQL/Valkey cancellation with a nonzero sub-budget hold

**Result:** **FAIL** for delegated-work cancellation; the hold was independently verified released. This is a fresh independent backend confirmation of DEF-069, not a new defect.  
**Baseline/runtime:** source 737079ddb847d97dbcf832f610ed0048556cb65b, exact-source Linux binary SHA-256 62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9; PostgreSQL 16, three-node Valkey 8.1 Cluster (cluster_state=ok), Control Plane 127.0.0.1:18151, Data Plane :18152; Chromium; TANPOPO_A2A_ENABLED=true. No real or paid provider.

## Reproduction and result

1. In the real MCP Hub UI, register a synthetic agent at http://127.0.0.1:19187/a2a with explicit loopback consent and 127.0.0.1/32 CIDR. The fixture accepts one task, waits 15 seconds, then returns completed with spent_micros=3000 and a synthetic artifact. It implements no cancellation operation.
2. In Access & Keys, create an A2A-only Virtual Key with /a2a/* access. The plaintext was held in process memory only and omitted from all evidence.
3. Send cookie-free POST /a2a/tasks to Data Plane :18152 with a $0.010000 sub-budget hold. The loopback fixture recorded the task ID and began its fixed work interval. Its request had trace-root and hop-count headers; caller Authorization was not forwarded.
4. The real Console displayed Working, the $0.010000 sub-budget cap, and Cancel. Clicking Cancel returned HTTP 200, transitioned the durable/UI task to canceled version 3 and announced “Canceled A2A task and released held budget.” The canceled state remained after browser reload.
5. Despite cancellation, the fixture completed the full 15-second work and returned its synthetic completed/$0.003 result. No cancellation call was sent to the fixture. The original Data Plane create request eventually returned HTTP 200 with the task still canceled version 3 and $0 settled cost.
6. Read-only PostgreSQL inspection in a transaction scoped to the test organization verified a2a_tasks.state=CANCELED, task sub-budget cap 10000, settled_cost_micros=0, and joined budget_reservations.state=RELEASED_UNUSED; the reservation's recorded original hold was 10000 and linked active ledger holds summed to 0. Thus the task field/UI shows the requested cap, not remaining active reservation. No separate budget/UI defect is inferred. The test key was revoked through the Console; its inventory shows Revoked.

**Expected:** once cancellation succeeds, the delegated operation stops promptly or receives a supported cancellation signal; the caller receives an unambiguous terminal result; any unused hold is released exactly once.  
**Actual:** the Tanpopo state, response, workflow notice and hold release are coherent, but delegated work continues for its full configured interval and can complete work whose result is discarded.  
**Impact:** Major under DEF-069. Cancellation can falsely imply that external work has stopped; downstream resource use or side effects may continue, and any downstream billable work can become unaccounted. The fixture is synthetic and no paid work was performed.

## Evidence and limits

- Sanitized API, fixture, task/reservation readback and classification: [CP50 evidence](../evidence/CP50-def069-postgres-cancellation.json).
- Visually inspected: [Working task](../evidence/screenshots/CP50-pg-a2a-working.png), [canceled task and success banner](../evidence/screenshots/CP50-pg-a2a-canceled.png), [persisted canceled state after reload](../evidence/screenshots/CP50-pg-a2a-persisted-canceled.png), [revoked key inventory](../evidence/screenshots/CP50-pg-a2a-key-revoked.png), and [post-cancel FinOps summary](../evidence/screenshots/CP50-pg-finops-after-cancel.png).
- An earlier exploratory run on this fixture completed before the script had refreshed the task list and failed to capture a valid cancellation; it is UNVERIFIED and excluded from the defect result. The captured row remains in the synthetic test database and is not used as evidence. The later run above is the valid controlled reproduction.
- One synchronous HTTP fixture, one owner, one Data Plane and no configured budget ceilings. Async workers, streams, retries, races, tenant/RBAC boundaries and real partner cancellation interoperability remain UNVERIFIED. The PostgreSQL reservation readback verified release for this exact cancellation; other settlement paths remain open.
