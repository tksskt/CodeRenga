$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$bin = Join-Path $root ".local\bin"
$env:PATH = "$(Join-Path $root '.local\go\bin');$env:PATH"
$env:GOMODCACHE = Join-Path $root ".local\cache\go-mod"
$env:GOCACHE = Join-Path $root ".local\cache\go-build"

New-Item -ItemType Directory -Force -Path $bin | Out-Null

Push-Location $root
try {
    powershell -NoProfile -ExecutionPolicy Bypass -File scripts\generate-windows-resources.ps1
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    go build -o (Join-Path $bin "coderenga.exe") ./cmd/coderenga
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}
