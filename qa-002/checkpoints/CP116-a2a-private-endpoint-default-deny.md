# CP116 — A2A private endpoint default-deny registration

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated Tanpopo `18084`  
**Browser/role:** real Chromium, 1440 × 1000; QA Org Owner + Operator  
**Product implementation modified:** No  
**Release gate:** `GATE-REL-07 BLOCKED`

## Test

In the MCP Hub's **A2A Agents & Active Tasks** tab, opened the actual Register agent drawer in project `QA002 CP78 Fresh Budget`, with **No env** selected. Entered synthetic slug `qa002-cp116-default-deny`, display name `QA002 CP116 Default Deny`, and endpoint `http://127.0.0.1:19189`. Left **Allow private or loopback endpoint** unchecked and submitted once. The address was a loopback fixture address from CP114; its fixture was stopped. No A2A discovery, task dispatch, key grant, or call to the endpoint was attempted.

## Result

The UI sent one `POST /admin/v1/a2a/agents`. The API returned HTTP 400 `invalid_endpoint_url` with `https endpoint_url is required unless allow_private_ip is true`. Follow-up `GET /admin/v1/a2a/agents?limit=100` returned 200 and confirmed the test slug was not persisted. The drawer remained open and the UI displayed the validation error in its red page banner; no record or endpoint side effect occurred.

Screenshots were visually inspected: `../evidence/CP116-a2a-private-default-deny-form.png` shows the exact loopback URL and unchecked exception, and `CP116-a2a-private-default-deny-result.png` shows the rejection and unchanged six-agent list. Sanitized request/response and persistence evidence is `../evidence/CP116-a2a-private-default-deny.json`; request payloads, credentials and secrets are not retained.

## Disposition

**PASS, scoped:** the A2A registration path rejects a private loopback URL unless the explicit trust exception is enabled. This confirms the default-deny control at the server boundary; it does not weaken or bypass SSRF authorization. It complements CP08/CP68's separate explicit-CIDR allowlisted positives. No new defect was found. DEF-005 remains PARTIAL for mixed DNS responses, resolution/connection races, redirect variants, other transports/runtimes, and further A2A private-address forms. The registration form permits submission and relies on the API for this validation; the visible banner provided the failure reason. No desktop error-occlusion defect is assigned here.

## Consolidated status summary

- **Historical DEF-001–015:** only scoped QA-002 outcomes are retained; no QA-001 PASS is inherited. DEF-005 remains PARTIAL. DEF-007 successful audit paths do not cover fault atomicity, injected storage failures, or independent backup/physical erasure proof.
- **Existing DEF-016–038:** preserved unchanged. DEF-039 Low remains confirmed; no new product defect was identified in CP116.
- **Coverage completed:** real Chromium A2A registration attempt with a synthetic loopback address, explicit exception left unchecked, HTTP 400 and post-submit persistence check. Screenshot inspection confirms displayed error and no record.
- **Coverage blocked/remaining:** no A2A discovery/tool/task call is in scope for this registration-denial test; further DNS rebinding/mixed answers, resolver races, redirects, transports/runtimes, other roles, browsers and platforms remain unverified. Firefox/WebKit current-image runs are blocked as recorded in CP111.
- **Recommended handoffs:** no new handoff. Preserve server-side default-deny and explicit private-address authorization. Continue DEF-005 residual coverage and all open defect retests.

`GATE-REL-07` remains **BLOCKED**. QA-002 remains in progress.
