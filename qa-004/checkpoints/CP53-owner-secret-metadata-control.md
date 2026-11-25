# CP53 — Owner control for DEF-066 secret metadata persistence

**Status:** PASS (scoped Owner persistence and unfiltered read control); CP52 Team Admin failure remains **FAIL (scoped)** as DEF-066. **GATE-REL-07:** BLOCKED.
**Baseline:** frozen RC6 source `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`; exact artifact run `45581179274` at isolated runtime `127.0.0.1:18132`.
**Product source:** unchanged.

## Owner control workflow

Signed in to the existing isolated runtime as synthetic Org Owner `qa004-cp49-owner@example.invalid`, opened **Providers → Vault Secrets**, hard-refreshed, and revisited the tab. Both before and after refresh, the UI showed exactly one row for `QA-004 Project Metadata Canary` with `API_KEY`, trailing fingerprint `...bdbb`, and the same credential ID recorded in CP52. The screenshot was visually inspected; it contains only metadata, not plaintext.

The same Owner session issued `GET /admin/v1/secrets?limit=100`: HTTP 200, one matching metadata row, and no `secret_value` field. No secret was revealed or sent to a provider.

## Interpretation

This control confirms that the synthetic credential metadata persisted and Owner’s unfiltered collection view works after refresh. It narrows DEF-066 to the project-scoped Team Admin Console path: CP52’s identical persisted record was available through the authorized project-filtered API but hidden by the UI’s unfiltered request. It does not establish behavior for other roles, cross-organization listing, credential rotation/deletion, provider binding, or a fresh login.

## Evidence

- [Owner Vault Secrets after hard refresh](../evidence/CP53-owner-secret-after-refresh.png) — visually inspected.
- [Sanitized Owner control results](../evidence/CP53-owner-secret-control.json).
- [CP52 Team Admin failure and scoped API control](CP52-teamadmin-provider-metadata-and-cross-screen-sweep.md).
- [DEF-066 reproduction and acceptance criteria](../reports/DEF-066-teamadmin-provider-secret-metadata-hidden-after-refresh.md).

## Coverage

- **PASS (scoped):** Org Owner can read the persisted credential’s safe metadata in UI and unfiltered API after hard refresh.
- **FAIL (scoped):** project-scoped Team Admin metadata readback after refresh remains DEF-066.
- **UNVERIFIED:** Owner fresh-login persistence, credential deletion and rotation, binding, cross-organization boundaries, and other roles.

QA-004 remains ongoing; no release conclusion is made.
