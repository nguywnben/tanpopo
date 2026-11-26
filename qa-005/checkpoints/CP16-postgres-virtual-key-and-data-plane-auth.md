# CP16 — PostgreSQL Virtual Key creation and Data Plane auth boundary

**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime:** exact-source local build on PostgreSQL 16 + loopback Valkey 8.1, `127.0.0.1:18146`; Playwright Chromium owner Console; no product code changes.  
**Result:** PARTIAL; proves the key issue/auth path, while routed provider behavior is blocked by DEF-081.

## Real browser action and persisted state

Signed in as the synthetic PG Org Owner, opened Access & Keys, and used the actual “Issue key” form to create `QA005 PG auth boundary`, PROVIDER plane, wildcard route, all models. The Console returned HTTP 201 and a show-once secret; the secret was saved only to a mode-0600 temporary file under `/tmp`, then the modal was closed. Visually inspected screenshot `evidence/screenshots/CP16-postgres-key-inventory.png` displays only the masked prefix and last four. Independent test-only DB readback confirmed the token digest, tail, and active lifecycle state matched; no secret/hash was added to branch evidence.

## Cookie-free Data Plane attempt

Sent a real POST `/v1/chat/completions` with only `Authorization: Bearer <synthetic Virtual Key>`, synthetic request content, and a nonexistent route header. Response was HTTP 503 `no_compliant_deployment_available`, not 401. The request authenticated and then failed route selection because the preceding PostgreSQL extension activation left the provider-adapter catalog empty. This is direct evidence that the created PG key works at the auth boundary, not a successful provider route or protocol pass.

Valkey container remained reachable (`PONG`), but its DB size stayed zero after this request; no rate/budget key operation could be verified. Successful PostgreSQL/Valkey Data Plane behavior remains BLOCKED/UNVERIFIED behind DEF-081.

The first attempt used `X-Tanpopo-Virtual-Key`, which the unified chat handler does not accept; the correct contract is `Authorization: Bearer`. That bad request returned 401 and is excluded as a harness error. No authentication defect is recorded.

Screenshots/evidence: `screenshots/CP16-postgres-key-inventory.png`; sanitized response details in the checkpoint only. GATE-REL-07 remains BLOCKED.
