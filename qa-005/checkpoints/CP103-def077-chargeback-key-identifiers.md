# CP103 — DEF-077 Virtual Key Chargeback identifiers

**Source/runtime:** frozen unsigned RC7 source `737079ddb847d97dbcf832f610ed0048556cb65b`; runtime binary SHA-256 `62aba8761eae2733b1e8957b0b09cde45dee4fb2c29fa6eb36294ce29a600fe9`; exact-source SQLite through TLS proxy `https://127.0.0.1:19444`; Chromium 156; `ORG_OWNER`; QA005 Org / QA005 No Budget Project / QA005 Budgetless Dev.

## Result

**PARTIAL (fresh exact-RC7 UI/API/CSV/JSON retest).** In FinOps → Chargeback grouped by Virtual Key, the real Console showed 10 rows. The two same-named `Playground session` rows were separately identified by a masked key prefix and full non-secret Virtual Key ID in addition to their name. The authenticated UI's actual `GET /admin/v1/usage/summary?group_by=key` returned HTTP 200 and separate `group_id` values. Actual Export CSV and Export JSON actions each returned HTTP 200. Both file formats carried `group_id` and `key_prefix`; the two duplicate-name rows retained distinct IDs/prefixes. The CSV had 10 data rows; the JSON array had 10 rows, and every JSON row contained both disambiguators. UI and export screenshots were visually inspected. No credentials, key plaintext, or provider calls were used.

This current data contains same-name keys with the same observed usage values (each one exact request and zero micro-USD), so it does not meet the accepted distinct-usage control. Keep DEF-077 PARTIAL. Historical QA-004 evidence of 11 same-name, distinct-key rows remains preserved.

## Reproduction

1. Sign in to the exact-source Owner Console and open FinOps → Chargeback.
2. Set Group by to Virtual Key. Inspect repeated `Playground session` names in the rendered table; verify each row presents the masked prefix and stable `vky_…` identifier.
3. Use Export CSV and Export JSON. Inspect their headings/field names and compare duplicate-name rows by `group_id` and `key_prefix`.
4. Observe the actual successful UI request `GET /admin/v1/usage/summary?group_by=key` (HTTP 200) and verify it returns distinct key IDs for repeated names.

Evidence: [Chargeback table](../evidence/screenshots/CP103-chargeback-virtual-key.png), [post-export UI](../evidence/screenshots/CP103-chargeback-virtual-key-export-state.png), and [sanitized result/hash record](../evidence/CP103-def077-chargeback-identifiers.json). The exported files themselves are not committed; they contain only synthetic aggregate data and their captured hashes are recorded in the JSON.

## Reconciliation and remaining scope

The historical QA-004 failure described a schema with display name only. Current RC7 UI and both export formats expose stable, non-secret IDs and masked prefixes in the tested scope, so the old indistinguishability is not reproduced for the observed duplicate rows. The run did not create controlled duplicate-name keys with different usage, compare another role/scope/backend, or test CSV/JSON import/open behavior. Those acceptance paths remain UNVERIFIED. A first manual probe guessed the API enum `group_by=virtual_key` and got HTTP 400; that malformed query was excluded as a harness-contract mistake after the real UI request established the valid `group_by=key` parameter and returned 200. No conclusion is drawn from the invalid probe. `GATE-REL-07` remains BLOCKED.
