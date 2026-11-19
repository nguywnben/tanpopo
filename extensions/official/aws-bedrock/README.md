# `@tanpopo/aws-bedrock` Official Adapter

Tier-2 sandboxed WebAssembly adapter for the Amazon Bedrock Runtime Converse API.

## Contract

- Translates normalized OpenAI chat messages, system instructions, tool calls/results, and inference settings to Bedrock `Converse`; translates text, tool use, stop reason, model, and token usage back to the normalized chat completion response.
- Parses AWS EventStream frames from `ConverseStream` (including split frames, CRC checks, text deltas, tool-use deltas, stop reason, and trailing usage metadata) into normalized OpenAI SSE chunks.
- Uses regional `bedrock-runtime.{region}.amazonaws.com` endpoints. `config.aws_region` selects the signing/endpoint region (default `us-east-1`); `config.endpoint_base_url` may pin an approved regional endpoint.
- Credentials are represented only by the symbolic `aws_credentials` slot. The adapter emits a `HOST_CRYPTO_SIGN` instruction for `AWS_SIGV4`; the Host Blind Credential Broker loads the structured Vault credential and signs the final HTTPS request. No access key, secret key, or session token is supplied to Wasm.
- SigV4 canonicalizes the final method, escaped URI, sorted query, normalized signed headers, payload SHA-256, and trusted Host clock inside the broker. The adapter never builds an Authorization value or receives a signature.
- Image/document payloads, guardrail configuration, and Bedrock-specific inference parameters outside the normalized chat contract are not implemented by this package.

## Build

```bash
clang --target=wasm32 -mcpu=mvp -nostdlib -ffreestanding -O2 -Wall -Wextra -Werror \
  -Wl,--no-entry -Wl,--strip-all -Wl,--initial-memory=262144 \
  -Wl,--max-memory=16777216 -Wl,-z,stack-size=65536 \
  extensions/official/aws-bedrock/src/adapter.c \
  -o extensions/official/aws-bedrock/dist/adapter.wasm
```

## Offline contract verification

Run `TANPOPO_TEST_HERMETIC=1 CGO_ENABLED=0 go test ./extensions/official/aws-bedrock` and `TANPOPO_TEST_HERMETIC=1 CGO_ENABLED=0 go run ./cmd/tanpopo plugin test extensions/official/aws-bedrock`. The package-local tests replay the cassettes through the Wasm adapter and validate the resulting `HOST_CRYPTO_SIGN` instruction and the broker's canonical `AWS4-HMAC-SHA256` Authorization form against a deterministic fixture; no AWS endpoint is contacted.
