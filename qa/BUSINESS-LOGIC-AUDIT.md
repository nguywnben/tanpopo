# Business Logic Audit

## Checkpoint 2026-11-23

No business-rule behavior was exercised against a running system. Tenant isolation, budgets, billing arithmetic, retention, policy precedence, approvals, failover, and audit integrity remain unverified. Repository specification crosswalk is in progress; code inspection alone will not be reported as behavioral validation.

## Checkpoints 8, 12–16 — runtime business-rule observations

- FinOps configuration: Admin API created an isolated organization budget; Console listed and edited it; negative hard-limit input was rejected without a request. First-budget creation is unavailable in the Console (DEF-003). Holds, enforcement, settlement, thresholds, concurrency and chargeback remain untested.
- Tenant scope: an isolated second organization had no project/environment visible. A non-operator Team Admin could list projects in its bound org (200) and received a generic 404 when targeting the other org. This covers one read route only; write, pagination and resource-ID cross-scope checks remain open.
- Role assignment: Team Admin Operations access was denied in the UI and the organizations API returned 403. Its visible Add member form failed because it could not supply required project IDs (DEF-006).
- Audit/backup: runtime organization/project/environment creates left no corresponding audit entries; CLI backup verification failed for the empty-chain organization (DEF-007). Console backup returns 503 because the unified serving binary does not pass its config path (DEF-008).
- Provider readiness: the existing DEF-002 remains reproducible; activation can mark an unverified provider route healthy although the data plane returns 503. Real Google inference remains blocked by the guarded-egress/proxy path.

GATE-REL-07 remains BLOCKED. This crosswalk is still partial; WF-01..13 and FT-01..16 are not individually complete.

## Checkpoint 17 — operational business rules

- Readiness/cluster rule: live Operations UI reported `Ready` / `Serving traffic`, config epoch 23, `sqlite_wal`, one standalone node marked `OK`. This is a single-node snapshot, not failover or restart evidence.
- Upgrade rule: compatibility preflight for Core `0.2.0` returned `READY_TO_UPGRADE`; no upgrade was applied and no extension lockfile lifecycle or rollback behavior was exercised.
- Webhook rule: empty subscriptions; registration form requires a public HTTPS receiver and a sealed write-only `WEBHOOK_HMAC_SECRET`. With no matching credential available, the UI correctly disabled submission. Delivery signing, retry, and receiver failure semantics remain untested.
- Audit/recovery rule: Security UI verified the existing global chain across 172 events. The separately observed empty chain for the new org remains DEF-007. Restoring the encrypted QA backup into a fresh destination with correct KEK and secret-decryption verification failed on that organization's missing verifiable audit history; destination was not published. Backup does not currently provide a successful restore path for this tested state.

Evidence: `evidence/phase17-operations-interactions.json`, `phase17-restore-attempt.json`, and visually inspected phase 17 screenshots. GATE-REL-07 remains BLOCKED.

## Checkpoint 21 — restore conditional on audit history

After a QA Owner was added to the second org and authenticated, the org had legitimate `member.created` and `auth.login_succeeded` audit events, while the org/project/environment creation operations still had no corresponding events. Under this non-empty-chain state, CLI backup create, verify, and restore with secret-decryption validation all succeeded; the restored DB passed SQLite integrity checks and a newly staged restored server accepted owner login and rendered Overview. The previous empty-chain restore failure remains valid. This shows backup recovery is conditional on audit-chain availability, and later activity does not satisfy the business requirement to record provisioning mutations. Evidence: `evidence/phase21-backup-successful-recovery.json`, `phase21-restored-runtime-smoke.json`, and visually inspected screenshot `phase21-restored-runtime-owner.png`.

## Checkpoint 22 — kill-switch blast radius and lifecycle

For a QA-only `CREATED_UNVERIFIED` provider connection, blast-radius preview returned zero organizations, projects, virtual keys and connections. The owner confirmed the emergency action; the switch activated at epoch 24 with one entity affected and no streams severed. Clear succeeded and the active count returned to zero. The test supports the zero-impact preview and reversible trigger/clear business path only. Other target levels, populated blast radius, multi-entity cascade, stream interruption and concurrent state propagation remain unverified. Evidence: `evidence/phase22-killswitch-journey.json`; screenshots visually inspected. GATE-REL-07 remains BLOCKED.

## Checkpoint 23 — audit lifecycle consistency

The Audit Log showed the trigger and clear records for the same kill-switch lifecycle in order; Verify hash chain returned success across 189 events. This confirms those two action events were appended and included in the currently verifiable chain. It does not verify event-detail payload completeness, deliberate tamper detection, or the independent tenancy provisioning gap (DEF-007). Evidence: `evidence/phase23-post-clear-audit-verify.json` and inspected screenshot.

## Checkpoint 24 — empty kill-switch form guard

In Firefox 153 and WebKit 26.5 at 375×844, opening the Project-level switch form without setting target ID, reason, or confirmation slug left Review & trigger disabled and active switch count at zero. No mutation was issued. This is only the client-side empty-form guard; server-side required-field validation and allowed target/role combinations remain open. Evidence: `phase24-killswitch-cross-engine-mobile-sanitized.json` and visually inspected screenshots.

## Checkpoint 25 — provider validation state transitions

The active runtime's unavailable validation runtime produced HTTP 503 and did not alter the connection from `CREATED_UNVERIFIED`. The restored runtime's allowed-origin validation endpoint returned HTTP 200 at the transport/API level while the validation verdict was `upstream_unreachable`; the connection also remained `CREATED_UNVERIFIED`. A rejected proxy-origin attempt returned 403 before mutation. These results show no false promotion for this retest, but do not establish provider failure classification across all adapters or successful validation. Evidence: `phase25-provider-egress-retest.json`.

## Checkpoint 26 — provider validation consistency across runtimes

All three retained the provider connection as `CREATED_UNVERIFIED`; 8080 and 8082 returned an explicit negative validation result, and 8081 returned a runtime-availability error without promoting it. This is consistent fail-closed state behavior despite different transport/runtime failures. The shell proxy success versus no-proxy timeout and Tanpopo's guarded transport policy indicate an environment compatibility issue; a safe explicit proxy path is still unverified. Evidence: `phase26-provider-runtime-comparison.json` and inspected per-runtime screenshots.

## CP27 regression notes

- First-budget behavior remains blocked: the empty default-org FinOps state offers no create action (DEF-003).
- Tenant member creation continues to contradict its visible Team Admin affordance: project scope cannot be entered, and the API rejects submission (DEF-006).
- The private-endpoint checkbox for MCP and A2A cannot express the required `allowed_cidrs` contract; both forms fail before side effects (DEF-005).
- Console backup still exposes an enabled action that fails because runtime backup configuration is unavailable to the handler (DEF-008).
- No new business-logic defect assigned this phase. Budget accrual/settlement, concurrent limits, billing reconciliation, approval lifecycle and provider failover remain untested; GATE-REL-07 remains BLOCKED.

## Checkpoints 32–34 — A2A authorization mismatch

The Console supports registering A2A agents and displays an A2A discovery/task surface, yet the standard Virtual Key issuance flow cannot grant the A2A plane or route access. With the runtime feature flag enabled, the normally issued key was rejected by A2A discovery while provider authorization remained its only plane. This is DEF-010 and blocks the A2A user journey before task creation; a targeted handoff is recorded. Runtime flag absence itself returned a disabled-plane 404 and is an environment configuration condition. GATE-REL-07 remains BLOCKED.
