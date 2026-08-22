[CmdletBinding()]
param(
    [ValidateSet('amd64')]
    [string]$Architecture = 'amd64',

    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = '3.1.3',

    [string]$OutputPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$buildScript = Join-Path $repositoryRoot 'build\ci\build-windows.ps1'
$compileScript = Join-Path $repositoryRoot 'build\windows\compile-nsis.ps1'
$binaryPath = Join-Path $repositoryRoot "ci-artifacts\windows\$Architecture\graal-rc-windows-$Architecture.exe"
$nativeLibraryPath = Join-Path $repositoryRoot 'rclib\native\windows-amd64\grclib64.dll'
$canonicalInstallerPath = Join-Path $repositoryRoot "bin\graal-rc-$Architecture-installer.exe"

if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $repositoryRoot "bin\graal-rc-$Architecture-installer-test-$Version.exe"
}
elseif (-not [System.IO.Path]::IsPathRooted($OutputPath)) {
    $OutputPath = Join-Path $repositoryRoot $OutputPath
}

$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)
$canonicalInstallerPath = [System.IO.Path]::GetFullPath($canonicalInstallerPath)
New-Item -ItemType Directory -Path (Split-Path -Parent $OutputPath) -Force | Out-Null

$makensisCommand = Get-Command makensis -CommandType Application -ErrorAction SilentlyContinue
if ($null -ne $makensisCommand) {
    $makensisPath = $makensisCommand.Source
}
else {
    $makensisCandidates = @(
        (Join-Path ${env:ProgramFiles(x86)} 'NSIS\makensis.exe'),
        (Join-Path $env:ProgramFiles 'NSIS\makensis.exe')
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_ -PathType Leaf) }
    $makensisCandidates = @($makensisCandidates)

    if ($makensisCandidates.Count -eq 0) {
        throw 'makensis was not found. Install NSIS or add makensis.exe to PATH.'
    }
    $makensisPath = (Resolve-Path -LiteralPath $makensisCandidates[0]).Path
}

$canonicalBackupPath = Join-Path ([System.IO.Path]::GetTempPath()) "graal-rc-canonical-installer-$PID.exe"
$hadCanonicalInstaller = Test-Path -LiteralPath $canonicalInstallerPath -PathType Leaf
if ($hadCanonicalInstaller) {
    Copy-Item -LiteralPath $canonicalInstallerPath -Destination $canonicalBackupPath -Force
}

try {
    & $buildScript -Architecture $Architecture -Version $Version
    if ($LASTEXITCODE -ne 0) {
        throw "Windows test binary build failed with exit code $LASTEXITCODE."
    }

    & $compileScript `
        -InstallScope machine `
        -Architecture $Architecture `
        -AppName graal-rc `
        -ExecutablePath $binaryPath `
        -NativeLibraryPath $nativeLibraryPath `
        -NativeLibraryName grclib64.dll `
        -ProductVersion $Version `
        -MakensisPath $makensisPath
    if ($LASTEXITCODE -ne 0) {
        throw "Windows test installer build failed with exit code $LASTEXITCODE."
    }

    if (-not (Test-Path -LiteralPath $canonicalInstallerPath -PathType Leaf)) {
        throw "NSIS did not produce the expected installer '$canonicalInstallerPath'."
    }

    Copy-Item -LiteralPath $canonicalInstallerPath -Destination $OutputPath -Force
    $installerVersion = [string](Get-Item -LiteralPath $OutputPath).VersionInfo.ProductVersion
    if ($installerVersion -ne $Version) {
        throw "The generated installer reports version '$installerVersion', expected '$Version'."
    }

    $hash = (Get-FileHash -LiteralPath $OutputPath -Algorithm SHA256).Hash.ToLowerInvariant()
    $size = (Get-Item -LiteralPath $OutputPath).Length
    Write-Host "Test installer: $OutputPath"
    Write-Host "Version: $installerVersion"
    Write-Host "Size: $size bytes"
    Write-Host "SHA256: $hash"
}
finally {
    if ($hadCanonicalInstaller) {
        Copy-Item -LiteralPath $canonicalBackupPath -Destination $canonicalInstallerPath -Force
    }
    else {
        Remove-Item -LiteralPath $canonicalInstallerPath -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $canonicalBackupPath -Force -ErrorAction SilentlyContinue
}
