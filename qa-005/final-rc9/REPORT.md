# TEEP-00 RC9 Final Foundation Verification

**Campaign:** Bounded independent verification of Engineering Round 6; not a comprehensive QA restart or release certification.

**Frozen source:** `ac9ae87a68f095203de82968ac5d08729e33a531`
**Engineering remediation:** [PR #51](https://github.com/nguywnben/tanpopo/pull/51), merged to `main` at the frozen source commit on 2026-11-27
**Prior independent evidence:** `qa/qa-005-closeout-rc8` at `7073e2fecfdabf45f5a8d445d8d4b3b4c1f22889`; CP125
**Post-merge CI:** [46048372404](https://github.com/nguywnben/tanpopo/actions/runs/46048372404), success
**RC9 artifact run:** [46048372483](https://github.com/nguywnben/tanpopo/actions/runs/46048372483), artifact `15224031447`
**Evidence branch:** `qa/qa-005-final-rc9`

## TEEP-00 exit recommendation

# BASELINE_STABLE

The RC9 baseline is sufficiently stable to begin TEEP-01 product and technology research. This is a bounded recommendation based on the Round 6 foundation targets below. It does **not** declare QA-005 complete, certify production, or authorize release. Historical partials and release-certification gaps remain open. `GATE-REL-07` remains **BLOCKED**.

## Executive results

- **DEF-081 — PASS (scoped).** The original forced-RLS global quarantine failure from RC8 was not reproduced. Actual RC9 runtime activation, adapter registration, live SSE and NDJSON stream termination, barrier enforcement, outbox recording, unquarantine, re-enable and restart persistence passed in an isolated PostgreSQL 16 / Valkey Cluster configuration. Eight targeted PostgreSQL source-integration tests also passed, including global operator resolution, authorization denials, four-eyes self-approval rejection and cross-organization inventory.
- **DEF-082 — PASS (scoped).** In the real Console, an active extension switch appeared in Security with count 1. Clearing it through the Console changed the count to 0 immediately without a reload; the extension correctly remained quarantined until explicit recovery. After unquarantine and renewed permission approval, both adapters returned to `ENABLED_ACTIVE`; a subsequent runtime restart preserved that state and left zero active switches.
- **New defects:** none confirmed. No `DEF-087+` identifier assigned.
- **Product gaps:** PG-001–PG-014 are preserved. Added `ARCH-OBS-01` for organization-wide principal and audit lookup scalability; it is a TEEP-01 research observation, not a defect.
- **Historical PARTIAL findings:** carried without closure or downgrade. CP125's 15 partial entries remain visible in the RC9 overlay and residual register.
- **Release certification:** production signing remains deferred; local cryptographic verification of the GitHub attestation was unavailable. Windows/macOS/ARM and Safari/iOS coverage was not repeated.

## Artifact and source identity

The RC9 artifact was downloaded before its recorded expiration. Its `metadata.json` names source commit `ac9ae87a68f095203de82968ac5d08729e33a531`, version `0.0.1-snapshot`, build date `2026-11-27T18:26:58.446793854Z`, and `linux/amd64`. The Linux binary reported the same source commit and build time. Its SHA-256 is `1ad30a0d08f957ab92b6e5ca6facf2b7836f1fcc5df0271544740169261012e9`.

All five archive entries matched the artifact's `SHA256SUMS` before the non-Linux archives were removed from temporary storage; the Linux/amd64 archive digest is `36f0ac4aa57332bea3556b60a8f4d1891aa1c2ad4a1f482094f2c2be7bb1e01d`. Full manifest and provenance facts are in [artifact identity](evidence/artifact-identity.json).

Both CI runs completed successfully on the frozen commit. The artifact workflow's attestation job succeeded and an API attestation record was present. Local signature-chain verification remains **UNVERIFIED**: this environment's `gh 2.46.0` has no `gh attestation` command and `cosign` was unavailable. No production private key or signature was accessed.

## Runtime and method

Testing used the exact RC9 Linux/amd64 release binary, not a substitute build:

- PostgreSQL 16 with `FORCE ROW LEVEL SECURITY`; actual application login `qa005rc9_app` had `rolsuper=false` and `rolbypassrls=false`. `principals` had both row security and forced row security enabled.
- A healthy Valkey 8.1 Cluster of three primary nodes: `cluster_state=ok`, 16,384 slots assigned, zero failed slots.
- Actual Tanpopo runtime at `127.0.0.1:18181`, with successful `/healthz` and `/readyz` after restart.
- Real Chromium Console interaction, visual screenshot inspection, authenticated admin requests and HTTP inference clients.
- Deterministic local provider fixtures for OpenAI-compatible SSE and Ollama NDJSON. No paid provider calls or provider credentials were used.
- Source tests were compiled from an archive of the exact RC9 commit with Go 1.26.0 in `golang:1.26-alpine`. The PostgreSQL harness used a separate NOBYPASSRLS test role.

The initial Console state had no extensions, connections, models, deployments or keys. Both bundled adapters were staged and explicitly approved after the UI identified them as unsigned/unverified. The organization trust policy was explicitly enabled for the package IDs through the authorized ORG_OWNER policy API before creating project-scoped connections. An initial OpenAI probe used an incorrect `/v1` suffix, resulting in `/v1/v1/models`; correcting the configured base URL to the provider root produced successful probes. These were test setup/configuration corrections, not Tanpopo defects. Detailed setup identity is in [environment identity](evidence/environment-identity.json).

## Visual evidence

The screenshots were captured during real Chromium interactions and visually inspected during the run:

- [Fresh owner Console](evidence/screenshots/01-owner-overview.png), [permission consent](evidence/screenshots/02-openai-consent.png), [Ollama permission review](evidence/screenshots/03-ollama-permission-review.png) and [both active adapters](evidence/screenshots/04-adapters-active.png).
- [Provider probe results](evidence/screenshots/05-provider-probes.png).
- [Quarantined package in the Console](evidence/screenshots/06-console-quarantined.png), [one active switch](evidence/screenshots/07-console-killswitch-active.png), and [cleared switch with refreshed zero count](evidence/screenshots/08-console-killswitch-cleared.png).
- [Package remains quarantined after clearing the switch](evidence/screenshots/09-extension-quarantined-after-switch-clear.png).
- [Both active after restart](evidence/screenshots/10-active-after-restart.png), [zero switches after restart](evidence/screenshots/11-no-active-switch-after-restart.png), and [final active package state](evidence/screenshots/12-final-active-state.png).

## DEF-081 — PostgreSQL forced-RLS extension quarantine

### Expected

A canonical INSTANCE OPERATOR should be able to activate and register the OpenAI-compatible and Ollama adapters, quarantine an installation across organization-bound scope under PostgreSQL FORCE RLS, terminate active streams before further egress, preserve correct switch inventory and audit/outbox state, and recover without bypassing the barrier.

### Actual and evidence

1. In the real Console, both bundled packages activated after explicit permission review. The adapter registry listed `openai-compatible` and `ollama`. Each local connection probed successfully. Screenshots `02`–`05` show the permission review, active packages and provider probes.
2. Actual inference was sent through RC9 for each protocol: unified OpenAI-compatible `/v1/chat/completions` and native Ollama `/native/ollama/api/chat`. Each returned HTTP 200 and the local fixture's first streaming chunk.
3. Quarantining each corresponding installation returned HTTP 200 and immediately terminated its in-flight stream. The fixture recorded a client disconnect for both upstream calls.
4. With both packages quarantined, subsequent real requests to both ingress paths returned HTTP 503 `extension_quarantined`. The local fixture's POST count stayed at 4 before and after those rejected requests. No outbound call escaped the barrier. See [quarantine enforcement evidence](evidence/quarantine-admission-enforcement.json).
5. Both quarantine transitions persisted `extension.quarantined` and `kill_switch.triggered` webhook outbox events. PostgreSQL contained six of each after the repeated lifecycle runs; no webhook subscription was installed, so delivery was not exercised.
6. PostgreSQL source-integrated tests verified global switch inventory across organizations, forced-RLS operator identity resolution, and fail-closed denials for a non-operator, forged organization-bound operator binding, suspended operator, nonexistent principal, and cross-tenant clear probe. No explicit revoked-principal test was found or run; that subcase remains **UNVERIFIED**.
7. The authenticated bootstrap Console account exposed the canonical instance operator persona (`Operator, Org Owner`). The source tests also exercised the personal bootstrap INSTANCE OPERATOR identity. The attempted cross-org escalation by a forged org-scoped role was denied.

### Verdict

**PASS (scoped).** This closes the prior CP125 `quarantine_kill_switch_failed` reproduction for the exercised RC9 topology and organization-bound INSTANCE OPERATOR paths. The emergency barrier remained fail-closed. It is not evidence for every deployment topology, cluster failure mode or production scale.

## DEF-082 — unquarantine, badge freshness and restart recovery

### Expected

After containment is cleared, lifecycle state, active-switch inventory and Console badges should agree; re-enable and restart should restore adapters without leaving stale or active switches.

### Actual and evidence

- The Console Security page showed one active Extension Installation switch after quarantine. Screenshot `07-console-killswitch-active.png` shows its target, reason and count.
- Clearing the switch through the real Console returned HTTP 200. Without a page reload, the success state displayed “Kill switch cleared and logged,” the active count changed from 1 to 0, and the navigation badge cleared. Screenshot `08-console-killswitch-cleared.png` captures that state.
- Clearing the emergency switch did **not** reactivate the package: the OpenAI-compatible package remained `QUARANTINED` (screenshot `09`). This is safe separation of containment clear from explicit extension recovery.
- The authorized unquarantine action returned HTTP 200 and moved the package to `INSTALLED_DISABLED`; explicit permission review and activation returned HTTP 200 and `ENABLED_ACTIVE`.
- Both adapter registrations and active lifecycle states persisted across a runtime restart. Post-restart real HTTP requests completed successfully through both adapters: the Ollama NDJSON response contained a fixture chunk and `done:true`; OpenAI-compatible SSE contained a fixture chunk and `[DONE]`. Response digests and content-type evidence are recorded in [Ollama](evidence/ollama-after-restart.json) and [OpenAI](evidence/openai-after-restart.json).
- Final runtime state: both packages `ENABLED_ACTIVE`; both adapters present; zero active switches; PostgreSQL organization audit chain verified intact with 114 checked events. The final Console state was visually inspected in screenshots `10`–`12`.

### Verdict

**PASS (scoped).** Immediate badge freshness passed for the real Console kill-switch clear flow. Other browsers, concurrent Console sessions and platform-specific notification timing were not tested.

## PR #51 change review

The merged source changes covered PostgreSQL global operator resolution, extension lifecycle/adapters, kill-switch trigger/clear and stream severing, audit verification, and webhook dispatch/outbox behavior. The complete changed-file list and review focus are in [PR #51 source review](evidence/pr51-source-review.md). The PR-authored tests were rerun from the exact RC9 source; actual runtime streams and Console operations supplied independent behavioral evidence.

## Round 6 security and audit results

### PostgreSQL forced-RLS test results

All targeted PostgreSQL tests passed (exact test names and execution record: [round6-test-results.md](evidence/round6-test-results.md)):

- Extension quarantine barrier, adapter recovery and denial paths.
- Kill-switch affected-entity counts across organizations.
- Cross-org four-eyes self-approval rejection.
- Bootstrap operator blast radius.
- Forced-RLS global quarantine and clear operator resolution.
- Negative authorization and tenant isolation.
- Filtered/global switch listing edge cases.
- Multi-org bootstrap operator extension lifecycle.

The restricted test role was used after `postgres.Open` correctly refused an initial SUPERUSER DSN. No test granted `BYPASSRLS` to the application or test role.

### Four-eyes independent approval

Cross-org self-approval returned the expected 403 `self_approval_forbidden` in a PostgreSQL test. A separate SQLite-backed HTTP harness confirmed that a distinct INSTANCE OPERATOR could successfully approve after a self-approval attempt was rejected. Successful independent approval on PostgreSQL remains **PARTIAL / UNVERIFIED** for this focused slice.

### Audit, inventory and containment webhook

- Actual RC9 org audit chain verification: `intact=true`, 114 events checked, no broken sequence reported.
- Switch inventory was visible across the organization and returned to zero after clear/unquarantine.
- Actual PG outbox persisted six `extension.quarantined` and six `kill_switch.triggered` events. The organization-scoped queue callback test passed, as did atomic rollback on a failed outbox write.
- No external webhook subscriber was configured; delivery, signatures at a receiver and retry behavior are **UNVERIFIED**.

## Bounded SQLite regression

The following source-compiled SQLite/in-memory harness tests passed: `TestInstallerAC1FullLifecycleAndFastRollback`, `TestDEF058_071_ReservedNamespaceAndUnquarantineLifecycle`, `TestAll4KillSwitchLevelsRestartHydrationAndTenantIsolation`, and `TestKillSwitchFailClosedRBACValidationAndCrossProcessWatcher`, plus the webhook queue/outbox tests listed above. These are bounded adjacent checks; no independent RC9 SQLite server was booted. SQLite platform/runtime coverage therefore remains **PARTIAL**.

## Historical ledger and defect register

The RC8 report, CP125, evidence, and original regression overlay remain intact in this branch's ancestry. The additive [RC9 regression overlay](registers/regression-ledger-overlay-001-086.csv) preserves all 86 historical rows and adds RC9 disposition columns. Only DEF-081 and DEF-082 receive new scoped results; every other row is marked `NOT RETESTED` here.

CP125's count of 61 PASS, 15 PARTIAL and 3 FAIL refers to its CP123 historical cohort of DEF-001–079. DEF-080–DEF-086 were separate Round 5 additions and are represented as their own rows in the CP125 overlay; they are not added to or used to rewrite CP123's cohort counts. The 15 CP123 PARTIAL IDs remain DEF-004, DEF-020, DEF-027, DEF-028, DEF-036, DEF-041, DEF-043, DEF-044, DEF-048, DEF-049, DEF-050, DEF-060, DEF-072, DEF-077 and DEF-078. CP125's scoped passes and historical failures are preserved without inheriting a new PASS from RC9.

The [coverage matrix](registers/coverage-matrix.csv) and [residual register](registers/residual-risk-register.md) separate tested behavior from untested coverage and confirmed defects. No additional failure was established, so no new defect record or remediation handoff was created.

## Product and technology intelligence

PG-001–PG-013 remain in the original [product gap register](../PRODUCT-GAP-REGISTER.md). PG-014 remains in the RC8 handoff. The additive [Carbon handoff](product-gap-handoff-carbon.md) preserves them and records `ARCH-OBS-01`: scalability and operating cost of organization-wide principal resolution and audit lookup as tenant and audit volume grow. The organization trust-policy setup behavior is recorded as workflow research context only; this campaign did not prove an absent UI path or assign a gap ID.

## Residual risk and certification boundary

Material items still open include historical PARTIAL findings, explicit revoked-principal verification, independent successful approval on PostgreSQL, actual webhook receiver delivery, live SQLite runtime coverage, tenant erasure recovery (DEF-041), cluster inventory and KEK lifecycle (DEF-072), financial rollup/Chargeback parity (DEF-078), cross-platform binaries, Safari/iOS, production signing and local cryptographic attestation verification. See the [residual register](registers/residual-risk-register.md).

No implementation or specification was changed. `main`, `develop`, historical QA branches and the owner signing manifest were not modified. No tag, release or production signature was created. No paid provider call was made. `GATE-REL-07` remains **BLOCKED**.
