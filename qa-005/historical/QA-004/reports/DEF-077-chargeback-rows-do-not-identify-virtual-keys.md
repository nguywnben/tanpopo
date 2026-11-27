# DEF-077 — Virtual Key chargeback rows are not distinguishable when names repeat

**Severity:** Moderate  
**Status:** FAIL (confirmed on frozen RC6)  
**Affected baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Affected workflows:** FinOps → Chargeback by Virtual Key; CSV/JSON chargeback export; key-level spend investigation and audit.

## Summary

The Virtual Key chargeback rollup keeps separate rows for separate Virtual Keys, but exposes only each key's display name as the row's `group_key`. Multiple keys with the same name therefore produce visually and machine-wise indistinguishable chargeback records. This is especially common for Playground sessions, which RC6 labels `Playground session` by default.

## Reproduction

1. Sign in as the synthetic Org Owner or FinOps Viewer in the isolated RC6 runtime. Use the QA test organization containing prior synthetic Playground requests.
2. Open **FinOps → Chargeback** and choose **Virtual Key** in **Group by**.
3. Inspect the rows named `Playground session`. Eleven separate rows were present; each showed Exact `$0.000001`, Estimated `$0.000000`, and `1 / 0` exact/estimated requests.
4. Export both CSV and JSON. Each contained 42 rows. Eleven records had `group_key: "Playground session"` and identical one-micro/one-request figures. The `UsageChargebackRow` fields were only `group_key` and the five charge/count fields; neither export included a Virtual Key ID or other stable disambiguator.
5. Read the corresponding project/environment Virtual Key catalog. It contained 13 different key IDs named `Playground session` (active and revoked); the 11 usage-bearing rows were distinct groups, not one combined name aggregate. With the key identifier omitted, the export cannot be correlated to the specific key metadata or lifecycle.

## Expected behavior

Each per-key chargeback row should include a stable, non-secret Virtual Key identifier (or a unique masked prefix) alongside its user-facing name. The table and CSV/JSON exports should expose the same disambiguator so operators can reconcile spend to one credential without revealing plaintext secrets.

## Actual behavior

Eleven separate rows show the same name and same amount/count. The API/export schema contains only the display label and aggregate values, so there is no reliable way to tell which credential each row represents. Access & Keys lists distinct credentials, but their IDs are not linked from the chargeback rows.

## Impact

An organization cannot reliably attribute spend to one of several same-named keys, including default-named ephemeral Playground keys. This weakens cost reconciliation and incident investigation and can lead an operator to inspect or revoke the wrong credential. Aggregate arithmetic remained consistent in this sample; the defect is per-key attribution.

## Evidence and scope

- [CP136 sanitized role, scope, export, and duplicate-row results](../evidence/CP136-finops-scope-and-key-attribution-results.json)
- [Owner chargeback grouped by Virtual Key](../evidence/CP136-chargeback-group-virtual-key.png)
- [FinOps Viewer chargeback in the canary scope](../evidence/CP136-finops-viewer-chargeback.png)
- Owner CSV export: 42 records, SHA-256 `4fe35f00f4a3510950c9eae9aa1029a08358766881251a2f171d3cb389e8d734`
- Owner JSON export: 42 records, SHA-256 `e37c57a9ec2b41856cf758eb97b6aaa4206e40e85d8254fb07e120b6d0e6c8e3`

The Owner and FinOps Viewer Org-level rollups were consistent across `QA-004 Isolated Canary / qa004-scope-canary-env` and `default / dev`; that aggregate behavior matches the RC6 organization-scope contract and is not classified as a defect. A separate project-scoped Developer saw zero usage in its empty canary scope, zero rows when attempting a sibling project/env filter, and HTTP 403 `insufficient_role_permission` for the sibling budget read. These are scoped authorization PASS results. No product data was changed and no inference request was sent during CP136.

## Root-cause hypothesis (not independently proven)

The RC6 query groups by the internal `virtual_key_id` but projects `vk.name` into `group_key`, and the response DTO has no ID field. This explains why rows stay separate but become indistinguishable for name collisions. Confirm the intended grouping identity and public data contract with the API owner before changing behavior.

## Acceptance criteria

- Two keys with the same display name and different usage produce separately identifiable rows in the UI and both export formats.
- Each row includes a stable key ID or a non-secret masked identifier plus the human-readable key name.
- The UI's column heading, API schema, CSV header, and JSON schema agree on that identifier.
- No plaintext key material is included; existing exact/estimated/unmetered separation and totals remain unchanged.
- Verify Viewer access and confirm project-scoped Developer data remains restricted to its own key scope.
