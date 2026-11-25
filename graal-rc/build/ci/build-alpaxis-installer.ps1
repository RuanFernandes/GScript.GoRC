<#
.SYNOPSIS
Builds the Windows x64 installer of Alpaxis Remote Control.

.DESCRIPTION
The app installs and runs beside the official Graal RC: it has its own install
folder, shortcuts, uninstall entry, executable (alpaxis-rc.exe), data folder
(%APPDATA%\alpaxis-rc) and MCP port (8767).
#>
[CmdletBinding()]
param(
    [ValidatePattern('^(\d+\.\d+\.\d+)?$')]
    [string]$Version = '',

    [ValidateSet('user', 'machine')]
    [string]$InstallScope = 'machine',

    # Reuses frontend/bindings and frontend/dist from an earlier build.
    [switch]$SkipFrontend
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if ($Version -eq '') {
    $Version = (Get-Content -LiteralPath (Join-Path $repositoryRoot 'VERSION') -Raw).Trim()
}

function Assert-LastExitCode {
    param([Parameter(Mandatory = $true)][string]$Step)

    if ($LASTEXITCODE -ne 0) {
        throw "$Step failed with exit code $LASTEXITCODE."
    }
}

Push-Location -LiteralPath $repositoryRoot
try {
    if (-not $SkipFrontend) {
        & (Join-Path $PSScriptRoot 'generate-bindings.ps1')
        & npm --prefix frontend run build
        Assert-LastExitCode 'The frontend build'
    }

    $global:LASTEXITCODE = 0
    & (Join-Path $PSScriptRoot 'build-windows.ps1') -Architecture amd64 -Version $Version
    Assert-LastExitCode 'The binary build'

    & wails3 generate webview2bootstrapper -dir (Join-Path $repositoryRoot 'build\windows\nsis')
    Assert-LastExitCode 'The WebView2 bootstrapper download'

    # NSIS does not create the output folder.
    New-Item -ItemType Directory -Path (Join-Path $repositoryRoot 'bin') -Force | Out-Null

    & (Join-Path $repositoryRoot 'build\windows\compile-nsis.ps1') `
        -InstallScope $InstallScope `
        -Architecture amd64 `
        -AppName alpaxis-rc `
        -ExecutablePath (Join-Path $repositoryRoot 'ci-artifacts\windows\amd64\graal-rc-windows-amd64.exe') `
        -NativeLibraryPath (Join-Path $repositoryRoot 'rclib\native\windows-amd64\grclib64.dll') `
        -NativeLibraryName grclib64.dll `
        -ProductVersion $Version
    Assert-LastExitCode 'The installer build'

    Write-Host "Installer: $(Join-Path $repositoryRoot 'bin\alpaxis-rc-amd64-installer.exe')"
}
finally {
    Pop-Location
}
