# GoClaw + vLLM (embedding)

## Server

Start vLLM with the OpenAI API server entrypoint so GoClaw can call `POST /v1/embeddings`. See [examples/gemma-2b-embeddings.sh](examples/gemma-2b-embeddings.sh) for a concrete command line.

## GoClaw provider

1. Add an LLM provider with **`provider_type`: `vllm`** (or use the UI equivalent).
2. Set **API base** to the vLLM OpenAI root, including `/v1`, for example:
   - `http://127.0.0.1:8002/v1` if vLLM listens on port `8002`.
3. **API key**: vLLM often runs without auth; GoClaw accepts a placeholder such as `-` for Bearer when the server ignores it.
4. Under provider **embedding** settings:
   - **Enable** embedding.
   - **Model** = the value passed to vLLM as `--served-model-name` (e.g. `gemma-2b-embeddings`).

## `dimensions` and Matryoshka

vLLM only accepts the OpenAI-style **`dimensions`** body field for models that support variable output size (Matryoshka). Fixed-size embedding models (typical Gemma embedding checkpoints) **must not** receive `dimensions` on `POST /v1/embeddings`.

GoClaw’s server omits upstream `dimensions` for **`vllm` providers** so fixed-dim embedding models work without extra UI flags. Do not rely on `settings.embedding.dimensions` to “shrink” vectors for those models against vLLM; if you need a smaller vector for application logic, truncate or project **after** you receive the full embedding.

## Memory / pgvector note

GoClaw memory uses **vector(1536)** in PostgreSQL. The embedding pipeline **truncates or zero-pads** model output to that length (e.g. **2048 → first 1536 dimensions**) so fixed-schema storage works without a migration. This is a pragmatic default; for full-fidelity 2048-dim search you would need a schema change to `vector(2048)` and re-embed.
