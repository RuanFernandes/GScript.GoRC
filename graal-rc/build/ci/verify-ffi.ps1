[CmdletBinding()]
param(
    [ValidateSet('amd64', '386')]
    [string]$Architecture = 'amd64',
    [string]$BinaryPath = '',
    [switch]$RunStaticcheck
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $repositoryRoot

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

try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = $Architecture

    $cgoEnabled = (& go env CGO_ENABLED).Trim()
    if ($cgoEnabled -ne '0') {
        throw "FFI validation requires CGO_ENABLED=0, got '$cgoEnabled'"
    }

    $packageJson = (& go list -json ./rclib | Out-String)
    if ($LASTEXITCODE -ne 0) {
        throw "go list could not load the rclib package"
    }
    $package = $packageJson | ConvertFrom-Json
    $cgoFiles = @()
    if ($null -ne $package.PSObject.Properties['CgoFiles']) {
        $cgoFiles = @($package.CgoFiles)
    }
    if ($cgoFiles.Count -ne 0) {
        throw "rclib unexpectedly contains cgo files: $($cgoFiles -join ', ')"
    }

    $expectedMachine = if ($Architecture -eq 'amd64') { [uint16]0x8664 } else { [uint16]0x014c }
    $nativeName = if ($Architecture -eq 'amd64') { 'grclib64.dll' } else { 'grclib.dll' }
    $nativePath = Join-Path $repositoryRoot (Join-Path "rclib\native\windows-$Architecture" $nativeName)
    $nativeMachine = Get-PeMachine -Path $nativePath
    if ($nativeMachine -ne $expectedMachine) {
        throw "$nativeName has PE machine 0x$('{0:X4}' -f $nativeMachine), expected 0x$('{0:X4}' -f $expectedMachine) for $Architecture"
    }
    Write-Host "FFI native library: $nativeName matches $Architecture (PE 0x$('{0:X4}' -f $nativeMachine))"

    $testRoot = Join-Path ([System.IO.Path]::GetTempPath()) "graal-rc-ffi-$PID-$Architecture"
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
    $testBinary = Join-Path $testRoot "rclib-$Architecture.test.exe"
    try {
        Write-Host "Compiling rclib for windows/$Architecture with CGO disabled"
        & go test -c -o $testBinary ./rclib
        if ($LASTEXITCODE -ne 0) {
            throw "go test -c ./rclib failed with exit code $LASTEXITCODE"
        }

        Write-Host 'Running vet for the FFI package with unsafeptr explicitly disabled'
        & go vet -unsafeptr=false ./rclib
        if ($LASTEXITCODE -ne 0) {
            throw "go vet ./rclib failed with exit code $LASTEXITCODE"
        }

        if ($BinaryPath -ne '') {
            $binary = if ([System.IO.Path]::IsPathRooted($BinaryPath)) { $BinaryPath } else { Join-Path $repositoryRoot $BinaryPath }
            if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
                throw "Built binary was not found: $binary"
            }
            $binaryMachine = Get-PeMachine -Path $binary
            if ($binaryMachine -ne $expectedMachine) {
                throw "$binary has PE machine 0x$('{0:X4}' -f $binaryMachine), expected 0x$('{0:X4}' -f $expectedMachine)"
            }
            Write-Host "Built binary matches $Architecture (PE 0x$('{0:X4}' -f $binaryMachine))"
        }

        if ($RunStaticcheck) {
            $staticcheck = Get-Command staticcheck -CommandType Application -ErrorAction Stop
            & $staticcheck.Source '-checks=all,-ST1000,-ST1005,-U1000,-SA2001' ./rclib
            if ($LASTEXITCODE -ne 0) {
                throw "staticcheck ./rclib failed with exit code $LASTEXITCODE"
            }
        }
    }
    finally {
        Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    }

    Write-Host "FFI validation passed for windows/$Architecture with CGO_ENABLED=0"
}
finally {
    Pop-Location
}
