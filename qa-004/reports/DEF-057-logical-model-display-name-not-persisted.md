# DEF-057 — Logical model display name is lost on readback and refresh

**Severity:** Moderate  
**Status:** FAIL  
**Baseline:** frozen RC6 `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1`  
**Affected workflow:** Model catalog registration, discovery, and operator recognition.

## Summary

The logical-model form accepts a human display name distinct from the canonical slug. After a successful Org Owner create, the Console immediately shows the supplied name, but after a full page reload the catalog displays the slug in both the model and display-name columns. The authenticated model-list API likewise returns `display_name` equal to `slug`. The declared `embeddings` capability persisted, isolating the failure to display-name persistence/readback.

## Reproduction

1. In the fresh isolated RC6 Owner runtime (`127.0.0.1:18121`), sign in as Org Owner and open **Models → Register model**.
2. Enter display name `QA004 Owner Embeddings Catalog`, slug `qa004-owner-embedding-catalog`, context `128000`, max output `8192`, and select `embeddings`.
3. Submit. The UI reports successful registration and initially renders the human display name.
4. Reload the page and reopen **Logical Models**.
5. Observe the canonical slug as both the model label and its secondary display name. `GET /admin/v1/models?limit=100` returns `display_name: "qa004-owner-embedding-catalog"` with `capabilities.embeddings: true`.

## Evidence

- `evidence/CP39-owner-model-results.json` — role, HTTP 201 creation, persisted model fields, and after-refresh UI/API values.
- `evidence/CP39-owner-model-form.png` — distinct display name submitted.
- `evidence/CP39-owner-model-readback.png` — immediate UI state after creation.
- `evidence/CP39-owner-model-after-refresh.png` — real refreshed page showing the label replaced by the slug.

## Expected and actual behavior

**Expected:** the submitted human display name is persisted, returned by model-list/readback APIs, and remains visible after refresh or re-login, while the canonical slug remains independently addressable.

**Actual:** the immediate Console state shows the provided label, but a reload and the model-list API show the slug in both fields.

## Business impact

Model catalogs become harder to scan and distinguish when names are technical identifiers. API clients receive a display-name field that does not honor the submitted value, creating inconsistent labels across create-versus-read workflows.

## Facts and root cause

**Facts:** the UI submitted distinct values and showed the human label immediately after HTTP 201. Subsequent API readback and a full Console reload returned/rendered the slug as `display_name`. The frozen RC6 create handler passes the requested `displayName` into the 201 DTO, while its logical-model insert persists the slug and the list query reconstructs the DTO using the slug for both `slug` and `display_name`.

**Root cause:** confirmed in the frozen RC6 persistence/readback path: the submitted logical-model display name is not stored and list reconstruction substitutes the canonical slug.

## Acceptance criteria

1. Persist the logical-model display name separately from its canonical slug.
2. Create response, paginated list/readback, UI after create, UI after reload, and subsequent sessions return/display the same human label.
3. Search and filtering continue to match both canonical slug and display name.
4. Existing models with no stored display name receive an explicit compatible fallback without changing their canonical slugs or capabilities.
5. Add API and real-browser regression coverage for distinct labels, refresh persistence, pagination, and legacy rows.

