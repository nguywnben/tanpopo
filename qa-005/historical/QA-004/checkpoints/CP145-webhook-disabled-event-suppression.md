# CP145 — Disabled webhook event suppression

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** exact RC6 SQLite listener `127.0.0.1:18128`  
**Actor:** synthetic Org Owner in the isolated QA test-root organization  
**Provider spend:** $0  
**Product source modified:** no

## Outcome

- **PASS (scoped):** Registered an enabled, organization-scoped webhook subscription for `kill_switch.triggered` and `mcp.approval.pending` against a public example hostname using a synthetic write-only HMAC credential. The initial Console form and row were visually inspected.
- **PASS (scoped):** Disabled the subscription through its real Console row action. The UI showed the disabled state and success notice; authenticated API readback returned `enabled=false`, version 2.
- **PASS (scoped):** Triggered a project freeze for the existing isolated `qa004-isolation-canary` project. The Console showed one entity affected and zero streams severed. A read-only query to the active RC6 SQLite database, scoped to the synthetic organization and subscription and excluding payload, found **zero webhook delivery rows** after the event.
- **PASS (scoped):** Cleared the kill switch through the real Console. The screen showed zero active switches; authenticated readback confirmed no active switch remained.
- **PASS (scoped):** Deleted the subscription through the Console and confirmed zero matching subscription and delivery rows. Removed both temporary same-name HMAC credentials through the Console; both returned HTTP 204.
- **No new defect:** This closes the narrow disabled-subscription event-suppression gap noted in CP69. Receiver delivery, HTTP signing/acknowledgement, retries against reachable receivers, PostgreSQL worker behavior, project-bound subscriptions, concurrency and other event types remain unverified.

## Reproduction

1. Sign in as the isolated test-root Org Owner and select organization-only scope.
2. In Providers → Vault Secrets, use a synthetic organization-owned `WEBHOOK_HMAC_SECRET`.
3. In Operations → Webhooks, register `https://example.com/qa004-cp145-disabled-webhook` for the displayed kill-switch and MCP approval events.
4. Disable the subscription through its Actions menu and confirm the persisted state/version.
5. In Security → Kill Switches, trigger a project freeze for `qa004-isolation-canary` with an explicit reason and slug confirmation.
6. Read `webhook_deliveries` in read-only mode from `/tmp/qa004-rc6-a2a-data/tanpopo.db`, filtering by exact organization and subscription IDs. Select event type/status/count metadata only; do not read payload or credential material.
7. Clear the test switch, delete the subscription and temporary synthetic credentials through the Console, and verify no active switch, subscription or delivery row remains.

Sanitized IDs and result summary are in [CP145 evidence](../evidence/CP145-disabled-event-suppression.json). Actual screenshots were visually inspected: [registration form](../evidence/CP145-webhook-create-form.png), [disabled subscription](../evidence/CP145-webhook-disabled.png), [switch active](../evidence/CP145-kill-switch-triggered.png), [switch cleared](../evidence/CP145-kill-switch-cleared.png), [delete confirmation](../evidence/CP145-webhook-delete-confirm.png), and [cleanup state](../evidence/CP145-webhook-cleanup.png).

## Limits and campaign status

The receiver URL is a placeholder and no successful outbound request was attempted. This PASS establishes event suppression while disabled for one organization-scoped subscription and one kill-switch event on the SQLite runtime. It does not establish delivery behavior or broader webhook acceptance. No new defect was assigned; historical and new defect counts are unchanged. QA-004 remains ongoing and **GATE-REL-07 remains BLOCKED.**
