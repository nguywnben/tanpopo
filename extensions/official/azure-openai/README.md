# `@tanpopo/azure-openai` Official Provider Adapter

Tier-1 declarative adapter for Azure OpenAI's deployment-scoped Chat Completions API. It uses the pinned `2024-10-21` API version and injects the `api_key` secret slot as the `api-key` HTTP header at the host egress boundary. The secret is not placed in adapter configuration, URLs, or request bodies.

> **Deployment blocker:** The checked-in package uses a cassette-only sample resource hostname and is not production-complete for arbitrary Azure resources. The current declarative runtime requires exact manifest host authorization; it cannot accept a caller-selected Azure resource host while preserving the adapter's egress boundary.

## Request contract

- Chat requests target `/openai/deployments/{model}/chat/completions?api-version=2024-10-21`; the normalized `model` is the Azure deployment name.
- Requests and responses use the OpenAI Chat Completions JSON/SSE shape, including function tool calls. Streaming injects `stream_options.include_usage` for terminal usage accounting. The connection probe uses Azure's versioned models endpoint.
- Native passthrough is limited to deployment chat completions and model listing. Native bodies remain untouched; only `api-key` and `azureml-model-deployment` are permitted as native headers.
- Authentication failures (401/403) require reauthentication; throttling and server errors are retryable.

## Resource hostname and egress

Azure OpenAI uses a distinct hostname for each Azure resource. Wildcard egress is forbidden by the extension contract, so this package pins its manifest to `tanpopo-resource.openai.azure.com` (the cassette fixture authority). Before using a real Azure resource, set `permissions.networkEgress.allowedHosts` and `adapter.baseUrl` to that resource's exact HTTPS hostname, then validate, contract-test, and package the resulting manifest under the publisher's normal review. This package as checked in cannot contact arbitrary Azure resource hosts.

## Offline contract tests

From the repository root:

```bash
TANPOPO_TEST_HERMETIC=1 CGO_ENABLED=0 go run ./cmd/tanpopo plugin test ./extensions/official/azure-openai
TANPOPO_TEST_HERMETIC=1 CGO_ENABLED=0 go test ./extensions/official/azure-openai
```

The cassette runner executes local loopback exchanges and verifies blind auth injection, sync and stream normalization, error mapping, and native passthrough without contacting Azure.
