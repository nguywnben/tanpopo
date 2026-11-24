# DEF-036 — Marketplace offers a duplicate install and exposes a storage error

**Status:** CONFIRMED on QA-002 frozen baseline `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Severity:** Low  
**Affected workflow:** Extension Marketplace install/review and installed package upgrade discovery

## Reproduction

1. Sign in to QA runtime `18084` as the QA Org Owner/Operator and open **Extensions → Marketplace**.
2. Locate the **Google Gemini Official Adapter** (`@tanpopo/gemini`) and choose **Stage for review**. At the time of the test, the runtime already had two active extension installations.
3. Observe the HTTP 409 response and the red page banner. It exposes `storage: unique constraint conflict: package "@tanpopo/gemini" is already registered in state ENABLED_ACTIVE (use StageUpgrade)`.
4. Query `/admin/v1/extensions/installations?limit=100` before and after the action. Both reads returned 200 and the same two `ENABLED_ACTIVE` installations; no package state or installation record changed.

The same failure was reproduced on two real UI attempts. Marketplace and error states were captured and visually inspected in `../evidence/CP99-marketplace.png`, `../evidence/CP99-google-stage-review.png`, and `../evidence/CP99-duplicate-stage-error.png`. The final attempt's sanitized request outcome and before/after installation state are in `../evidence/CP99-duplicate-stage-result.json`; the first attempt's rendered state is preserved in `../evidence/CP99-stage-review-inspection.json`.

## Expected vs actual

- **Expected:** A catalog item that is already installed should be identified as installed and should not offer a duplicate install action. If a catalog version is eligible for an upgrade, the UI should route the user through the supported upgrade and permission-review flow. Any failure should be rendered as concise user-facing guidance.
- **Actual:** The Marketplace presents the same **Stage for review** action without regard to installation state. Selecting the existing Gemini package returns HTTP 409 and displays raw storage/constraint wording; the user is told to use `StageUpgrade` without a corresponding action in the Marketplace row.

## Impact

This creates a dead-end and exposes an internal persistence error when a user follows the visible Marketplace action for a package already installed. It can confuse package lifecycle management and hides the intended upgrade path. No package was installed, changed, activated, or removed, and no authorization boundary was affected.

## Root-cause hypothesis (not established)

The Extensions page filters catalog rows by search and publisher/capability filters, but the Marketplace stage handler submits the selected catalog artifact as an install without first reconciling it with the installation list. The server's uniqueness guard correctly prevents a duplicate, but its conflict text is passed to the page as-is. This is consistent with the visible behavior; it is not a claim about the complete server implementation.

## Acceptance criteria

1. Marketplace rows are reconciled with installed package identity and version.
2. A same-version installed package is labeled installed and does not offer duplicate staging.
3. An eligible newer version exposes a supported upgrade action that preserves staged permission diff, independent review, and activation controls.
4. A race or duplicate attempt remains safely rejected without changing persistent state, but the user sees a clear message and a direct recovery path rather than database/implementation terminology.
5. Regression covers installed, not-installed, upgrade-available, stale-catalog/race, and disabled/quarantined lifecycle states.

`GATE-REL-07` remains **BLOCKED**; QA-002 remains in progress.
