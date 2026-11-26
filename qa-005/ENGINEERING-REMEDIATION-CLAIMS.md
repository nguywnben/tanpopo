# Engineering remediation claim intake — Rounds 1–4

This file records source-history claims to target independent QA. It does not establish that the fixes work. Every matching DEF-001–079 status in QA-005 remains UNVERIFIED until fresh runtime evidence is recorded.

| Round | Source commits / change summary | Findings/features explicitly targeted | QA-005 status |
|---|---|---|---|
| Round 1 | Initial defect remediation history is being reconciled from QA-001 and source commit history | DEF-001–015 and other Round 1 targets per historical reports | UNVERIFIED |
| Round 2 | `9e5265b` backend remediation; `29735dd` Console remediation | DEF-016–039, plus DEF-005/007 gaps and proxy security | UNVERIFIED |
| Round 3 | `94d72c0` runtime remediation; `f311974` Console remediation | Workspace/routing/provenance/session, native ingress, erasure and security findings per QA-003 | UNVERIFIED |
| Round 4 | `e384c12` FinOps/native/A2A/VKEY reaper fixes; `f409958` extension/security/CLI/signing-tooling fixes | DEF-051–053, 058–062, 067, 069–071, 073–079 | UNVERIFIED |

Round 1 exact source commits and the complete per-finding crosswalk remain to be extracted from the authoritative Git history and historical remediation handoffs. Round 4 descriptions are from commit subjects only until associated implementation and QA reports are fully reviewed.
