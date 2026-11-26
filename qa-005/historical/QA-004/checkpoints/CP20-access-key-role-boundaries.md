# CP20 — Access & Keys role boundaries

**Outcome:** PARTIAL for Virtual Key metadata visibility and key lifecycle authorization; no new defect confirmed. A project-only Developer could inspect the scoped key's metadata but received an explicit denial for organization member enumeration and a server-side 403 when attempting key revocation. The Org Owner successfully revoked the same key through the real UI; API readback confirmed `REVOKED`, version 2. GATE-REL-07 remains BLOCKED.

## Reproduction and evidence

The synthetic Developer was assigned only to `qa004-isolation-canary`, then opened Access & Keys in `qa004-human-label-check`. The Virtual Keys tab displayed one row containing its name, key prefix/last four, plane/route grants, and state. The API returned HTTP 200 for the key list with no plaintext secret/token field. The Members & Roles tab explicitly explained that this role can inspect Virtual Keys but cannot list organization members. A CSRF-protected revoke attempt with `If-Match: "v1"` returned HTTP 403 `insufficient_role_permission`; an Owner readback confirmed the key remained active at version 1. See `CP20-developer-key-metadata.png`, `CP20-developer-members-tab.png`, and sanitized `CP20-developer-vkey-rbac.json`.

The Org Owner then selected the exact `qa004-human-label-check` environment, confirmed the key was absent in the sibling canary environment, reopened the scoped Access & Keys table, and used **Actions → Revoke key → Graceful drain**. The UI confirmed “Virtual key revoked. 0 active streams severed.” and displayed `Revoked`; a fresh authenticated list returned HTTP 200 with state `REVOKED`, version 2, and no secret/token field. Screenshots of the scope picker, action menu, revoke confirmation and post-action state are `CP20-owner-scope-menu.png`, `CP20-owner-key-actions.png`, `CP20-owner-revoke-dialog.png`, and `CP20-owner-key-revoked.png`; structured sanitized readbacks are `CP20-owner-vkey-revoked.json` and `CP20-developer-vkey-rbac.json`.

The screenshots were visually inspected. The one-time key secret was neither captured nor stored in QA evidence. No secret, credential, database, or sensitive log is included in this checkpoint.

## Interpretation

The observed boundaries are consistent with least privilege: the Developer can see only non-secret metadata for a key in its assigned project/environment, cannot enumerate organization members, and cannot revoke credentials; the Owner can revoke and the state persists. This is a scoped PASS for these individual authorization checks, not proof of complete Access & Keys RBAC. Key rotation, grant edits, list behavior in other roles, stale `If-Match`, active-stream drain, audit events for provisioning/revocation, cross-project identifiers, and other lifecycle/error cases remain UNVERIFIED. The key is revoked and was not used for inference.

## Environment isolation observation

The Owner's saved browser state reopened in the sibling `qa004-scope-canary-env`; the key list there was empty. Switching to `qa004-human-label-check` made the expected single row appear. This is consistent with environment-scoped key isolation and confirms that the earlier empty list reflected selected scope, not loss of the key.
