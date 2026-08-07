Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$Repository = "haowang02/agent-session-cleaner"
$Binary = "agent-session-cleaner"
$Alias = "asc"

function Fail([string]$Message) {
    throw "agent-session-cleaner installer: $Message"
}

if (-not [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform(
        [System.Runtime.InteropServices.OSPlatform]::Windows)) {
    Fail "this installer only supports Windows"
}

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
    "X64" { $Architecture = "x86_64" }
    "Arm64" { $Architecture = "arm64" }
    default { Fail "unsupported architecture: $($_)" }
}

$Version = if ($env:ASC_VERSION) { $env:ASC_VERSION } else { "latest" }
if ($Version -eq "latest") {
    $ReleasePath = "latest/download"
} else {
    if ($Version -notmatch '^[A-Za-z0-9._-]+$') {
        Fail "invalid ASC_VERSION: $Version"
    }
    $Tag = if ($Version.StartsWith("v")) { $Version } else { "v$Version" }
    $ReleasePath = "download/$Tag"
}

if ($env:ASC_INSTALL_DIR) {
    $InstallDirectory = $env:ASC_INSTALL_DIR
} else {
    $LocalAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    if (-not $LocalAppData) {
        Fail "the local application data directory is unavailable; set ASC_INSTALL_DIR explicitly"
    }
    $InstallDirectory = Join-Path $LocalAppData "Programs\agent-session-cleaner\bin"
}
if (-not [IO.Path]::IsPathRooted($InstallDirectory)) {
    Fail "the install directory must be an absolute path: $InstallDirectory"
}
$InstallDirectory = [IO.Path]::GetFullPath($InstallDirectory)

$Asset = "$Binary-windows-$Architecture.zip"
$BaseUrl = "https://github.com/$Repository/releases/$ReleasePath"
$TemporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("agent-session-cleaner." + [guid]::NewGuid().ToString("N"))
$Archive = Join-Path $TemporaryDirectory $Asset
$Checksums = Join-Path $TemporaryDirectory "checksums.txt"
$ExtractDirectory = Join-Path $TemporaryDirectory "extract"
$Staged = $null

function Download([string]$Uri, [string]$Destination) {
    $Parameters = @{
        Uri = $Uri
        OutFile = $Destination
    }
    if ($PSVersionTable.PSVersion.Major -lt 6) {
        $Parameters["UseBasicParsing"] = $true
    }
    for ($Attempt = 1; $Attempt -le 3; $Attempt++) {
        try {
            Invoke-WebRequest @Parameters
            return
        } catch {
            if ($Attempt -eq 3) {
                throw
            }
            Start-Sleep -Seconds $Attempt
        }
    }
}

function Normalize-PathValue([string]$Value) {
    if (-not $Value) {
        return $null
    }
    $Expanded = [Environment]::ExpandEnvironmentVariables($Value.Trim().Trim([char]34))
    try {
        return [IO.Path]::GetFullPath($Expanded).TrimEnd('\')
    } catch {
        return $Expanded.TrimEnd('\')
    }
}

function Test-PathListContains([string]$PathList, [string]$Directory) {
    $NormalizedDirectory = Normalize-PathValue $Directory
    foreach ($Entry in @($PathList -split ';')) {
        if ((Normalize-PathValue $Entry) -ieq $NormalizedDirectory) {
            return $true
        }
    }
    return $false
}

try {
    New-Item -ItemType Directory -Path $TemporaryDirectory | Out-Null

    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    Write-Host "Downloading $Asset..."
    Download "$BaseUrl/$Asset" $Archive
    Download "$BaseUrl/checksums.txt" $Checksums

    $Expected = $null
    $EscapedAsset = [regex]::Escape($Asset)
    foreach ($Line in (Get-Content -LiteralPath $Checksums)) {
        if ($Line -match "^([0-9A-Fa-f]{64})\s+\*?$EscapedAsset$") {
            $Expected = $Matches[1]
            break
        }
    }
    if (-not $Expected) {
        Fail "checksums.txt has no valid entry for $Asset"
    }
    $Actual = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash
    if ($Actual -ne $Expected) {
        Fail "checksum verification failed for $Asset"
    }

    Expand-Archive -LiteralPath $Archive -DestinationPath $ExtractDirectory
    $Files = @(Get-ChildItem -LiteralPath $ExtractDirectory -File -Recurse)
    $ExecutableName = "$Binary.exe"
    $ExpectedExecutable = Join-Path $ExtractDirectory $ExecutableName
    if ($Files.Count -ne 1 -or $Files[0].FullName -ine $ExpectedExecutable) {
        Fail "$Asset does not contain exactly one $ExecutableName executable"
    }

    New-Item -ItemType Directory -Path $InstallDirectory -Force | Out-Null
    $Destination = Join-Path $InstallDirectory $ExecutableName
    $Staged = Join-Path $InstallDirectory (".$Binary.install." + $PID + ".exe")
    Copy-Item -LiteralPath $Files[0].FullName -Destination $Staged -Force
    if (Test-Path -LiteralPath $Destination) {
        [IO.File]::Replace($Staged, $Destination, $null)
    } else {
        [IO.File]::Move($Staged, $Destination)
    }
    $Staged = $null

    $AliasPath = Join-Path $InstallDirectory "$Alias.cmd"
    $AliasMarker = "rem Managed by the agent-session-cleaner installer"
    $AliasContents = "@echo off`r`n$AliasMarker`r`n`"%~dp0$ExecutableName`" %*`r`n"
    $ResolvedAlias = Get-Command $Alias -ErrorAction SilentlyContinue
    $ManageAlias = -not $ResolvedAlias
    if ($ResolvedAlias -and $ResolvedAlias.CommandType -eq "Application") {
        $ManageAlias = [IO.Path]::GetFullPath($ResolvedAlias.Path) -ieq $AliasPath
    }
    $UseAlias = $false
    if ($ManageAlias) {
        $WriteAlias = -not (Test-Path -LiteralPath $AliasPath)
        if (-not $WriteAlias) {
            $ExistingAlias = Get-Content -LiteralPath $AliasPath -Raw
            $WriteAlias = $ExistingAlias.Contains($AliasMarker)
        }
        if ($WriteAlias) {
            Set-Content -LiteralPath $AliasPath -Value $AliasContents -Encoding ASCII -NoNewline
            $UseAlias = $true
        } else {
            Write-Host "Leaving existing alias unchanged: $AliasPath"
        }
    } else {
        Write-Host "Leaving existing '$Alias' command unchanged: $($ResolvedAlias.Source)"
    }

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not (Test-PathListContains $UserPath $InstallDirectory)) {
        $UpdatedPath = if ($UserPath) {
            $UserPath.TrimEnd(';') + ";" + $InstallDirectory
        } else {
            $InstallDirectory
        }
        [Environment]::SetEnvironmentVariable("Path", $UpdatedPath, "User")
        Write-Host "Added $InstallDirectory to your user PATH."
    }
    if (-not (Test-PathListContains $env:Path $InstallDirectory)) {
        $env:Path = "$InstallDirectory;$env:Path"
    }

    Write-Host "Installed $Binary to $Destination"
    if ($UseAlias) {
        Write-Host "Run '$Alias' in a new terminal to get started."
    } else {
        Write-Host "Run '$Binary' in a new terminal to get started."
    }
} finally {
    if ($Staged -and (Test-Path -LiteralPath $Staged)) {
        Remove-Item -LiteralPath $Staged -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $TemporaryDirectory) {
        Remove-Item -LiteralPath $TemporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
    }
}
