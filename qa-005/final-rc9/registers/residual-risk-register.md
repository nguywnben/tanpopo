# Residual risk register — RC9 TEEP-00

This register adds RC9 status to CP125 without closing, waiving, downgrading or replacing historical records. `PARTIAL`, `FAIL`, and `BLOCKED` entries remain as recorded unless the row has explicit scoped RC9 evidence. All coverage statuses remain scope-bound.

| ID | Class | Status after RC9 | Remaining risk / untested acceptance | Recommendation |
|---|---|---|---|---|
| DEF-004 | Historical PARTIAL | PARTIAL; not retested | Preserve CP123/CP125 scoped acceptance and prior evidence. | Carry into later assurance; no new remediation claim. |
| DEF-020 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-027 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-028 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-036 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-041 | Historical PARTIAL | PARTIAL; not retested in RC9 | Destructive tenant erasure/recovery remains unverified. | Retain as material data-destruction and recovery assurance gap. |
| DEF-043 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-044 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-048 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-049 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-050 | Historical PARTIAL | PARTIAL overall; RC8 single-primary criterion passed, multi-target concern remains | RC9 did not retest. Keep DEF-083 separate. | Carry remaining RC8 scope as assurance gap. |
| DEF-060 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-072 | Historical PARTIAL | PARTIAL; not retested in RC9 | Expected-node inventory loss/rejoin and KEK safety/rewrap remain unverified. | Retain as material cluster and key-management assurance gap. |
| DEF-077 | Historical PARTIAL | PARTIAL; not retested | Preserve CP125 baseline status. | Carry into later assurance. |
| DEF-078 | Historical PARTIAL | PARTIAL; not retested in RC9 | Budget/settlement rollup parity, Chargeback UI and boundaries remain unverified. | Retain as material financial-integrity assurance gap. |
| DEF-081 | Round 6 target | PASS (scoped) | Verified RC9 paths and topologies only; no multi-region production cluster, provider-auth matrix, load or failover campaign. | Preserve RC8 failure history and RC9 scoped pass together; do not mark globally closed. |
| DEF-082 | Round 6 target | PASS (scoped) | Console clear count refreshed immediately without reload; broader browser/platform and concurrent UI session freshness remain untested. | Preserve RC8 BLOCKED history and RC9 pass together. |
| Security revoked principal | Residual assurance gap | UNVERIFIED | Executed suites cover denied/non-operator, suspended/inactive, forged, nonexistent and cross-tenant principals; an explicit revoked-principal fixture was not found/run. | Include in future security regression scope. |
| Successful independent approval on PostgreSQL | Residual assurance gap | PARTIAL | Cross-org self-approval denial passed on PG; independent approval passed in a separate SQLite-backed harness. | Verify approval succeeds with a distinct reviewer against PostgreSQL. |
| Containment webhook delivery | Residual assurance gap | PARTIAL | Outbox rows persisted in the RC9 PG runtime and rollback/queue harnesses passed; no actual subscriber delivery/receiver was configured. | Preserve delivery verification as open coverage. |
| SQLite runtime compatibility | Residual assurance gap | PARTIAL | Bounded SQLite-backed source tests passed; no separately booted RC9 SQLite server was used. | Do not infer whole-runtime SQLite coverage. |
| Artifact provenance attestation | Release-certification gap | UNVERIFIED locally | CI attestation jobs succeeded and a GitHub API record was present; local cryptographic verification unavailable with installed tooling. | Carry to release certification; no production signature or key was used. |
| Supported platforms and browser breadth | Release-certification gap | UNVERIFIED | This focused mission did not re-run Windows/macOS/ARM binaries, Safari/iOS, or broad browser/accessibility/responsive coverage. | Preserve prior coverage matrix and certify separately. |
| Production signing and canonical archive digest | Governance blocker | BLOCKED | Production signing remains deferred by user instruction; owner manifest branch untouched. | Keep `GATE-REL-07` BLOCKED. |
| Exhaustive product QA | Scope limit | NOT COMPLETE | This is targeted Round 6 verification, not a full Alpha/Beta campaign. | Do not label QA-005 complete or product release-ready. |
