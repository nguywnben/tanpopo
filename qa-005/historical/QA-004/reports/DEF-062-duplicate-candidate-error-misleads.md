# DEF-062 — Duplicate candidate version error misdirects extension upgrade users

**Severity:** Low  
**Status:** FAIL  
**First found:** QA-004 CP46  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Affected workflow:** retrying an already rejected/discarded immutable extension candidate.

## Summary

After an escalated candidate is discarded, attempting to stage the exact same immutable archive again correctly encounters the unique package-version constraint. The API responds with `409 resource_already_exists` and explains that the candidate version is already registered. The Console instead presents “This extension package is already installed. Open Installed extensions to inspect or upgrade it.” The active package is already on the Installed screen and the requested operation is already in its Upgrade drawer, so this message does not explain why the retry failed or how to recover.

## Reproduction

1. In an isolated RC6 test runtime, install a minimal unsigned declarative package and explicitly acknowledge its unverified provenance and permissions.
2. Stage an escalated candidate version and discard it through the documented candidate-discard endpoint.
3. Confirm the original version remains active and there is no candidate.
4. In **Extensions → Installed → Actions → Upgrade package**, submit the same source URI and digest again.
5. Observe HTTP 409 `resource_already_exists`; the error detail says `candidate version "0.1.1" already registered` after a unique `extension_versions.package_id, extension_versions.bundle_sha256` conflict.
6. Observe the Console error banner says the extension package is already installed and advises opening Installed extensions. The row remains `ENABLED_ACTIVE` on 0.1.0 with no candidate.

Evidence: `evidence/CP46-extension-rejected-version-retry.png` and the sanitized duplicate retry result in `evidence/CP46-extension-lifecycle-results.json`.

## Expected and actual

- **Expected:** preserve the safe active state and tell the user that this exact immutable candidate version was already registered or rejected. Provide an actionable next step, such as choosing a new version/digest or consulting the existing candidate history.
- **Actual:** HTTP 409 is shown as if the package itself cannot be upgraded because it is already installed, with a direction to the screen and upgrade action already in use.

## Impact and root-cause facts

This is a low-severity recovery/usability issue. The request fails closed and no active or candidate state changes. Users can proceed with a higher immutable version, but the Console message can cause avoidable support requests or repeated attempts.

Verified source behavior: installer duplicate-version insertion returns `port.ErrAlreadyExists`; the HTTP layer maps this to `resource_already_exists`. The Console's error translation maps the `already_exists` response to `extensions.errAlreadyInstalled`, whose text tells the user to open Installed extensions and upgrade. The source mapping aligns with the observed response. No product code was modified.

## Acceptance criteria

1. A duplicate package-version conflict remains safe and does not replace or activate a candidate.
2. The API error identifies the duplicate immutable version or equivalent actionable conflict.
3. The Console differentiates “package is already installed” from “this exact candidate version is already registered.”
4. Recovery guidance tells the operator how to proceed without directing them to the already-open Installed → Upgrade workflow.
5. The normal new-version upgrade path and pending-candidate discard path continue to work.
