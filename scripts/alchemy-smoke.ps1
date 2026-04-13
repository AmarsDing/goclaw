# Smoke test for the Alchemy Go wrapper generator (Windows).
# Same as scripts/alchemy-smoke.sh
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
Push-Location $Root
try {
  go test ./internal/alchemy/... -run TestGenerateGoWrapper_Compiles -count=1 @args
} finally {
  Pop-Location
}
