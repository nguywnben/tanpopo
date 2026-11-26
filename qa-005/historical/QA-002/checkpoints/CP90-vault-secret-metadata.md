# CP90 — Vault secret metadata and plaintext exposure check

**Baseline:** frozen RC4 `40bba5b55c2da6fc00b107b315c9b3167fb20169`  
**Runtime:** isolated QA runtime `http://127.0.0.1:18084`  
**Role/browser:** Organization Owner, real Chromium at 1440×900  
**Gate:** `GATE-REL-07 BLOCKED`

## Journey and result

1. Signed into the live Console and navigated Providers → Vault Secrets.
2. Read the authenticated `GET /admin/v1/secrets` response in the browser session and compared the field names to the visible table.
3. Did not reveal, rotate, delete, or rebind the existing credential.

The list contained one pre-existing QA Google AI Studio API-key credential. The Console showed its name, kind, trailing fingerprint, credential ID, and a Delete action. The API returned HTTP 200 with metadata keys `created_at`, `credential_id`, `name`, `project_id`, `secret_kind`, `trailing_fingerprint`, and `version`; it contained no `secret_value` field or credential-shaped value. Screenshot inspected visually. The existing secret was left untouched because it may be bound to a QA connection.

## Classification and limits

- **PASS (narrow):** owner list view and API metadata did not expose plaintext for the one existing record.
- **UNVERIFIED:** broader `TP-AC-SEC-01` scans across all Admin APIs, CLI, provider forms, error/log paths and all roles; actual binding and rotation; OAuth/PKCE lifecycle; secret deletion recovery; side-channel/fingerprint requirements.
- **No defect assigned:** the visible trailing fingerprint and opaque credential ID match metadata-only behavior; no plaintext was observed.
- No mutation was performed.

## Evidence

- `evidence/CP90-vault-metadata.png` — real UI, visually inspected.
- `evidence/CP90-vault-metadata.json` — sanitized response key names/status and exposure checks.

## Consolidated status

- **Historical DEF-001–015:** no new retests in CP90; scoped prior statuses remain, DEF-005 is partial, and independent acceptance coverage remains incomplete. No QA-001 PASS is inherited.
- **Confirmed open defects:** DEF-016–034 remain open; no status changed. Preserve DEF-034 and the DEF-020 recurrence.
- **New findings:** no new defect. A narrow owner Vault metadata view showed no plaintext.
- **Coverage completed:** actual Vault Secrets screen plus authenticated metadata API comparison, without mutating the existing credential.
- **Blocked:** full secret-privacy contract still needs a credential-bearing end-to-end log/API scan; full OTLP fail-open test lacks known collector endpoint/access. Webhook delivery remains managed-egress blocked; runtime 18445 A2A is configuration-disabled; RC4 artifact is unverified in separate DevBox.
- **Coverage remaining:** independent DEF-001–015 acceptance; DEF-005 SSRF edge cases; DEF-007 populated erasure/failure atomicity; secret binding/revoke/OAuth and cross-role checks; all open DEF-016–034 remediation/regression; rest of product/browser/platform/integration/accessibility/operations coverage.
- **Recommended handoffs:** unchanged—DEF-034, DEF-016/023/029/030, DEF-033, DEF-005 without weakening SSRF, and DEF-007. No handoff from CP90.

QA-002 remains in progress.
