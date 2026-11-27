# CP01 — RC6 artifact, fresh install, and provenance

**Baseline:** `2eb7f6f6e5ba703d44fbd273f0245ba441de74e1` (RC6 / PR #46). **Overall:** PARTIAL. QA continues; `GATE-REL-07` remains BLOCKED.

## Artifact verification

GitHub Actions run `45581179274` reports success for the exact frozen commit, and the artifact `tanpopo-verified-artifacts` was downloaded. All five published archive checksums matched `SHA256SUMS`; archive listings had no absolute or parent traversal paths. The run metadata identifies `0.0.1-snapshot`, exact RC6 commit, UTC build date `2026-11-25T09:09:37.118552769Z`, Linux/amd64. The Linux binary reports the same version and commit, with timestamp rounded to whole seconds. It starts, serves the embedded Console, and `GET /readyz` returns 200 `ready`.

Both CycloneDX 1.6 SBOMs parse (48 Go components; 342 web components). Other OS/architecture binary files and archives were inspected but not executed. GitHub `gh attestation` is absent from this environment; the GitHub REST attestation lookup for the Linux archive SHA returned 404. Artifact attestation therefore remains UNVERIFIED; workflow success is not treated as signature verification. Full sanitized record: [artifact verification](../evidence/CP01-artifact-verification.json).

## Fresh install

Using the exact Linux RC6 artifact in an isolated SQLite runtime (`--role all`, offline, loopback-only), a real Chromium user completed Owner Account → First Connection → Key Custody → Review. The UI reported no provider adapters enabled and that initial connection was skipped. The review did not claim a route had been created. The synthetic owner explicitly acknowledged KEK custody after the synthetic test KEK was backed up outside the repository with restrictive permissions. No real provider credential was entered.

First-login, connection-step, key-custody, review, and completion screenshots are in `../evidence/CP01-rc6-setup-*.png`. A new authenticated read after refresh returned one organization, one default project and one `dev` environment. Audit readback showed matching `organization.created`, `project.created` and `environment.created` targets with the owner as actor; chain verification returned HTTP 200, `intact=true`, 5 checked events (the subsequent Virtual Key creation is in CP03). See [fresh install readback](../evidence/CP01-fresh-install-readback.json) and [Overview after refresh](../evidence/CP01-overview-after-refresh.png), visually inspected.

This resolves only the no-adapter bootstrap wording/state slice of historical DEF-049: the fresh wizard now says it creates the primary organization and owner; First Connection says it is skipped; no route was silently promised by the reviewed UI copy. Adapter-enabled bootstrap remains unverified.

## Scope / limits

This is an exact-artifact fresh-install observation, not full RC6 release acceptance. Cross-platform execution, external database/cache modes, upgrade paths, provider-backed onboarding, and attestations remain open. The existing runtime stays isolated in `/tmp`/`/workspace`; credentials, key material, database files, and logs are not included in this branch.
