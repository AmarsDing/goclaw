#!/usr/bin/env bash
# Example: vLLM OpenAI server for a local Gemma embedding checkpoint.
# Copy or symlink this file outside the repo if you need machine-specific paths in gitignored space.
#
# Usage:
#   export VLLM_MODEL_PATH=/path/to/gemma-2b-embeddings
#   ./gemma-2b-embeddings.sh
#
# Requires: Python environment with vLLM installed (`pip install vllm`).

set -euo pipefail

: "${VLLM_MODEL_PATH:?set VLLM_MODEL_PATH to the model directory (e.g. /home/user/models/gemma-2b-embeddings)}"
: "${VLLM_HOST:=0.0.0.0}"
: "${VLLM_PORT:=8002}"
: "${VLLM_SERVED_MODEL_NAME:=gemma-2b-embeddings}"

exec python -m vllm.entrypoints.openai.api_server \
  --model "${VLLM_MODEL_PATH}" \
  --served-model-name "${VLLM_SERVED_MODEL_NAME}" \
  --trust-remote-code \
  --host "${VLLM_HOST}" \
  --port "${VLLM_PORT}" \
  --load-format auto \
  --gpu-memory-utilization 0.1 \
  --max-model-len 8192 \
  --enable-prefix-caching \
  --enable-chunked-prefill \
  --max-num-seqs 40 \
  --max-num-batched-tokens 32768
