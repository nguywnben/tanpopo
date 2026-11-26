# CP46 — Extension upgrade, re-consent, discard, and rollback lifecycle

**Status:** PARTIAL overall; positive and negative extension upgrade lifecycle slices exercised. **DEF-061 FAIL** for the missing Console discard control. Full extension lifecycle/security acceptance remains incomplete.
**Baseline:** frozen RC6 artifact from run 45581179274, source baseline `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`.
**Product source:** unchanged. **GATE-REL-07:** BLOCKED.

## Environment and trust boundary

- Used a new, isolated RC6 process on `127.0.0.1:18129`, its own SQLite data directory, and a local archive root under `/tmp/qa004-upgrade-testroot/`. Existing 18124/18128 test runtimes were not changed.
- Created a fresh synthetic owner, organization, project, and environment in the real Chromium setup wizard. Runtime remained air-gapped.
- Built a minimal declarative test package `@qa004/lifecycle-canary`; it contains a manifest only, no executable Wasm or adapter code. Its only host permissions are `api.qa004.invalid` and, for escalation candidates, `extra.qa004.invalid`; both are reserved `.invalid` names, TLS is required, and private IP access is disabled.
- The package is **unsigned / unverified external**. The real Console displayed that status and required explicit acknowledgement. Both permission consent and unverified-source acknowledgement were checked in the UI. This is test-only fixture evidence, not a production signature, and does not change release-signing readiness.

## User journeys and results

1. **Fresh install 0.1.0 — scoped PASS.** From the Extensions screen, staged a digest-pinned `tanpopo-local://v0.1.0.tpkg`, reviewed the host, execution tier, TLS/private-IP settings, unsigned provenance warning, and explicit consent controls, then approved and activated it. API readback showed `ENABLED_ACTIVE`, active version `0.1.0`, `unverified_external`, and `signature_verified=false`.
2. **Escalated upgrade 0.1.1 — scoped PASS for staging/containment; FAIL for Console recovery UX.** The real Upgrade package drawer staged 0.1.1, adding only `extra.qa004.invalid`. API returned HTTP 202 and `UPDATING_PENDING_RECONSENT`; the UI showed the added host, while persistent state retained active 0.1.0. The review dialog offered Cancel and Approve & activate. Cancel closed the dialog but left the package pending. The row menu contained Review permissions, Upgrade package, and Quarantine package, but no Reject/Discard candidate action and no Uninstall action.
3. **Candidate discard — API PASS; Console FAIL (DEF-061).** The authenticated browser fetched the session CSRF token and permission-diff ETag, then issued the documented `DELETE /admin/v1/extensions/{installationId}` request. It returned HTTP 204 and durable readback returned `ENABLED_ACTIVE`, active `0.1.0`, no candidate, and no rollback pin. The audit log included `extension.escalated_upgrade_rejected`. The operator had no equivalent UI action.
4. **Non-escalating upgrade 0.1.2 — scoped PASS.** The real UI staged the same-permission version. The API returned HTTP 200; persistent state became `ENABLED_ACTIVE` 0.1.2 with previous rollback version 0.1.0. The row menu then exposed Rollback version.
5. **Rollback — scoped PASS.** The UI displayed the rollback target (0.1.0) and affected projects (`None`), required an audit reason, and completed rollback. Refreshed Console and API readback returned active 0.1.0, with no candidate or rollback pin.
6. **Positive escalation re-consent — scoped PASS.** To avoid reusing the already rejected immutable 0.1.1 artifact, staged fresh version 0.1.3 with the same added `.invalid` host. The permission diff again blocked auto-activation. After reviewing the exact delta and explicitly acknowledging both the added permission and unsigned provenance, Approve & activate completed; readback showed active 0.1.3 and previous rollback 0.1.0. The real rollback action then restored 0.1.0.
7. **Audit persistence — scoped PASS.** The real Security → Audit Log page showed the install, permission approval, re-consent staging, rejection, upgrade, and rollback lifecycle events. Verify hash chain reported “Cryptographic RFC 8785 JCS chain intact across 15 events.”

Retrying the exact already-rejected 0.1.1 archive returned HTTP 409 `resource_already_exists` because its `(package_id, bundle_sha256)` version row was already registered. The active 0.1.0 state did not change. The Console mapped that response to “This extension package is already installed. Open Installed extensions to inspect or upgrade it,” even though the operator was already in the installed package's Upgrade drawer and the package itself was active. This is recorded as low-severity DEF-062, separate from the absent discard action. A fresh higher test version was used for the positive re-consent path.

## DEF-061 summary

The backend and OpenAPI contract support discarding a staged candidate, including while it is `UPDATING_PENDING_RECONSENT`, but the Extensions Console offers no way to do so. Closing the permission review is not a discard; the row remains pending and the active version remains 0.1.0. See [DEF-061](../reports/DEF-061-console-cannot-discard-escalated-upgrade.md).

The duplicate immutable candidate response is technically correct as a uniqueness conflict, but Console copy gives the wrong next step. See [DEF-062](../reports/DEF-062-duplicate-candidate-error-misleads.md).

## Evidence

- `evidence/CP46-extension-install-review.png`, `...-install-consented.png` — fresh install provenance and consent reviewed visually.
- `evidence/CP46-extension-escalated-review.png` — actual 0.1.0 → 0.1.1 permission delta, inspected visually.
- `evidence/CP46-extension-escalated-actions-no-discard.png` — actual pending-upgrade row menu, inspected visually.
- `evidence/CP46-extension-escalation-discarded.png` — active 0.1.0 after API discard, inspected visually.
- `evidence/CP46-extension-active-v012.png`, `...-v012-actions.png` — active 0.1.2 and newly available rollback action, inspected visually.
- `evidence/CP46-extension-rollback-confirm.png`, `...-rollback-result.png` — reasoned rollback confirmation and persistent 0.1.0 result, inspected visually.
- `evidence/CP46-extension-reconsent-checks.png`, `...-escalation-approved.png` — explicit re-consent and active 0.1.3, inspected visually.
- `evidence/CP46-extension-audit-chain.png` — actual 15-event log and successful integrity verification, inspected visually.
- `evidence/CP46-extension-rejected-version-retry.png` — 409 duplicate-version response rendered as a misleading already-installed message.
- `evidence/CP46-extension-final-state-v010.png` — final active 0.1.0 state after recovery, inspected visually.
- `evidence/CP46-extension-lifecycle-results.json` — sanitized stage responses, persistent readbacks, provenance boundary, and audit-event summary.

## Remaining coverage

Only a minimal declarative synthetic package and one Owner/Operator context were exercised. Production release signatures remain BLOCKED. Package integrity tampering, invalid signatures, broader roles, concurrent upgrades, interrupted activation, process restart between stages, drain/failure behavior, referenced extension rollback, uninstall, quarantine recovery, cross-org boundaries, functional provider calls through a third-party package, and production OCI registry upgrade are UNVERIFIED. This checkpoint does not complete extension acceptance or QA-004.
