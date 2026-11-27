# CP65 — DEF-004 CLI configuration inspection dispatch

**Source:** frozen unsigned RC7 commit `737079ddb847d97dbcf832f610ed0048556cb65b`; exact compiled Linux/amd64 runtime artifact recorded in CP64 and campaign identity records.
**Disposition:** **PARTIAL**. The historical duplicate-command dispatch failure did not reproduce: the `config show` subcommand is present, its help resolves, and invoking it reaches the remote Admin API authentication boundary. A valid CLI Admin bearer token was unavailable, so successful authorized configuration output and output-format correctness remain UNVERIFIED.

## Reproduction

1. Ran `/tmp/qa005-bin/tanpopo --help`. Root help showed one `config` command group (the local config group).
2. Ran `/tmp/qa005-bin/tanpopo config --help` and `... config show --help`. The first shows the `show` subcommand; the second resolves to “Show the instance overview” and documents global `--output table|json|yaml`.
3. Ran `tanpopo config show --output json` without a token. It exited 14 with the localized precondition `TANPOPO_ADMIN_TOKEN is required for Admin API access`; stdout was empty. This is an auth prerequisite, not the prior local-reconcile help output.
4. Repeated against the isolated CP64 exact-RC7 service with a deliberately invalid synthetic sentinel bearer and loopback `TANPOPO_URL`. It exited 14 with `Admin API returned HTTP 401`; stdout remained empty. The request reached the Admin API. No real credential was used, and the sentinel was not persisted.

## Classification and limits

The historical DEF-004 failure was specifically duplicate `config` group shadowing: `config show` exited successfully while showing the local reconciliation group and no remote data. That behavior is absent in the current exact-RC7 command tree. The authenticated Admin CLI result still requires an authorized Admin bearer; the Owner's browser session is not that CLI credential and no usable CLI token was available in the isolated QA setup. Record **PARTIAL**, preserving the historical FAIL and previous RC6 scoped pass. Do not claim the entire configuration-inspection contract passes.

Evidence is in [CP65 evidence](../evidence/CP65-def004-cli-config-show.json). No product or specification files changed; no provider or paid service was used. `GATE-REL-07` remains BLOCKED.
