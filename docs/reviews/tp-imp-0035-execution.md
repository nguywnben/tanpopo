# TP-IMP-0035 Execution Checkpoint

**Status:** `READY` under `DEC-070`; implementation is in progress on the persistent `develop` branch and draft PR [#14](https://github.com/nguywnben/tanpopo/pull/14).

## Ownership

- Implementer: **GPT-6 Luna**.
- Independent reviewers: pending final diff; the required `code-reviewer` and `security-auditor` reviews have not yet been run.
- Acceptor: **GPT-6 Luna**, under the Product Owner delegation in `DEC-060`.

## Current checkpoint

- The current working checkpoint includes the later rerank and OCI commits on `develop`; the exact PR head and its Actions run are recorded on GitHub.
- The first implementation slice adds a signed feed envelope verifier with Ed25519 domain separation, expiry and strictly increasing serial checks, typed marketplace/revocation entry validation, immutable OCI digest validation, bounded HTTPS retrieval through an injected guarded HTTP client, and focused tests.
- The `POST /v1/rerank` ingress path now includes strict input validation, authenticated routing with rate limits/idempotency/budget settlement, response validation, and optional document return behavior. Focused parser and response contract tests are included.
- A digest-pinned OCI blob puller now uses only an injected guarded HTTP client, bounds the archive size, verifies the downloaded SHA-256, rejects redirects and mutable tags, and scopes basic auth to the exact registry authority. Focused transport tests use an in-memory round tripper.
- The Blind Broker now has host-side SigV4 signing over the final request, using the trusted host clock and a structured `AWS_SIGV4_KEY` vault secret. It adds and signs the AWS date, content digest, and optional session token without passing plaintext keys to the adapter. A deterministic signature vector and invalid-input tests are included.
- CI at `da626b1e18f31a20cab915eb1c20387ff14b7655` found a missing `context` import in the feed fetch helper on Ubuntu and macOS. The import was fixed in `78f6d61`; Stage 0 and all three Stage 1 jobs passed on that head. The rerank and OCI changes also passed all four required PR checks on `243ab92`; the latest SigV4 changes are pending CI.
- The complete four required PR checks passed on `243ab92e9e443f725477931d9e774fbe60d2bde4` in run [`44440088676`](https://github.com/nguywnben/tanpopo/actions/runs/44440088676). The later SigV4 change is not covered by that run. Release artifact dry-run `44440088712` is separate from the four required checks.
- Go tooling in the Cloud shell is a wrapper that rejects standard Go flags; use the repository Actions workflow for Go verification.

## Remaining work

1. Complete the private OCI Distribution client with host-scoped credentials, digest verification, and guarded transport tests.
2. Integrate signed revocation feed persistence and automatic quarantine, stream fencing, and `INC-03` alerting.
3. Implement Bedrock SigV4 and Azure OpenAI official adapters without exposing credentials to extensions.
4. Connect the signed revocation feed to durable serial storage, installer lifecycle, active-stream fencing, and `INC-03` alerting; add end-to-end quarantine tests.
5. Implement the Bedrock and Azure OpenAI official adapters and their package contract tests; add rerank adapter contract coverage.
6. Run all Unit 35 gates, obtain both independent reviews on the final diff, fix findings, and re-run affected checks/reviews.
7. Merge PR #14 only after the required four CI checks and reviews pass; then record Unit 35 acceptance in a governance PR and verify post-merge CI on `main`.

No review participation beyond **GPT-6 Luna** has been recorded for this checkpoint.
