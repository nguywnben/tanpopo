# CP10–12 — RBAC, pricing boundary, backup and restore

**Campaign:** QA-005 / TEEP-00 baseline stabilization  
**Source:** `737079ddb847d97dbcf832f610ed0048556cb65b`  
**Runtime:** freshly compiled exact-source Linux amd64 binary `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; isolated SQLite on `127.0.0.1:18145`; synthetic OpenAI-compatible provider on `127.0.0.1:19090`; Chromium 1440×1000.  
**Checkpoint result:** PARTIAL campaign evidence; one new low-severity defect confirmed. Not a completion or release verdict.

## FinOps Viewer authorization and UI state (CP10)

Provisioned and enrolled a synthetic principal with **only** FinOps Viewer. In FinOps, the user could read the exact 1-micro-USD usage and could not create or edit budgets. `GET /admin/v1/members` returned 403 `insufficient_role_permission`. Direct navigation to Security showed “Access restricted.” These are correct scoped denials; they are not defects.

On Access & Keys → Virtual Keys, `GET /admin/v1/virtual-keys` returned 403 `insufficient_role_permission`; nevertheless, the rendered page displayed both “Environment Virtual Keys 0” and “No Virtual Keys in this environment.” The owner has issued keys in that environment. I visually inspected the captured screen. The red authorization error and the zero/empty result contradict each other. Recorded as **DEF-080 (Low, FAIL)**. It does not expose key data or weaken authorization. Reproduction and acceptance expectation are in the defect register.

Provider read view displayed the configured loopback endpoint and no bound Vault credential; no secret value was present. Further role-based metadata disclosure expectations remain unverified.

The Add Member form preselects Developer and allows multiple roles. Leaving this default while selecting FinOps Viewer grants both roles. A pure Viewer required clearing all roles first. No reviewed accepted spec says this default is forbidden; recorded separately as **PG-001** for product-contract/usability review.

Screenshots: `CP10-finops-viewer-only-form.png`, `CP10-finops-viewer-only-pending.png`, `CP10-finops-viewer-finops.png`, `CP10-finops-viewer-security-denied.png`, `CP10-finops-viewer-access-route.png`, `CP10-finops-viewer-providers.png`.

## Exact pricing and project budget boundary (CP11)

Used a loopback synthetic provider and self-hosted billing classification; no external provider or paid usage. Configured one deployment at input 15 and output 25 nano-USD/token and a project hard budget of 1 micro-USD. The deterministic response reported 17 input and 6 output tokens. Arithmetic is `17×15 + 6×25 = 405 nano-USD`; conservative ceiling to micro-USD settled exactly 1 micro-USD. The usage/charge appeared in FinOps and the successful request in Traces. A second request returned 429 `project_budget_exceeded`; fixture access log confirms that denied request did not reach the provider. Traces persisted the denied request as unsettled. Screenshots were visually inspected.

This is **PASS for this narrow accounting/budget slice**, not for pricing migrations, concurrent reservations, other modalities, credential-level budgets, refunds, exports, or SQL engines beyond SQLite. Note: a second logical model was added but the deployment form retained its default model; the priced deployment therefore targeted the original logical model. This was a test-operator selection error, not product evidence.

Screenshots: `CP11-priced-deployment-active.png`, `CP11-priced-route.png`, `CP11-finops-exact-pricing-and-boundary.png`, `CP11-priced-trace-and-rejected-request.png`.

## Encrypted SQLite backup and restore (CP12)

Created a valid empty version-1 test lockfile in the isolated runtime because the server had no configured on-disk lockfile. This is a test fixture and is not source or historical evidence. Created encrypted backup `eddef129149688268806e6dcee30796f` (43,815 bytes; SHA-256 `27561c394a3af7ea45b645ecb122bb68a77d1cf40bdba449d42781824901aae3`) from the running runtime, verified the archive, restored into a previously absent directory with `--verify-secrets-decrypt`, and ran `doctor` with explicit restored database/lock paths.

**PASS for archive integrity, clean-destination restore, SQLite integrity, secret-envelope trial decrypt, and restored structural checks.** Source and restored databases both report `PRAGMA integrity_check = ok`, `journal_mode = wal`, and one organization. Doctor returned `pass` for ports, KEK readiness, storage/FS lock, bundle/lockfile integrity, and egress/SSRF. It returned `warn` for unconfigured expected clock and loopback TLS posture; cluster cache was `skipped` because this is standalone. The first trial with an empty, malformed lockfile failed the doctor lock check; corrected the test fixture to a schema-valid empty lockfile and reran successfully. This is an environment-fixture correction, not a product defect.

Limitations: no actual provider credential was sealed, so trial-decrypt did not exercise a real credential; no restored server was started; no concurrent write/snapshot, archive tamper, retention, PostgreSQL/Valkey, Windows, or crash/restart recovery was tested. Encrypted backup and restored database remain in `/tmp/qa005-runtime` and are excluded from QA evidence.

Machine-readable, sanitized receipt: `evidence/CP12-backup-restore.json`.

## Campaign status

DEF-001–079 remain UNVERIFIED unless separately changed by a fresh acceptance run; no historical PASS was inherited. Official RC7 artifact verification remains UNVERIFIED, production signing remains BLOCKED, and real provider interoperability remains BLOCKED/not attempted. Broad historical regression, PostgreSQL/Valkey, MCP/A2A, other roles, cross-tenant isolation, audit/telemetry, accessibility/browser/platform breadth, and operational failure modes remain incomplete. GATE-REL-07 remains BLOCKED.
