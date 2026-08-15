[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('user', 'machine')]
    [string]$InstallScope,

    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64')]
    [string]$Architecture,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$AppName,

    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,

    [Parameter(Mandatory = $true)]
    [string]$NativeLibraryPath,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$NativeLibraryName,

    [string]$InfoPath,

    [string]$ProjectPath,

    [string]$MakensisPath = 'makensis'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($InfoPath)) {
    $InfoPath = Join-Path $PSScriptRoot 'info.json'
}

if ([string]::IsNullOrWhiteSpace($ProjectPath)) {
    $ProjectPath = Join-Path $PSScriptRoot 'nsis\project.nsi'
}

function Resolve-ExistingFile {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Required installer input was not found: '$Path'."
    }

    return (Resolve-Path -LiteralPath $Path).Path
}

function Resolve-Makensis {
    param([Parameter(Mandatory = $true)][string]$RequestedPath)

    $command = Get-Command -Name $RequestedPath -ErrorAction SilentlyContinue
    if ($null -ne $command -and $command.CommandType -eq 'Application') {
        return $command.Source
    }

    $candidates = @($RequestedPath)
    foreach ($programFilesRoot in @(${env:ProgramFiles(x86)}, $env:ProgramFiles)) {
        if (-not [string]::IsNullOrWhiteSpace([string]$programFilesRoot)) {
            $candidates += Join-Path $programFilesRoot 'NSIS\makensis.exe'
        }
    }

    $candidates = $candidates | Where-Object { $_ -and (Test-Path -LiteralPath $_ -PathType Leaf) }

    foreach ($candidate in $candidates) {
        return (Resolve-Path -LiteralPath $candidate).Path
    }

    throw "makensis was not found. Add it to PATH or pass -MakensisPath with the NSIS executable path."
}

function Get-RequiredMetadataValue {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Metadata,

        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $property = $Metadata.PSObject.Properties[$Name]
    if ($null -eq $property -or [string]::IsNullOrWhiteSpace([string]$property.Value)) {
        throw "Windows metadata '$Name' is missing from build/windows/info.json."
    }

    $value = [string]$property.Value
    if ($value.Contains('"')) {
        throw "Windows metadata '$Name' contains a double quote that cannot be represented safely in an NSIS define."
    }

    return $value
}

$resolvedInfoPath = Resolve-ExistingFile -Path $InfoPath
$resolvedProjectPath = Resolve-ExistingFile -Path $ProjectPath
$resolvedExecutablePath = Resolve-ExistingFile -Path $ExecutablePath
$resolvedNativeLibraryPath = Resolve-ExistingFile -Path $NativeLibraryPath

if ([System.IO.Path]::GetExtension($resolvedExecutablePath) -ne '.exe') {
    throw "The application payload must be an .exe file: '$ExecutablePath'."
}

# The NSIS payload is written with `/oname=${PRODUCT_EXECUTABLE}`. CI artifacts
# intentionally carry their target architecture in the source filename (for
# example, graal-rc-windows-amd64.exe), so the input name need not match the
# installed executable name.

if ([System.IO.Path]::GetExtension($resolvedNativeLibraryPath) -ne '.dll') {
    throw "The native Windows payload must be a .dll file: '$NativeLibraryPath'."
}

if ($NativeLibraryName -ne 'grclib64.dll') {
    throw "Graal RC amd64 installers must package the native library as grclib64.dll."
}

if ([System.IO.Path]::GetFileName($resolvedNativeLibraryPath) -ne $NativeLibraryName) {
    throw "The native library payload '$NativeLibraryPath' must be named '$NativeLibraryName'."
}

$infoDocument = Get-Content -LiteralPath $resolvedInfoPath -Raw | ConvertFrom-Json
$metadata = $infoDocument.info.'0000'
if ($null -eq $metadata) {
    throw "Windows metadata section 'info.0000' is missing from '$InfoPath'."
}

$companyName = Get-RequiredMetadataValue -Metadata $metadata -Name 'CompanyName'
$productName = Get-RequiredMetadataValue -Metadata $metadata -Name 'ProductName'
$productVersion = Get-RequiredMetadataValue -Metadata $metadata -Name 'ProductVersion'
$copyright = Get-RequiredMetadataValue -Metadata $metadata -Name 'LegalCopyright'

if ($productVersion -notmatch '^\d+\.\d+\.\d+$') {
    throw "Windows metadata ProductVersion '$productVersion' must contain exactly three numeric components for NSIS."
}

$fixedVersionProperty = $infoDocument.fixed.PSObject.Properties['file_version']
if ($null -ne $fixedVersionProperty -and [string]$fixedVersionProperty.Value -ne $productVersion) {
    throw "The generated Windows metadata contains conflicting versions: info.0000.ProductVersion='$productVersion', fixed.file_version='$($fixedVersionProperty.Value)'."
}

$resolvedMakensisPath = Resolve-Makensis -RequestedPath $MakensisPath
$requestExecutionLevel = if ($InstallScope -eq 'user') { 'user' } else { 'admin' }
$defines = @(
    "-DINFO_PROJECTNAME=$AppName",
    "-DPRODUCT_EXECUTABLE=$AppName.exe",
    "-DINFO_COMPANYNAME=$companyName",
    "-DINFO_PRODUCTNAME=$productName",
    "-DINFO_PRODUCTVERSION=$productVersion",
    "-DINFO_COPYRIGHT=$copyright",
    "-DWAILS_INSTALL_SCOPE=$InstallScope",
    "-DREQUEST_EXECUTION_LEVEL=$requestExecutionLevel",
    "-DARG_WAILS_AMD64_BINARY=$resolvedExecutablePath",
    "-DARG_GRCLIB_FILE=$NativeLibraryName",
    "-DARG_GRCLIB_DLL=$resolvedNativeLibraryPath"
)

$projectDirectory = Split-Path -Parent $resolvedProjectPath
Push-Location -LiteralPath $projectDirectory
try {
    Write-Host "Compiling $productName $productVersion NSIS installer ($InstallScope, $Architecture) using metadata from $resolvedInfoPath."
    & $resolvedMakensisPath @defines $resolvedProjectPath
    $exitCode = $LASTEXITCODE
}
finally {
    Pop-Location
}

if ($exitCode -ne 0) {
    exit $exitCode
}
