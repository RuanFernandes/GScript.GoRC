[CmdletBinding()]
param(
    [ValidateSet('amd64', '386')]
    [string]$Architecture,
    [string]$OutputDirectory = '',
    [string]$Version = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if ($Version -ne '' -and $Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version '$Version' must contain exactly three numeric components."
}

if ($OutputDirectory -eq '') {
    $OutputDirectory = Join-Path $repositoryRoot (Join-Path 'ci-artifacts\windows' $Architecture)
}
elseif (-not [System.IO.Path]::IsPathRooted($OutputDirectory)) {
    $OutputDirectory = Join-Path $repositoryRoot $OutputDirectory
}

New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

function Get-PeMachine {
    param([Parameter(Mandatory = $true)][string]$Path)

    $bytes = [System.IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 0x40 -or $bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a) {
        throw "$Path is not a valid PE file"
    }
    $peOffset = [System.BitConverter]::ToInt32($bytes, 0x3c)
    if ($peOffset -lt 0 -or $peOffset + 6 -gt $bytes.Length) {
        throw "$Path contains an invalid PE header offset"
    }
    if ($bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45 -or $bytes[$peOffset + 2] -ne 0 -or $bytes[$peOffset + 3] -ne 0) {
        throw "$Path is missing the PE signature"
    }
    return [System.BitConverter]::ToUInt16($bytes, $peOffset + 4)
}

$wails = Get-Command wails3 -CommandType Application -ErrorAction Stop
$binaryName = "graal-rc-windows-$Architecture.exe"
$binaryPath = Join-Path $OutputDirectory $binaryName
$expectedMachine = if ($Architecture -eq 'amd64') { [uint16]0x8664 } else { [uint16]0x014c }
$sysoPath = Join-Path $repositoryRoot "wails_windows_$Architecture.ci.syso"
$temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) "graal-rc-build-$PID-$Architecture"
$sysoBackup = Join-Path $temporaryRoot 'original.syso'
$hadSyso = Test-Path -LiteralPath $sysoPath -PathType Leaf

if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot 'frontend\dist\index.html') -PathType Leaf)) {
    throw 'frontend/dist/index.html is missing; the frontend build artifact must be downloaded before the Go build'
}

New-Item -ItemType Directory -Path $temporaryRoot -Force | Out-Null
if ($hadSyso) {
    Copy-Item -LiteralPath $sysoPath -Destination $sysoBackup -Force
}

$sysoInfoPath = Join-Path $repositoryRoot 'build\windows\info.json'
if ($Version -ne '') {
    $testInfoPath = Join-Path $temporaryRoot 'info.json'
    $testInfo = Get-Content -LiteralPath $sysoInfoPath -Raw | ConvertFrom-Json
    $testInfo.fixed.file_version = $Version
    $testInfo.info.'0000'.ProductVersion = $Version
    $testInfo | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $testInfoPath -Encoding utf8
    $sysoInfoPath = $testInfoPath
}

$buildModFile = ''
if ($Architecture -eq '386') {
    # Wails alpha2.117 cannot compile for 386: its updater package passes a
    # 2 GiB untyped constant to fmt as int. alpha2.118 contains the upstream
    # int64 fix. Keep the repository's module files untouched and use the
    # corrected dependency only through this disposable modfile.
    $buildModFile = Join-Path $temporaryRoot 'go.mod'
    Copy-Item -LiteralPath (Join-Path $repositoryRoot 'go.mod') -Destination $buildModFile -Force
    Copy-Item -LiteralPath (Join-Path $repositoryRoot 'go.sum') -Destination (Join-Path $temporaryRoot 'go.sum') -Force
    & go mod edit "-modfile=$buildModFile" '-replace=github.com/wailsapp/wails/v3=github.com/wailsapp/wails/v3@v3.0.0-alpha2.118'
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not create the disposable x86 Wails compatibility modfile'
    }
    & go mod download "-modfile=$buildModFile" 'github.com/wailsapp/wails/v3@v3.0.0-alpha2.118'
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not download the disposable x86 Wails compatibility dependency'
    }
}

$oldGoos = $env:GOOS
$oldGoarch = $env:GOARCH
$oldCgo = $env:CGO_ENABLED
try {
    $env:GOOS = 'windows'
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = '0'

    & $wails.Source generate syso -arch $Architecture -icon 'build/windows/icon.ico' -manifest 'build/windows/wails.exe.manifest' -info $sysoInfoPath -out $sysoPath
    if ($LASTEXITCODE -ne 0) {
        throw "wails3 generate syso failed with exit code $LASTEXITCODE"
    }

    $ldflags = '-w -s -H windowsgui'
    if ($Version -ne '') {
        $ldflags += " -X main.RCVersion=$Version"
    }

    $buildArguments = @(
        'build',
        $(if ($buildModFile -ne '') { @('-modfile', $buildModFile) } else { @() }),
        '-tags', 'production',
        '-trimpath',
        '-buildvcs=false',
        '-ldflags', $ldflags,
        '-o', $binaryPath,
        '.'
    )
    & go @buildArguments
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
}
finally {
    $env:GOOS = $oldGoos
    $env:GOARCH = $oldGoarch
    $env:CGO_ENABLED = $oldCgo

    if ($hadSyso) {
        Copy-Item -LiteralPath $sysoBackup -Destination $sysoPath -Force
    }
    else {
        Remove-Item -LiteralPath $sysoPath -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $temporaryRoot -Recurse -Force -ErrorAction SilentlyContinue
}

if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
    throw "Go build did not produce $binaryPath"
}
$binaryMachine = Get-PeMachine -Path $binaryPath
if ($binaryMachine -ne $expectedMachine) {
    throw "$binaryName has PE machine 0x$('{0:X4}' -f $binaryMachine), expected 0x$('{0:X4}' -f $expectedMachine)"
}

$nativeName = if ($Architecture -eq 'amd64') { 'grclib64.dll' } else { 'grclib.dll' }
$nativePath = Join-Path $repositoryRoot (Join-Path "rclib\native\windows-$Architecture" $nativeName)
$artifactFiles = @([System.IO.FileInfo](Get-Item -LiteralPath $binaryPath))
$nativeMachine = Get-PeMachine -Path $nativePath
if ($nativeMachine -ne $expectedMachine) {
    throw "$nativeName has PE machine 0x$('{0:X4}' -f $nativeMachine), expected 0x$('{0:X4}' -f $expectedMachine)"
}
$artifactNativePath = Join-Path $OutputDirectory $nativeName
Copy-Item -LiteralPath $nativePath -Destination $artifactNativePath -Force
$artifactFiles += [System.IO.FileInfo](Get-Item -LiteralPath $artifactNativePath)

$hashLines = foreach ($file in ($artifactFiles | Sort-Object Name)) {
    $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $($file.Name)"
}
[System.IO.File]::WriteAllLines((Join-Path $OutputDirectory 'SHA256SUMS.txt'), $hashLines, [System.Text.Encoding]::ASCII)

Write-Host "Built $binaryName for windows/$Architecture"
Write-Host "SHA-256 manifest: $(Join-Path $OutputDirectory 'SHA256SUMS.txt')"
