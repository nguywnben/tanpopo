# RC9 focused test results

All commands ran on the source snapshot from `git archive ac9ae87a68f095203de82968ac5d08729e33a531`, using Go 1.26.0 in `golang:1.26-alpine`. PostgreSQL integration suites used PostgreSQL 16 and a test principal without SUPERUSER or BYPASSRLS. No implementation files were changed.

## PostgreSQL forced-RLS suites — PASS

The following individual tests passed with `-count=1`:

- `internal/admin/http`: `TestDEF081_DEF082_PostgresQuarantineStreamBarrierRecoveryAndDenials`
- `internal/admin/http`: `TestDEF081_PostgresKillSwitchAffectedEntitiesCountAcrossOrgs`
- `internal/admin/http`: `TestDEF081_PostgresFourEyesCrossOrgSelfApprovalDenied`
- `internal/admin/http`: `TestDEF081_PostgresExtensionBlastRadiusBootstrapOperator`
- `internal/security/killswitch`: `TestDEF081_PostgresForcedRLSGlobalQuarantineAndClearOperatorResolution`
- `internal/security/killswitch`: `TestDEF081_PostgresForcedRLSNegativeAuthorizationAndIsolation`
- `internal/security/killswitch`: `TestDEF081_PostgresListWithFilterAndEmptyOrgEdgeCases`
- `internal/extension/installer`: `TestDEF081_PostgresMultiOrgBootstrapOperatorExtensionLifecycleAndDenials`

The negative suite covered non-operator denial, forged org-bound operator escalation, suspended operator, nonexistent principal, cross-tenant clear anti-oracle behavior and isolation. No explicit revoked-principal fixture was present in the executed cases; that subcase remains UNVERIFIED.

## Four-eyes independent approval — PASS (SQLite-backed test harness)

`internal/admin/http:TestPart2MultiTenantExtensionRBACAndFourEyesHardening` passed. It rejects a same-actor cross-org self-approval attempt and then accepts approval by a distinct INSTANCE OPERATOR. The cross-org self-approval rejection also passed in the PostgreSQL-specific `TestDEF081_PostgresFourEyesCrossOrgSelfApprovalDenied` test. The successful independent approval subcase was not executed against PostgreSQL in this bounded slice.

## Containment webhook and SQLite adjacency — PASS, scoped

- `internal/admin/http:TestKillSwitchQueuesOrganizationScopedWebhook` passed.
- `internal/security/killswitch:TestTriggerWebhookOutboxFailureRollsBackKillSwitch` passed.
- `internal/security/killswitch:TestAll4KillSwitchLevelsRestartHydrationAndTenantIsolation` passed.
- `internal/security/killswitch:TestKillSwitchFailClosedRBACValidationAndCrossProcessWatcher` passed.
- `internal/extension/installer:TestInstallerAC1FullLifecycleAndFastRollback` passed.
- `internal/extension/installer:TestDEF058_071_ReservedNamespaceAndUnquarantineLifecycle` passed.

These are source-compiled SQLite/in-memory harness checks, not independent real webhook delivery to a subscriber. On the actual RC9 PostgreSQL runtime, six `extension.quarantined` and six `kill_switch.triggered` records were present in the webhook outbox. No webhook subscription was configured, so delivery and receiver behavior remain UNVERIFIED.

## Tooling retries, not product defects

- The host Go shim was broken, and Go 1.24 was below the module's declared Go 1.26 minimum.
- Docker initially lacked the environment CA for module downloads; mounting the host CA bundle restored normal verified TLS.
- The PostgreSQL harness rejected the initial superuser DSN as designed; a separate NOBYPASSRLS role resolved this setup error.
