$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$local = Join-Path $root ".local"
$goBin = Join-Path $local "go\bin"
$toolBin = Join-Path $local "bin"
$rsrc = Join-Path $toolBin "rsrc.exe"
$icon = Join-Path $root "assets\CodeRenga.ico"
$output = Join-Path $root "cmd\coderenga\rsrc_windows_amd64.syso"

$env:PATH = "$goBin;$env:PATH"
$env:GOBIN = $toolBin
$env:GOMODCACHE = Join-Path $local "cache\go-mod"
$env:GOCACHE = Join-Path $local "cache\go-build"

if (-not (Test-Path $icon)) {
    throw "Icon file not found: $icon"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go 1.25+ is required in PATH or .local/go."
}

New-Item -ItemType Directory -Force -Path $toolBin | Out-Null
if (-not (Test-Path $rsrc)) {
    go install github.com/akavel/rsrc@v0.10.2
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

& $rsrc -arch amd64 -ico $icon -o $output
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Output "Generated $output"
