$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$local = Join-Path $root ".local"
$env:PATH = "$(Join-Path $local 'go\bin');$env:PATH"
$env:GOMODCACHE = Join-Path $local "cache\go-mod"
$env:GOCACHE = Join-Path $local "cache\go-build"
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go 1.25+ is required in PATH or .local/go." }
Push-Location $root
try { go mod download } finally { Pop-Location }
