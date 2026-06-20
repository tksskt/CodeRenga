$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$env:PATH = "$(Join-Path $root '.local\go\bin');$env:PATH"
Push-Location $root
try { gofmt -w cmd internal } finally { Pop-Location }
