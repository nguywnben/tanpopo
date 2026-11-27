# CP106 — Existing QA extension-installation kill switch

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`
**Result:** existing isolated safety state confirmed and preserved; no defect assigned.

While comparing Ops state across runtimes, the 18124 QA Owner showed a Security navigation badge of one. The real Security → Kill Switches UI and authenticated `/admin/v1/kill-switches` readback confirmed exactly one active `EXTENSION_INSTALLATION` switch targeting the QA installation, with reason `QA-004 adapter quarantine acceptance probe`, version 1. The screenshot was visually inspected. This is a prior QA safety control, not an unexplained product alert. It was not cleared or otherwise mutated.

Evidence: [authenticated UI and kill-switch readback](../evidence/CP106-runtime18124-kill-switch-tab.png). No source changes. **GATE-REL-07 remains BLOCKED.**
