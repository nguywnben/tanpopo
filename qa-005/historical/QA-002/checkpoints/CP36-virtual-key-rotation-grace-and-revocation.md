# CP36 — Virtual Key rotation grace window and revocation

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`
**Runtime:** isolated SQLite Tanpopo at `127.0.0.1:18086`, synthetic provider route `qa002-cp22-streamroute`, real Chromium Console through local HTTPS proxy
**Release gate:** `GATE-REL-07` remains **BLOCKED**.

## Journey and result

- Used the Org Owner Console to issue a PROVIDER-plane environment key. The Admin API returned 201 and a real synthetic `/v1/chat/completions` request with the original secret returned 200.
- In Access & Keys → Actions → Rotate key, the Console stated that both secrets remain valid for a 24-hour grace window and the replacement plaintext appears once. The real rotate request returned 200 and persisted `ROTATING_DUAL_VALID`.
- Tested actual streaming inference using both the previous secret and replacement secret during the grace period. Both returned 200 `text/event-stream` and the synthetic completion.
- Reopened the key in Console and selected graceful revocation. The confirmation view reported three trace-derived requests; the real revoke request returned 200 and the UI success banner said zero active streams were severed. The row visibly changed to Revoked.
- Replayed both old and new secrets after revoke. Each returned 401 `invalid_virtual_key`. Read-only SQLite showed state `REVOKED`, version 3, standard revocation, no retained previous-key hash, and three EXACT settlement rows for the three successful calls.
- Visually inspected the pre-rotation form, persisted created-key row, rotation confirmation, and revoked-state screenshots. Neither one-time plaintext secret was saved into screenshot evidence. Both temporary credentials were deleted after the checks.

## Evidence

- `../evidence/CP36-key-created.png`
- `../evidence/CP36-rotation-form.png`
- `../evidence/CP36-key-rotated.png`
- `../evidence/CP36-revoke-confirm.png`
- `../evidence/CP36-key-revoked.png`
- `../evidence/CP36-rotation-lifecycle.json` — sanitized actions and results.
- `../evidence/CP36-rotation-persistent-state.json` — read-only database state and settlement count.

## Limits

The real 24-hour expiry was not allowed to elapse; old/new dual validation was verified immediately after rotation, then both credentials were invalidated by explicit revocation. Rotation expiry automation, rotation during active streams, emergency mode, multiple clients, cross-role access, and non-Provider planes remain UNVERIFIED. No product implementation was modified.
