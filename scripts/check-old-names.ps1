param(
    [Parameter(Mandatory = $true)]
    [string]$Root
)

$ErrorActionPreference = "Stop"

$terms = @(
    [string]::Concat("Light", "Code")
    [string]::Concat("Lazy", "Codex")
    [string]::Concat("Code", "Relay")
    [string]::Concat("Code", "Bridge")
    [string]::Concat("lightweight", "-coding-agent")
)

$rootPath = [IO.Path]::GetFullPath($Root)
if (-not [IO.Directory]::Exists($rootPath)) {
    Write-Error "Root directory does not exist: $rootPath"
    exit 2
}

$latin1 = [Text.Encoding]::GetEncoding(28591)
$utf16LE = [Text.Encoding]::Unicode
$utf16BE = [Text.Encoding]::BigEndianUnicode
$scanned = 0
$matchedFiles = 0
$unreadable = 0

Write-Host "[INFO] Full raw-file scan: $rootPath"
Write-Host "[INFO] No directory, file, or extension exclusions are applied."

try {
    $files = [IO.Directory]::EnumerateFiles(
        $rootPath,
        "*",
        [IO.SearchOption]::AllDirectories
    )

    foreach ($file in $files) {
        $scanned++
        try {
            $bytes = [IO.File]::ReadAllBytes($file)
            $views = @(
                @{ Name = "byte/UTF-8-compatible"; Text = $latin1.GetString($bytes) }
                @{ Name = "UTF-16LE"; Text = $utf16LE.GetString($bytes) }
                @{ Name = "UTF-16BE"; Text = $utf16BE.GetString($bytes) }
            )

            $fileMatched = $false
            foreach ($term in $terms) {
                foreach ($view in $views) {
                    if ($view.Text.IndexOf($term, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
                        Write-Host "[MATCH] $file [$($view.Name)] : $term"
                        $fileMatched = $true
                        break
                    }
                }
            }

            if ($fileMatched) {
                $matchedFiles++
            }
        }
        catch {
            $unreadable++
            Write-Host "[ERROR] Cannot read $file : $($_.Exception.Message)"
        }

        if (($scanned % 5000) -eq 0) {
            Write-Host "[INFO] Scanned $scanned files..."
        }
    }
}
catch {
    Write-Host "[ERROR] File enumeration failed: $($_.Exception.Message)"
    exit 2
}

Write-Host "[SUMMARY] scanned=$scanned matched_files=$matchedFiles unreadable=$unreadable"

if ($unreadable -gt 0) {
    exit 2
}
if ($matchedFiles -gt 0) {
    exit 1
}

Write-Host "[OK] No old project name references found."
exit 0
