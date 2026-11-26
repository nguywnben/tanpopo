# Engineering handoff — DEF-015: chargeback export ignores the selected grouping

**Severity:** Moderate (exported report uses the wrong aggregation key)  
**Status:** Confirmed in isolated 8083 Console; no product code changed.

## Reproduction

1. Open FinOps → Chargeback and select **Model** in Group by.
2. Confirm the visible row is keyed by `llama3-2-qa-local`.
3. Click **Export JSON**.
4. Inspect the downloaded JSON and request URL.

## Evidence

- `../evidence/phase64-export-selected-model-group.png` and `phase64-export-selected-model-result.png`: model group is visible before and after export.
- `../evidence/phase64-selected-group-export.json` and `phase64-export-after-model-group.json`: selected group is Model but exported `group_key` is `qa-fresh-project`; request URL explicitly sends `group_by=project`.

## Expected vs actual

- **Expected:** export uses the currently selected Group by dimension, or the UI clearly labels the export as always project-grouped.
- **Actual:** both CSV/JSON export wrapper uses `group_by: 'project'` irrespective of the selector. The table is grouped by Model, but downloaded report is grouped by Project.

## Affected workflow

Finance/operator exports for model, environment, or virtual-key reconciliation do not match the visible table and may be joined or interpreted under the wrong dimension. The controlled tenant has one project and one model, so totals match; the `group_key` mismatch is directly observed. Multi-project/multi-model aggregation impact remains untested.

## Suggested investigation

Pass the selected `groupBy` to `exportUsageChargeback`; make the selected dimension visible in export metadata/filename if needed. Add tests that compare each grouped table with its export.

GATE-REL-07 remains BLOCKED.
