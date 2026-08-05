[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [Parameter(Mandatory = $true)]
    [string]$NativeLibraryPath,

    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64', '386')]
    [string]$Architecture
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$expectedMachine = @{
    amd64 = 0x8664
    arm64 = 0xAA64
    '386' = 0x014C
}[$Architecture]

function Get-PeMachine {
    param([Parameter(Mandatory = $true)][string]$Path)

    $resolvedPath = (Resolve-Path -LiteralPath $Path -ErrorAction Stop).Path
    $bytes = [System.IO.File]::ReadAllBytes($resolvedPath)

    if ($bytes.Length -lt 0x40 -or $bytes[0] -ne 0x4D -or $bytes[1] -ne 0x5A) {
        throw "'$Path' is not a valid PE file."
    }

    $peOffset = [System.BitConverter]::ToInt32($bytes, 0x3C)
    if ($peOffset -lt 0 -or $peOffset + 6 -gt $bytes.Length) {
        throw "'$Path' has an invalid PE header offset."
    }

    $signature = [System.BitConverter]::ToUInt32($bytes, $peOffset)
    if ($signature -ne 0x00004550) {
        throw "'$Path' is not a valid Windows PE image."
    }

    return [System.BitConverter]::ToUInt16($bytes, $peOffset + 4)
}

try {
    foreach ($file in @($ExecutablePath, $NativeLibraryPath)) {
        if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
            throw "Required native packaging file was not found: '$file'."
        }
    }

    if ([System.IO.Path]::GetExtension($ExecutablePath) -ne '.exe') {
        throw "The application payload must be an .exe file: '$ExecutablePath'."
    }

    if ([System.IO.Path]::GetExtension($NativeLibraryPath) -ne '.dll') {
        throw "The native Windows payload must be a .dll file: '$NativeLibraryPath'."
    }

    $executableMachine = Get-PeMachine -Path $ExecutablePath
    $nativeMachine = Get-PeMachine -Path $NativeLibraryPath

    if ($executableMachine -ne $expectedMachine) {
        throw "The executable architecture does not match '$Architecture' (PE machine 0x{0:X4})." -f $executableMachine
    }

    if ($nativeMachine -ne $expectedMachine) {
        throw "The native library architecture does not match the '$Architecture' executable (PE machine 0x{0:X4})." -f $nativeMachine
    }

    Write-Host ("Validated {0} executable and native library: PE machine 0x{1:X4}." -f $Architecture, $expectedMachine)
    exit 0
}
catch {
    Write-Error $_.Exception.Message
    exit 1
}
