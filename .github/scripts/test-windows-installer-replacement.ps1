Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$TestDirectory = Join-Path $env:RUNNER_TEMP ("installer-replace-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TestDirectory | Out-Null

try {
    $Staged = Join-Path $TestDirectory "staged.exe"
    $Destination = Join-Path $TestDirectory "destination.exe"
    $Backup = Join-Path $TestDirectory "backup.exe"
    [IO.File]::WriteAllText($Staged, "new")
    [IO.File]::WriteAllText($Destination, "old")

    [IO.File]::Replace($Staged, $Destination, $Backup)
    if ([IO.File]::ReadAllText($Destination) -ne "new") {
        throw "File.Replace did not install the staged file"
    }
    if ([IO.File]::ReadAllText($Backup) -ne "old") {
        throw "File.Replace did not preserve the replaced file"
    }
} finally {
    Remove-Item -LiteralPath $TestDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
