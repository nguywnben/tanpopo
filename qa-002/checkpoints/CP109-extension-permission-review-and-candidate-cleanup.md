# CP109 — Extension permission review and staged-candidate cleanup

**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated RC4 QA Console on 18084  
**Result:** **PASS, scoped; no new defect**  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## User journey

From the real Extensions Marketplace, staged the uninstalled `@tanpopo/openai-compatible` package through its **Stage for review** action. The API returned HTTP 202. The installed list showed a new candidate at version 0.1.0 in **Pending Permission Approval**, with no active version. The real permission review showed the origin/trust tier, unsigned publisher state, declarative execution tier, and permission deltas, including localhost/private host entries and a write-only `api_key` slot.

In the pending permission review, the acknowledgement was unchecked and **Approve & activate** was disabled. No permission was approved and the package was never activated. No provider credential was entered and no provider call or package execution occurred. This preserved the SSRF boundary while testing the explicit approval guard.

Canceled the review and used **Uninstall package** with the required audit reason to remove the staged candidate. The authenticated request returned HTTP 204; after refresh the candidate row was absent and Installed returned to its prior count of two. Screenshots were inspected before and after cleanup. The permission-delta view and clean uninstall confirmation are retained in [evidence](../evidence/CP109-extension-review.json), [review screenshot](../evidence/CP109-extension-permission-review.png), and [cleanup screenshot](../evidence/CP109-extension-candidate-cleanup.png).

## Coverage disposition

- **PASS, scoped:** Marketplace stage action and persisted Pending Permission Approval state; permission delta and publisher/origin display; unchecked-consent disabled-activation state; audit-reason uninstall and persisted candidate removal.
- **No defect:** the candidate requested private-host permissions for a self-hosted provider adapter, and the UI displayed this delta before requiring explicit consent. No authorization or SSRF bypass was tested or inferred.
- **UNVERIFIED:** signing/trust verification, role-separated or multi-reviewer approval, approval while preserving disabled state on this exact package, activated runtime, blind secret broker, private egress enforcement, upgrades, rollback, quarantine, restart/lockfile persistence, other packages and browser/platform behavior.

## Consolidated status

- **Historical DEF-001–015:** retain scoped QA-002 results only; DEF-005 remains partial; DEF-007 successful populated-shred paths pass while injected faults/atomicity and independent ciphertext/backup erasure proof remain unverified. No QA-001 PASS is inherited.
- **Open historical/new findings:** DEF-016–037 remain preserved/open, including CP107 DEF-023 confirmation. DEF-038 High remains confirmed from CP108. CP109 changes no defect disposition and adds no defect.
- **Coverage completed this phase:** real Chromium marketplace stage and permission review; consent-disabled state; audit-reason uninstall; before/after installed count persistence; screenshots visually inspected.
- **Blocked/remaining:** broad Alpha/Beta coverage remains incomplete across defect retests, product workflows, roles, screens, browsers, integrations, runtime isolation, provider/secret execution, operational recovery and platforms. Extension activation/security behavior remains deliberately untested here because it would require granting the candidate's private-host permissions.
- **Recommended handoffs:** no new handoff from CP109. Continue DEF-038 with DEF-007, and retain the existing DEF-023/024/029/030/035 workspace and scope handoffs, DEF-034/033, DEF-005 without weakening SSRF, and DEF-037.

QA-002 remains in progress. `GATE-REL-07` remains **BLOCKED**.
