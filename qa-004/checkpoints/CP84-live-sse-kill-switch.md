# CP84 — Active provider SSE kill-switch termination and recovery

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Outcome:** PASS (scoped) for a Security Reviewer virtual-key kill switch severing one active provider SSE request and Owner recovery. No new defect.  
**Product source:** unchanged. **Gate:** `GATE-REL-07` remains BLOCKED.

## Setup and positive control

In real Chromium, an Org Owner in `default/dev` issued a synthetic PROVIDER Virtual Key limited to `/v1/chat/completions`. The loopback-only Ollama-compatible fixture ran on port 18125; the original fixture was saved at `/tmp/qa004-ollama-mock.py` and restored after the test. The key's full value was retained only in a mode-0600 `/tmp` file, never added to the repository, then removed after cleanup.

A cookie-free browser `fetch` sent `stream=true` to RC6 `/v1/chat/completions` using model `qa004-mock` and the exact fixture trigger prompt. It returned HTTP 200 `text/event-stream`, delivered the complete synthetic stream (24 provider chunks plus `[DONE]`), and the fixture recorded `completed`. No external provider or spend was used. Evidence: [baseline stream](../evidence/CP84-baseline-stream.json).

## Active termination

The Security Reviewer selected the same `default/dev` scope, opened Security → Kill Switches, chose **Virtual key**, entered the exact test key ID and a reason, previewed a blast radius of one org, one project, one key and one connection, and left **Abort active streams immediately** checked. The actual confirmation warned that active streams would be terminated. Screenshots were visually inspected: [preview](../evidence/CP84-killswitch-preview.png), [confirmation](../evidence/CP84-killswitch-confirmation.png).

While the provider fixture was streaming, the Owner browser had received one event chunk. Reviewer confirmation returned HTTP 200 at epoch 145, with one affected entity and `severed_streams_count: 1`. The client stream closed after two delivered chunks; the fixture recorded `client_disconnected` after sending three chunks. The Console showed the active key switch and “1 streams severed”; its screenshot was visually inspected at [active switch](../evidence/CP84-killswitch-active.png). The persisted trace for this request is HTTP 200 with `mid_stream_abort`, 253 ms duration, exact/zero-cost usage projection. The Traces screenshot was visually inspected at [trace list](../evidence/CP84-traces-after-stream.png); sanitized records are in [persisted traces](../evidence/CP84-persisted-traces.json).

## Recovery and cleanup

The Org Owner cleared the switch in Security. The same key then completed a separate cookie-free local inference with HTTP 200. The active-switch list returned zero; all temporary CP84 keys were revoked and read back as `REVOKED`; [recovery/cleanup](../evidence/CP84-recovery-cleanup.json) records the final state. The clear confirmation screen was visually inspected at [cleared switch](../evidence/CP84-killswitch-cleared.png). The slow fixture was stopped, the original `qa004-mock` fixture was restarted and `/v1/models` returned its expected synthetic catalog. Temporary key files were removed.

## Harness corrections, not product findings

The first attempt captured a masked key prefix rather than the one-time secret and received 401 `invalid_virtual_key`; no fixture request was dispatched. A later baseline request explicitly requested region `US` from the `ON_PREM` runtime and received 503 `policy_denied_region`; retry without that mismatched override succeeded. One exploratory reviewer session remained in the sibling canary scope while targeting a default-project key; that run was invalid for this acceptance and its temporary switch was cleared. No result from those attempts is counted as a product defect or pass. Evidence: [region mismatch](../evidence/CP84-region-mismatch-control.json), [initial switch cleanup](../evidence/CP84-invalid-harness-cleanup.json).

## Coverage boundary

- **PASS (scoped):** one VKEY target, one authenticated OpenAI-compatible provider SSE stream, active-stream abort enabled, one local runtime, one chunk boundary, Reviewer trigger, Owner clear, and post-clear inference recovery.
- **UNVERIFIED:** project/provider/extension switch targets; MCP streams; multiple concurrent streams; alternate native dialects and providers; other chunk timing/client disconnects; multi-node propagation and latency; switch behavior across restart/crash; role-specific cross-scope preview and clear permissions; detailed settlement under nonzero stream usage.
- This does not complete the RC6 provider, RBAC, operational, browser/platform or release acceptance matrices. Continue QA and preserve **GATE-REL-07 BLOCKED**.
