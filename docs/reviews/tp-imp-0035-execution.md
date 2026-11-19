# TP-IMP-0035 Execution Checkpoint

**Status:** `READY` under `DEC-070`; implementation is in progress on the persistent `develop` branch and draft PR [#14](https://github.com/nguywnben/tanpopo/pull/14).

## Ownership

- Implementer: **GPT-6 Luna**.
- Independent reviewers: pending final diff; the required `code-reviewer` and `security-auditor` reviews have not yet been run.
- Acceptor: **GPT-6 Luna**, under the Product Owner delegation in `DEC-060`.

## Current checkpoint

- PR head at this checkpoint: `78f6d61ca8d3721db34c8a4d7ef09930fd95c69f`.
- The first implementation slice adds a signed feed envelope verifier with Ed25519 domain separation, expiry and strictly increasing serial checks, typed marketplace/revocation entry validation, immutable OCI digest validation, bounded HTTPS retrieval through an injected guarded HTTP client, and focused tests.
- The `POST /v1/rerank` ingress path now includes strict input validation, authenticated routing with rate limits/idempotency/budget settlement, response validation, and optional document return behavior. Focused parser and response contract tests are included.
- CI at `da626b1e18f31a20cab915eb1c20387ff14b7655` found a missing `context` import in the feed fetch helper on Ubuntu and macOS. The import was fixed in `78f6d61`; Stage 0 and all three Stage 1 jobs are green on `78f6d61`. The rerank changes added after that run need their own CI result.
- Go tooling in the Cloud shell is a wrapper that rejects standard Go flags; use the repository Actions workflow for Go verification.

## Remaining work

1. Complete the private OCI Distribution client with host-scoped credentials, digest verification, and guarded transport tests.
2. Integrate signed revocation feed persistence and automatic quarantine, stream fencing, and `INC-03` alerting.
3. Implement Bedrock SigV4 and Azure OpenAI official adapters without exposing credentials to extensions.
4. Add rerank provider adapter contract coverage and validate the new ingress in CI.
5. Run all Unit 35 gates, obtain both independent reviews on the final diff, fix findings, and re-run affected checks/reviews.
6. Merge PR #14 only after the required four CI checks and reviews pass; then record Unit 35 acceptance in a governance PR and verify post-merge CI on `main`.

No review participation beyond **GPT-6 Luna** has been recorded for this checkpoint.
