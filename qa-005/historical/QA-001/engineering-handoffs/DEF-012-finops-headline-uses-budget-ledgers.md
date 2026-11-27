# Engineering handoff — DEF-012: FinOps headline omits settled usage without budget rows

**Severity:** Major (financial visibility and reconciliation)  
**Status:** Confirmed on isolated fresh-install runtime 8083; no product code changed.

## Reproduction

1. Bootstrap an isolated Tanpopo instance with a new organization, project, and environment.
2. Do not create any budget ledger.
3. Configure a healthy synthetic provider, model deployment, route, and an environment-scoped virtual key.
4. Send one successful provider request whose exact reported usage settles to 1 micro-USD.
5. Open FinOps → Chargeback and group by Project.

## Evidence

- `../evidence/phase47-finops-chargeback.png` and `.txt`: same screen shows headline Exact settled `$0.000000` while the project row shows Exact `$0.000001`, `1 / 0` exact/estimated requests.
- `../evidence/phase46-local-inference.json`: successful synthetic request, 13 prompt + 10 completion tokens.
- `../evidence/phase51-finops-kpi-chargeback-mismatch.json`: read-only DB readback, one `EXACT` settlement worth 1 micro-USD, zero budget ledgers.

## Expected vs actual

- **Expected:** headline settled totals summarize actual settled usage represented by the chargeback rows, regardless of whether a budget has been configured.
- **Actual:** all headline spend metrics display zero when there are no budget ledger rows, even though the chargeback table and usage settlement contain an exact nonzero charge.

## Affected workflows and impact

Operators reviewing current spend, reconciling costs, or using the FinOps summary to decide whether usage occurred can see a false zero. This is especially likely for first-use tenants where budgets are optional or not yet configured. The exact row remains available, but the headline summary contradicts it.

## Technical evidence / suggested investigation

`web/console/src/pages/finops/FinOpsPage.tsx` derives `totalExact`, `totalEstimated`, and `totalHolds` by reducing `budgets`, while chargeback rows come from `getUsageChargebackSummary(groupBy)`. The UI therefore couples spend KPIs to the presence and scope of budget records. Derive settled and unmetered KPIs from the usage summary endpoint (or a dedicated summary API), and define hold aggregation independently. Add an empty-budget/nonzero-settlement regression case.

## Verification boundary

This repro used an isolated local provider fixture and one exact 1-micro-USD settlement. No external provider call was made. GATE-REL-07 remains BLOCKED.
