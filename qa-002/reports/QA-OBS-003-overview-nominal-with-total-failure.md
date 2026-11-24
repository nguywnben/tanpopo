# QA-OBS-003 — Overview says nominal alongside a 100% error rate

**Classification:** Unresolved UX/business-contract observation; not yet a confirmed defect.  
**Baseline:** `40bba5b55c2da6fc00b107b315c9b3167fb20169`.  
**Environment:** disposable fresh-install SQLite runtime on 18086.

## Reproduction

1. Complete a fresh bootstrap without an initial provider/model.
2. Sign in as the new Owner and open Playground.
3. Enter synthetic model slug `no-such-model-cp17` and a harmless one-line prompt; submit.
4. Observe the Playground reports `Playground inference failed (503)`.
5. Open Traces, FinOps → Chargeback, then Overview.

## Evidence and actual behavior

- `evidence/CP17-empty-playground-error.png`: Playground visibly reports a 503; no success response is shown.
- `evidence/CP17-empty-traces-after-503.png`: persisted metadata-only row shows `503`, `no_compliant_deployment_available`, `Exact`, and `0 ms`.
- `evidence/CP17-empty-chargeback-after-503.png`: exact and estimated settled amounts, active holds and unmetered requests all remain zero; no usage rows appear.
- `evidence/CP17-overview-after-failed-playground.png`: Overview shows 1 request, 100.00% error rate, zero spend/holds and zero active alerts, while also showing “All systems nominal.”

The screenshots were visually inspected after interaction. The user prompt is not exposed by the metadata-only trace. No Tanpopo implementation was changed.

## Expected versus actual

**Expected question:** should a dashboard that reports a 100% request error rate also call the system nominal, or should it qualify that phrase as alert posture rather than request health?

**Actual:** the Overview calculates request error rate separately from active operational alerts. With no configured deployment and a single deliberately invalid model slug, it shows the 100% error KPI but still renders “All systems nominal,” with explanatory text “No active operational alerts or pending remediation items.”

The error is a caller-selected nonexistent model, so no alert may be appropriate. The word “nominal” can nevertheless be read as system readiness, especially in a fresh install with no working model. The documented alert thresholds and intended distinction between request health and operational alert posture have not been reviewed, so this is not assigned a DEF ID yet.

## Follow-up

Compare an invalid-model request, a provider outage, and a healthy successful request against alert contracts and Overview copy. Decide whether to clarify the label or alert logic only after those comparisons. Do not count this as a confirmed defect or a release pass.

## Independent recurrence — CP33

On the accumulated QA-002 SQLite runtime, WebKit at 375×844 showed 13 requests and a 53.85% internal/upstream error rate, zero active alerts, and “All systems nominal” with “No active operational alerts or pending remediation items.” This state differs from CP17's single deliberately invalid model request and 100% error rate, but it independently reproduces the KPI/copy tension. See `evidence/CP33-webkit-overview-375.png`, sanitized `evidence/CP33-webkit-mobile.json`, and `checkpoints/CP33-webkit-mobile-console-regression.md`. Because the runtime contains accumulated intentional negative testing and the meaning of this alert copy is still unspecified, classification remains an unresolved observation.
