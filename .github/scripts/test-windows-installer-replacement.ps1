Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$TestDirectory = Join-Path $env:RUNNER_TEMP ("installer-replace-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TestDirectory | Out-Null
$RunningProcess = $null

try {
    $Installer = Join-Path $PSScriptRoot "..\..\install.ps1"
    $Tokens = $null
    $Errors = $null
    $Ast = [System.Management.Automation.Language.Parser]::ParseFile(
        $Installer, [ref]$Tokens, [ref]$Errors
    )
    if ($Errors.Count) {
        throw "install.ps1 did not parse"
    }
    foreach ($FunctionName in @("Remove-InstallerFile", "Remove-StaleInstallerBackups")) {
        $Function = $Ast.Find({
            param($Node)
            $Node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
                $Node.Name -eq $FunctionName
        }, $true)
        if (-not $Function) {
            throw "install.ps1 has no $FunctionName function"
        }
        Invoke-Expression $Function.Extent.Text
    }
    $Binary = "agent-session-cleaner"

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

    # Reproduce the real update case: Windows allows File.Replace to move a
    # running executable to the backup path, but keeps that old image locked
    # until its process exits.
    $RunningDestination = Join-Path $TestDirectory "running.exe"
    $RunningStaged = Join-Path $TestDirectory "running-staged.exe"
    $LockedBackup = Join-Path $TestDirectory ".$Binary.backup.locked.exe"
    Copy-Item -LiteralPath "$env:SystemRoot\System32\ping.exe" -Destination $RunningDestination
    Copy-Item -LiteralPath "$env:SystemRoot\System32\where.exe" -Destination $RunningStaged
    $OldHash = (Get-FileHash -LiteralPath $RunningDestination -Algorithm SHA256).Hash
    $NewHash = (Get-FileHash -LiteralPath $RunningStaged -Algorithm SHA256).Hash
    $ProcessParameters = @{
        FilePath = $RunningDestination
        ArgumentList = @("127.0.0.1", "-n", "30", "-w", "1000")
        WindowStyle = "Hidden"
        PassThru = $true
    }
    $RunningProcess = Start-Process @ProcessParameters
    Start-Sleep -Milliseconds 500
    if ($RunningProcess.HasExited) {
        throw "the executable used to test a running update exited too early"
    }

    [IO.File]::Replace($RunningStaged, $RunningDestination, $LockedBackup)
    if ((Get-FileHash -LiteralPath $RunningDestination -Algorithm SHA256).Hash -ne $NewHash) {
        throw "File.Replace did not install the staged executable"
    }
    if ((Get-FileHash -LiteralPath $LockedBackup -Algorithm SHA256).Hash -ne $OldHash) {
        throw "File.Replace did not preserve the running executable"
    }
    if (Remove-InstallerFile $LockedBackup 1) {
        throw "running executable backup was unexpectedly reported as removed"
    }
    if (-not (Test-Path -LiteralPath $LockedBackup)) {
        throw "running executable backup disappeared before its process exited"
    }
    $Deferred = @(Remove-StaleInstallerBackups $TestDirectory)
    if ($Deferred.Count -ne 1 -or $Deferred[0] -ine $LockedBackup) {
        throw "stale cleanup did not report the running executable backup"
    }

    Stop-Process -Id $RunningProcess.Id -Force
    $RunningProcess.WaitForExit()
    $RunningProcess = $null
    if (@(Remove-StaleInstallerBackups $TestDirectory).Count -ne 0) {
        throw "unlocked stale backup was still reported as deferred"
    }
    if (Test-Path -LiteralPath $LockedBackup) {
        throw "unlocked stale backup was not removed"
    }
} finally {
    if ($RunningProcess -and -not $RunningProcess.HasExited) {
        Stop-Process -Id $RunningProcess.Id -Force -ErrorAction SilentlyContinue
        $RunningProcess.WaitForExit()
    }
    Remove-Item -LiteralPath $TestDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
