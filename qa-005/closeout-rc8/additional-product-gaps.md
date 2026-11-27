# Additional product-gap observations

Preserve PG-001–PG-013 in the original [product-gap register](../PRODUCT-GAP-REGISTER.md). This focused closeout adds:

| ID | Area | Observation | User/business impact | Evidence | Classification |
|---|---|---|---|---|---|
| PG-014 | Team Admin Security permission feedback | Security displayed an `insufficient role permission` banner while showing “Pending MCP approvals 0” / “Approval queue clear” and a visible Trigger kill switch control. APIs denied inventory/operation with 403. | A user may interpret the empty state as verified inventory or spend time on unavailable actions. | [Screenshot](evidence/screenshots/rc8-teamadmin-security-denied-state.png); see [report](REPORT.md#new-defects-and-product-gaps). | No accepted UI contract found for this role-specific presentation. Research input, not a defect. |
