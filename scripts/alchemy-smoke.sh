#!/usr/bin/env sh
# Smoke test for the Alchemy Go wrapper generator (CI-friendly).
# Runs the same check as `internal/alchemy` TestGenerateGoWrapper_Compiles.
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
go test ./internal/alchemy/... -run TestGenerateGoWrapper_Compiles -count=1 "$@"
