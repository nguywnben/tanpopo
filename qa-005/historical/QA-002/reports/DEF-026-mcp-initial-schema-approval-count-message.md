# DEF-026: MCP sync success notice omits newly discovered locked schema

**Severity:** Low (P3)
**Status:** Confirmed on the frozen QA-002 baseline
**Affected workflow:** MCP Hub → register server → sync tools → review and pin a newly discovered tool schema.

## Reproduction

1. Sign in as the synthetic organization owner and select the project/environment used for the test.
2. Register an isolated Streamable HTTP MCP endpoint with `Allow private IP` enabled and the explicit CIDR `127.0.0.1/32`.
3. Use a local fixture that returns one read-only `safe_status` tool from `tools/list`.
4. Select **Sync tools**.

## Expected

The sync result should tell the operator that the newly discovered schema is locked pending its first review, or clearly scope the zero count to *schema drift changes* so it cannot be read as saying no approval is needed.

## Actual

The UI displayed: “Synced 1 tools on QA002 CP47 private sync control; 0 schema changes require approval.” At the same time, the server row said **Schema Drift Locked**, the tool detail said **Drift locked**, and an **Approve schema** action was available. Selecting that action opened a first-review dialog for the observed schema hash and allowed a risk classification; after pinning, the UI confirmed “Pinned schema hash and classified risk tier (read_only).”

The tool remained locked until the owner explicitly pinned it. This is a count/status communication defect, not an authorization bypass: no MCP `tools/call` was made, and no tool was granted to a Virtual Key.

## Evidence and scope

- Visually inspected screenshots: `../evidence/CP47-owner-private-mcp-synced.png`, `../evidence/CP47-owner-first-schema-approval-form.png`, and `../evidence/CP47-owner-first-schema-approved.png`.
- Sanitized request/fixture outcome summary: `../evidence/CP47-mcp-private-and-scope-boundary.json`.
- Owner approval used the UI's default `read_only (no HITL required)` risk selection. Approval and follow-up sync returned success; the tool then displayed **Pinned** and the server **Active**.
- Team Admin cross-project sync was denied with HTTP 403 before the fixture received a request; this remains a passing authorization boundary, not a defect.

## Business impact

An operator can interpret the green success banner as evidence that the newly discovered tool is ready without approval, while the server/tool rows show a locked state. The lock prevents dispatch, so the observed risk is operator confusion and avoidable follow-up, not unauthorized tool execution.

## Acceptance criteria

1. On first discovery, the sync result explicitly reports the number of newly discovered schemas awaiting initial review, or labels the existing count as “schema drift changes” and separately reports first-review locks.
2. The success banner and the server/tool rows describe the same post-sync approval state.
3. A newly discovered tool remains non-callable and ungrantable until an authorized reviewer pins its schema; copy changes must not weaken this control.
4. A regression test covers initial discovery, unchanged resync, schema drift, and successful first pin, asserting both persisted lock state and user-visible counts.
