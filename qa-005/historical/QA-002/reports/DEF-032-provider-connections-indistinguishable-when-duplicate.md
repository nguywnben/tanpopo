# DEF-032 — Provider connection records can be indistinguishable in management UI

- **Severity:** Low
- **Status:** Confirmed for the observed duplicate-record state
- **First reproduced:** CP76, frozen RC4 baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`
- **Affected workflow:** Providers → Connections → probe, edit, disable, delete
- **Environment:** isolated A2A-enabled QA runtime at `127.0.0.1:18084`; real Chromium; Org Owner

## Reproduction

1. Sign in to the isolated QA organization and open **Providers → Connections**.
2. Have two connection records in the selected project with the same display name and the same visible values. CP76 observed two distinct persisted connection IDs for `QA002 Google Gemini`.
3. Compare their table rows, open each row's action menu, and open each edit form without saving.

Sanitized API evidence identifies two distinct records (`con_01m8530p3we00828f2x8mp6vs2` and `con_01m852zx84e008qspa6bkat3wm`) in the same project. Both have the same displayed name, endpoint, adapter, unverified state, billing mode, region and secret fingerprint. Each row exposes Probe, Edit, Disable and Delete. Their edit forms expose the same name, endpoint and secret-slot rotation controls, with no project label, stable record identifier, creation time or other differentiator.

Screenshots `evidence/CP76-provider-duplicates-table.png`, `CP76-provider-duplicate-1-menu.png`, `CP76-provider-duplicate-2-menu.png`, and both `CP76-provider-duplicate-*-edit.png` were visually inspected. Sanitized record comparison is `evidence/CP76-provider-duplicate-comparison.json`.

No probe or mutation was sent. The test does not establish which workflow created the duplicates or whether duplicate names are intended to be allowed. It does establish that when such records exist, a user cannot tell which backing connection an action will affect from the UI.

## Expected and actual behavior

- **Expected:** each persisted connection can be reliably identified before a potentially disruptive action; duplicate display names should either be prevented or disambiguated in the list, action menu and edit view.
- **Actual:** the two distinct records render identically, and the edit forms show no distinguishing identifier or scope. Selecting Disable/Delete therefore relies on row position rather than a user-visible identity.

## Impact

A user can probe or change credentials on the wrong upstream connection, or disable/delete the wrong connection. This is a discoverability and operational safety issue; CP76 found no authorization bypass, data leak, probe, or mutation.

## Facts and hypotheses

**Observed facts:** two different persisted IDs share the same project and all relevant display fields; both rows and edit forms are visually indistinguishable; both expose destructive actions.

**Hypotheses, not established causes:** creation may allow duplicate names; the UI may omit IDs/creation metadata by design; backend actions may still target the correct hidden row ID. CP76 did not test duplicate creation or action targeting because no mutation was necessary to establish the user-facing ambiguity.

## Acceptance criteria

1. Either reject duplicate connection names within their relevant scope with a clear inline error, or display sufficient stable context (for example, project and a short stable ID or creation timestamp) to distinguish records.
2. The same identity remains visible in edit/confirm views for Probe, Disable and Delete.
3. A real-browser regression with two same-name records proves the chosen action targets only the selected record and the list refresh reflects its persisted state.
4. Keep secret values write-only and masked; never expose credential plaintext to solve the identity problem.

## Limits

This is a low-severity UX/operational defect confirmed only for the duplicate state observed in QA data. Google reachability was not tested in CP76; no Google provider failure is attributed to Tanpopo from this finding. No product source was changed. `GATE-REL-07` remains **BLOCKED**.
