# CP27 — Vault/webhook boundary and emergency kill-switch lifecycle

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, Chromium, synthetic Ollama-compatible provider at `127.0.0.1:19106`. Source tree unchanged.  
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Results

- **Vault credential lifecycle — PASS for the tested flow.** Created a synthetic 32-byte `WEBHOOK_HMAC_SECRET` through Providers → Vault Secrets. The API returned 201; the UI displayed only the trailing fingerprint and said plaintext cannot be read again. The response summary explicitly excludes secret material. After the webhook test, deleted the credential through the UI (DELETE 204) and verified the authenticated list was empty. The local plaintext test file was removed.
- **Webhook private-target validation — PASS for the security boundary; delivery BLOCKED.** Operations → Webhooks showed zero subscriptions and documented a public HTTPS receiver requirement. A real UI attempt to register `https://127.0.0.1:19111/hook` with the synthetic HMAC credential returned HTTP 400 `ssrf_blocked_private_ip`; no subscription was created. This is expected SSRF protection. A webhook delivery/response/retry lifecycle remains BLOCKED because the runtime has no approved, controllable public HTTPS receiver reachable by Tanpopo. No third-party endpoint was given test events. Screenshots were visually inspected. Evidence: `CP27-webhooks-initial.png`, `CP27-webhook-private-form.png`, `CP27-webhook-private-rejection.png`, `CP27-webhook-private-rejection.json`, and vault create/delete evidence.
- **Project kill-switch workflow — FAIL, DEF-020 confirmed; enforcement and recovery PASS in scope.** Real UI preview reported 1 org, 1 project, 10 virtual keys and 1 connection. After two-step confirmation with the exact project slug, trigger returned 200 at epoch 35 and the API listed one active project freeze with the expected target, reason and owner actor. The Playground route returned 503 while the freeze was active. Clear succeeded through the UI; the page reported zero active switches and authenticated API returned an empty switch list. However, the global Kill Switch pill and Security navigation badge remained at `1` in the same session, including after navigation to Playground. A full reload reconciled both to zero; see `reports/DEF-020-killswitch-badge-stays-active-after-clear.md`.
- The first post-clear Playground request returned 429 because the earlier CP26 project budget remained exhausted at 2 micro. This was independent of kill-switch state. Raising the isolated test cap to 10 micro restored the same route; the next UI request returned 200 with the synthetic response and the fixture recorded a stream. This confirms route recovery after clear, though the required budget increase is included in the record.
- The Security UI hash-chain verification returned 200 with `intact=true` for 193 events. Trigger and clear audit events are present in `evidence/CP27-killswitch-recovery.json`; the server-reported active list is empty after clear.
- **DEF-017 reobserved:** successful Playground recovery returned a response but retained “Trace metadata pending…” for the request ID; the prior trace-card mismatch finding remains independently open.

## Evidence and inspection

- Visually inspected `CP27-ops-initial.png`, `CP27-webhooks-initial.png`, `CP27-webhook-private-form.png`, and `CP27-webhook-private-rejection.png`. The rejection is inline, the drawer remains open with the submitted target, and the list remains at zero.
- Visually inspected `CP27-secret-sealed.png`, `CP27-secret-delete-confirm.png`, and `CP27-secret-deleted.png`. Screens show fingerprint only and successful cleanup.
- Visually inspected `CP27-killswitch-initial.png`, `CP27-killswitch-preview.png`, `CP27-killswitch-active.png`, `CP27-killswitch-playground.png`, `CP27-killswitch-clear-confirm.png`, `CP27-killswitch-cleared.png`, `CP27-killswitch-recovery.png`, `CP27-killswitch-recovery-success.png`, and `CP27-killswitch-badge-after-reload.png`.
- `CP27-killswitch-active.json`, `CP27-killswitch-playground.json`, `CP27-killswitch-recovery.json`, `CP27-killswitch-recovery-success.json`, `CP27-killswitch-badge-after-reload.json`, and `CP27-secret-deleted.json` preserve API and UI summaries. Secret material, the isolated SQLite database, and raw runtime logs are excluded.

## Residual coverage

Public webhook registration and actual delivery, retries, signature verification, remote receiver failures, kill-switch roles other than owner, four-eyes clear barriers, extension/provider/key scopes, active-stream interruption, and multi-tenant propagation remain UNVERIFIED. CP27 is a partial phase checkpoint; full Alpha/Beta regression continues.
