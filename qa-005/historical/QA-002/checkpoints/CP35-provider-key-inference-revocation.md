# CP35 — Provider Virtual Key inference and graceful revocation

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, synthetic Ollama-compatible provider at `127.0.0.1:19106`, real Chromium Console through local HTTPS proxy
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Journey and evidence

1. Signed in as the isolated Org Owner and used Access & Keys → Issue key to create `QA002 CP35 live probe revoke`. The real Admin API returned 201, state `CREATED_ACTIVE`, PROVIDER plane, route `*`, model `*`, and a one-time secret. The secret was held only in a mode-0600 `/tmp` file for the data-plane request; it is absent from committed evidence and that file was deleted after testing.
2. Used the key against `POST /v1/chat/completions` with `qa002-cp22-streamroute`, streaming enabled and a synthetic prompt. The API returned 200 `text/event-stream`; the fixture produced “Synthetic inference completed.” Read-only SQLite independently showed a 200 trace for that route and one EXACT settlement for the same key, 13 input + 10 output tokens, total 1 micro-USD.
3. Opened the key row’s Actions → Revoke key, chose the default graceful-drain mode, and submitted through the Console. The real revoke endpoint returned 200. The key row visibly changed to Revoked. Read-only SQLite showed state `REVOKED`, version 2, mode `STANDARD`, and a revocation timestamp.
4. Replayed the identical synthetic data-plane request with the now-revoked credential. The server returned 401 `invalid_virtual_key` / “Virtual Key is invalid, expired, or revoked.” No inference success occurred after revocation.

The initial HTTPS proxy path returned 403 `csrf_validation_failed` before creating a key because its forwarded Origin did not match the HTTP upstream. A dedicated loopback-only test proxy was configured to rewrite the forwarded Origin to the upstream origin; the replay then succeeded. This was isolated to the test proxy and is not classified as a product defect.

During setup, one first successful issuance (`QA002 CP35 revoke lifecycle`) was not captured by the test harness because its parser did not recognize the response's `secret_key` field. That key was never used and was immediately revoked through the UI; the subsequent named key was used for the actual inference/revocation test. This harness handling issue did not affect Tanpopo behavior.

## Evidence

- `../evidence/CP35-key-issue-form.png` — visually inspected pre-submit form (contains no plaintext credential).
- `../evidence/CP35-key-issued-list.png` — visually inspected persisted active-key row; one-time secret is not shown.
- `../evidence/CP35-key-revoke-confirm.png` — actual Console revoke confirmation; it displayed one trace-derived request.
- `../evidence/CP35-key-revoked.png` — visually inspected list with the key marked Revoked.
- `../evidence/CP35-key-live-inference.json` — HTTP 200 streaming outcome, sanitized response excerpt.
- `../evidence/CP35-key-revoke.json` — sanitized UI revoke result.
- `../evidence/CP35-key-post-revoke.json` — post-revocation HTTP 401 outcome.
- `../evidence/CP35-key-persistent-state.json` — key state, trace and settlement fields from read-only SQLite; contains no key material.

## Unresolved trace/settlement correlation observation

The same key and approximately the same timestamp have a 200 trace ID `trc_01m85eb8nre00aqm8awr540zxc` and an EXACT one-micro settlement ID `trc_01m85eb8nse00b8bwess9qdpfg`, nine milliseconds apart. Both point to the same Virtual Key; the trace route and settlement model match the exercised synthetic inference. Their request IDs differ. CP22 already recorded separately generated trace/settlement identifiers and a zero trace cost beside a one-micro settlement. CP35 independently reproduces the ID discrepancy; no total mismatch was seen, but the documented correlation contract is unavailable. This is retained as QA-OBS-006, not a confirmed defect.

## Scope limits

This verifies one owner-issued PROVIDER key, one synthetic model/route and graceful single-key revocation with no concurrent stream. Rotation/grace modes, in-flight cancellation/drain behavior, other roles/tenants/planes, expiration, quota, and real providers remain UNVERIFIED. No Tanpopo implementation was changed.
