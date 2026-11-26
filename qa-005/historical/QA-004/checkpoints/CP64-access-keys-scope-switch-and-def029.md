# CP64 — Access & Keys scope switching / DEF-029

**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Runtime:** isolated offline RC6 on `127.0.0.1:18132`  
**Actor:** synthetic Org Owner  
**Product changes:** none

## Result

- **PASS (scoped; DEF-029 retest):** The populated CP57/CP59 environment showed its one persisted, revoked MCP-only Virtual Key row. Switching to CP55/`qa004-cp55-staging-west` showed `No Virtual Keys in this environment` and zero rows. Switching back to CP57/CP59 restored the same revoked metadata row.
- **PASS (scoped):** Stable UI states matched environment-filtered API reads (HTTP 200). The UI queried the selected `project_id` and `env_id`. No stale row appeared in CP55 after its scope settled. The exact key plaintext was not read or recorded.
- **UNVERIFIED:** Other roles, populated-to-populated environments, rapid/concurrent switch races, active-key display, pagination, revoke/rotate lifecycle across scopes, and other browsers.

## Reproduction

1. Sign into the isolated RC6 Console as Org Owner, open **Access & Keys → Virtual Keys**.
2. At `QA004 CP57 Project Recovery / qa004-cp59-mcp-test`, observe one revoked MCP-only key row.
3. Select `QA004 CP55 Ops Billing`; the active environment becomes `qa004-cp55-staging-west`. Observe zero keys and “No Virtual Keys in this environment.”
4. Select CP57 and `qa004-cp59-mcp-test` again. Confirm the same revoked row reappears.
5. Compare the page's environment-filtered `GET /admin/v1/virtual-keys` requests (HTTP 200) and selected project/environment IDs.

## Evidence

- [CP55 empty environment](../evidence/CP64-access-keys-cp55-empty.png)
- [CP59 populated environment](../evidence/CP64-access-keys-cp59-populated.png)
- [Sanitized scope/request evidence](../evidence/CP64-vkey-scope-switch.json)

Screenshots were opened and visually inspected. DEF-029 is **PASS (scoped)**, not globally resolved.

`GATE-REL-07` remains **BLOCKED**; QA-004 is ongoing.
