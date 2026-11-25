# QA-004 — Comprehensive Release Acceptance

Frozen RC6 baseline: `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (PR #46). QA-only branch: `qa/qa-004-regression`.

QA-004 is an independent campaign. Historical QA-001/002/003 results are preserved as references; every one of DEF-001 through DEF-050 starts `UNVERIFIED`. New confirmed defects begin at DEF-051. Status values are `PASS`, `FAIL`, `PARTIAL`, `BLOCKED`, and `UNVERIFIED`. Evidence is sanitized and the execution data directory and test credentials are excluded.

## Current checkpoint

- [CP01 artifact, clean install, and provenance](checkpoints/CP01-artifact-install-provenance.md)
- [CP02 marketplace and authenticated native ingress](checkpoints/CP02-marketplace-native-ingress.md)
- [CP05 isolated test-root verifier and provider activation](checkpoints/CP05-test-root-extension-verifier.md)
- [CP06 synthetic provider, model, deployment and route setup](checkpoints/CP06-provider-model-route.md)
- [CP07 native Ollama end-to-end journey](checkpoints/CP07-native-ollama-end-to-end.md)
- [CP08 budgets, enforcement, and chargeback exports](checkpoints/CP08-finops-budgets-and-exports.md)
- [CP09 failed provider promotion guard](checkpoints/CP09-provider-failure-promotion-guard.md)
- [CP10 MCP/A2A controls and resumable task lifecycle](checkpoints/CP10-a2a-mcp-authorized-lifecycle.md)
- [CP11 FinOps Viewer RBAC](checkpoints/CP11-finops-viewer-rbac.md)
- [CP12 Developer role and catalog consistency](checkpoints/CP12-developer-role-and-catalog-consistency.md)
- [CP13 Team Admin project boundary](checkpoints/CP13-team-admin-project-boundary.md)
- [CP14 Security Reviewer audit workflow](checkpoints/CP14-security-reviewer-audit-workflow.md)
- [CP15 targeted kill-switch lifecycle](checkpoints/CP15-targeted-kill-switch-lifecycle.md)
- [CP16 responsive Chromium and browser availability](checkpoints/CP16-responsive-browser-coverage.md)
- [CP17 localization across the 15 listed languages](checkpoints/CP17-localization-overview-and-persistence.md)
- [CP18 project-scope isolation and DEF-054 retest](checkpoints/CP18-project-scope-isolation.md)
- [CP19 environment display-name persistence](checkpoints/CP19-environment-name-persistence.md)
- [Historical defect register](HISTORICAL-DEFECT-REGISTER.md)
- [Coverage matrix](COVERAGE-MATRIX.md)
- [New defects](NEW-DEFECT-REGISTER.md)
- [Release signing readiness](RELEASE-SIGNING-READINESS.md)

QA is ongoing. Five confirmed new failures (DEF-051–055) remain open; 37 historical findings are UNVERIFIED, 10 have scoped PASS, and three remain PARTIAL. CP16 added real Chromium 320/390/768-pixel responsive interaction and download evidence; Firefox and WebKit remain blocked by this runner's browser dependencies. CP17 selected each of the 15 listed UI locales and verified Vietnamese navigation and refresh persistence; other screens and localized error states remain unverified. CP18 added a second project-only Developer scope and independent DEF-054 evidence. CP19 confirmed environment display names are lost after creation/readback (DEF-055). No full product acceptance or release approval is claimed. **GATE-REL-07 remains BLOCKED.**
