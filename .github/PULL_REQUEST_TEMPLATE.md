> **Security note:** Do **not** open a public PR or issue to report an unpatched vulnerability. Submit security reports privately via [Private Vulnerability Reporting](https://github.com/nguywnben/tanpopo/security/advisories/new) per `SECURITY.md`.

## Summary
<!-- Briefly describe what this PR changes and why. -->

## Related Issues / Units
<!-- Reference issue numbers or TP-IMP units if applicable. -->

## Verification
- [ ] `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -short ./...`
- [ ] `CGO_ENABLED=0 go run ./scripts/ci/check.go --all`
- [ ] `CGO_ENABLED=0 go run ./scripts/docs/check_drift.go` (when CLI, config, or docs changed)
- [ ] Web Console checks (`npm run typecheck`, `npm run check:i18n`, `npm test`, `npm run build`, `npm run sync:embed`, `npx astryx doctor --built dist`) when `web/console/` changed
- [ ] `SECURITY.md` followed (no secrets, tokens, or unredacted internal hosts committed)
