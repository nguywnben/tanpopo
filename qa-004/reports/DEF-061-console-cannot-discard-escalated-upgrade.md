# DEF-061 — Console cannot discard a permission-escalated extension upgrade

**Severity:** Moderate  
**Status:** FAIL  
**First found:** QA-004 CP46  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Affected workflow:** review/reject an extension permission escalation; return an installation to its prior active version after a candidate is parked for re-consent.

## Summary

When an extension upgrade adds permissions, RC6 correctly keeps the current version active and transitions to `UPDATING_PENDING_RECONSENT`. The permission review dialog can be closed, but the Console provides no action to reject or discard the staged candidate. The documented `DELETE /admin/v1/extensions/{installationId}` API does discard it and restores the prior active version, but operators must leave the normal Console workflow and call the API directly.

## Reproduction

1. Start a fresh, isolated RC6 test runtime with a local archive root and a synthetic Owner/Operator. Do not use a production signing key or release artifact.
2. Stage and activate a minimal declarative package under an unreserved test namespace. Explicitly acknowledge its `unverified_external` provenance and the initial permission set.
3. Open **Extensions → Installed → Actions → Upgrade package**. Stage a digest-pinned candidate whose only permission delta adds a reserved `.invalid` host.
4. Observe HTTP 202, state `UPDATING_PENDING_RECONSENT`, candidate version populated, and the original active version still active.
5. Inspect the permission dialog and close it with **Cancel**. Reopen the row action menu.
6. Observe that the row remains `UPDATING_PENDING_RECONSENT`. The available items are Review permissions, Upgrade package, and Quarantine package; there is no Reject/Discard candidate or Uninstall action.
7. As an independent positive control, use the documented API from the authenticated browser with its CSRF token and permission-diff ETag: `DELETE /admin/v1/extensions/{installationId}`. The API returns 204; a subsequent GET shows `ENABLED_ACTIVE`, the original active version, and `candidate_version=null`.

Reproduction evidence is in CP46 and `evidence/CP46-extension-escalated-review.png`, `evidence/CP46-extension-escalated-actions-no-discard.png`, `evidence/CP46-extension-escalation-discarded.png`, and `evidence/CP46-extension-lifecycle-results.json`.

## Expected and actual

- **Expected:** an authorized operator can reject/discard the escalated candidate from the Console. The action returns to the prior active version without applying new permissions, uses the server's optimistic concurrency/CSRF controls, and leaves an auditable decision.
- **Actual:** the modal's Cancel only closes the dialog. The installation remains pending; the normal row menu has no discard action. The API supports the workflow, but using it directly is required to clear the pending state.

## Business impact

An operator who declines a permission escalation cannot complete the normal Console workflow to restore the installation to an ordinary active state. The current active version remains serving, so immediate traffic availability and permission integrity were preserved in this reproduction. However, upgrade management is stuck in a pending state, and a subsequent attempt to stage the same immutable candidate was blocked as already registered. Users who cannot call administrative APIs directly need engineering/operator assistance.

## Facts and root-cause hypothesis

**Verified facts:**

- A 0.1.0 → 0.1.1 synthetic upgrade adding `extra.qa004.invalid` returned 202 and persisted `UPDATING_PENDING_RECONSENT` while 0.1.0 remained active.
- The actual pending-state row menu omitted a discard action.
- The documented DELETE endpoint returned 204 and restored active 0.1.0 with no candidate.
- Audit events included `extension.escalated_upgrade_rejected`; the Console hash-chain verification reported integrity across 15 events.

**Hypothesis:** `ExtensionsPage.tsx` conditionally offers Uninstall only for `INSTALLED_DISABLED`, `QUARANTINED`, and `PENDING_PERMISSION_APPROVAL`, and does not define a distinct reject/discard candidate row action for `UPDATING_PENDING_RECONSENT`. This matches the observed UI. Confirm implementation ownership during engineering triage.

## Acceptance criteria

1. In `UPDATING_PENDING_RECONSENT`, an authorized operator can invoke an explicit **Reject upgrade** or **Discard candidate** action in the Extensions Console.
2. The action is distinct from closing the permission review. If the review's Cancel button only closes the dialog, the UI must say so and leave the pending status visible.
3. Discard/rejection retains the previous active version and approved permissions, clears the candidate, and returns the installation to `ENABLED_ACTIVE`.
4. The action follows existing CSRF and `If-Match` protections, reloads authoritative state on conflict, and produces a linked audit decision.
5. The refreshed row and permission review reflect the persisted state; no new permission becomes active without explicit re-consent.
6. Regression coverage exercises both the approval path and rejection path, including refresh and stale ETag conflict handling.

## Test boundary

CP46 used an unsigned, minimal declarative manifest under `@qa004/lifecycle-canary`, with only reserved `.invalid` hosts and no executable package code. The Console displayed and required explicit acknowledgement of unverified external provenance. No production signing material, real provider credential, production data, or non-test release signature was used. The local app was offline. This is not evidence of production package signing readiness.
