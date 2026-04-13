# vLLM self-hosted (GoClaw)

This folder holds **optional, self-contained** material for running [vLLM](https://github.com/vllm-project/vllm) OpenAI-compatible servers alongside GoClaw. Nothing here is imported by the Go binary; it exists so deployments and upstream merges stay clean.

## Contents

| Path | Purpose |
|------|---------|
| [examples/gemma-2b-embeddings.sh](examples/gemma-2b-embeddings.sh) | Example launch script (embedding model). Tune env vars, not the core repo. |
| [goclaw-integration.md](goclaw-integration.md) | How to point GoClaw at the server (API base, model name, embedding settings). |

## Adding more examples

Place new scripts under `docs/vllm/examples/` with a descriptive name. Prefer environment variables for host, port, and model paths so the same file works across machines without editing tracked defaults.
