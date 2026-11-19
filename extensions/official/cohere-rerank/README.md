# Cohere Rerank Official Adapter

This official Tier-1 declarative package targets Cohere's v2 rerank API at the exact host `api.cohere.com`. It injects the API key at the host boundary and normalizes ranked indices, relevance scores, optional documents, and `billed_units.search_units`.

The adapter accepts the normalized Tanpopo rerank request shape (`model`, `query`, `documents`, and optional `top_n` / `return_documents`). Provider response validation rejects missing results, negative indices, nonnumeric scores, and nonfinite scores.

Run `tanpopo plugin test` from this directory to replay the offline cassette contracts.
