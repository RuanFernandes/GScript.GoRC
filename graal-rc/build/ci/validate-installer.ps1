[CmdletBinding()]
param(
    [ValidateSet('amd64', '386')]
    [string]$Architecture,
    [Parameter(Mandatory = $true)]
    [string]$BinaryPath,
    [string]$OutputDirectory = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if (-not [System.IO.Path]::IsPathRooted($BinaryPath)) {
    $BinaryPath = Join-Path $repositoryRoot $BinaryPath
}
if ($OutputDirectory -eq '') {
    $OutputDirectory = Join-Path $repositoryRoot (Join-Path 'ci-artifacts\installers' $Architecture)
}
elseif (-not [System.IO.Path]::IsPathRooted($OutputDirectory)) {
    $OutputDirectory = Join-Path $repositoryRoot $OutputDirectory
}

if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
    throw "Built binary is missing: $BinaryPath"
}

$makensis = Get-Command makensis -CommandType Application -ErrorAction SilentlyContinue
if ($null -eq $makensis) {
    Write-Output '::notice::makensis is not available on this runner; NSIS installer validation skipped.'
    exit 0
}

$nativeName = if ($Architecture -eq 'amd64') { 'grclib64.dll' } else { 'grclib.dll' }
$nativePath = Join-Path $repositoryRoot (Join-Path "rclib\native\windows-$Architecture" $nativeName)

$wails = Get-Command wails3 -CommandType Application -ErrorAction Stop
$nsisDirectory = Join-Path $repositoryRoot 'build\windows\nsis'
$bootstrapper = Join-Path $nsisDirectory 'MicrosoftEdgeWebview2Setup.exe'
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

Write-Host 'Generating the WebView2 bootstrapper consumed by the NSIS script'
& $wails.Source generate webview2bootstrapper -dir $nsisDirectory
if ($LASTEXITCODE -ne 0) {
    throw "wails3 generate webview2bootstrapper failed with exit code $LASTEXITCODE"
}
if (-not (Test-Path -LiteralPath $bootstrapper -PathType Leaf)) {
    throw "WebView2 bootstrapper was not generated: $bootstrapper"
}

$installerPath = Join-Path $repositoryRoot "bin\graal-rc-$Architecture-installer.exe"
$compileScript = Join-Path $repositoryRoot 'build\windows\compile-nsis.ps1'

# Keep the release installer on the legacy machine-wide path. This lets it
# replace pre-3.1 installations instead of creating a second per-user copy
# with a competing Start Menu shortcut. compile-nsis.ps1 also supplies the
# generated INFO_* metadata required by the NSIS project.
& $compileScript `
    -InstallScope machine `
    -Architecture $Architecture `
    -AppName graal-rc `
    -ExecutablePath $BinaryPath `
    -NativeLibraryPath $nativePath `
    -NativeLibraryName $nativeName `
    -MakensisPath $makensis.Source
if ($LASTEXITCODE -ne 0) {
    throw "compile-nsis.ps1 failed with exit code $LASTEXITCODE"
}

if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
    throw "NSIS did not produce the expected installer: $installerPath"
}

$artifactPath = Join-Path $OutputDirectory "graal-rc-$Architecture-installer.exe"
Copy-Item -LiteralPath $installerPath -Destination $artifactPath -Force
$hash = (Get-FileHash -LiteralPath $artifactPath -Algorithm SHA256).Hash.ToLowerInvariant()
[System.IO.File]::WriteAllText((Join-Path $OutputDirectory 'SHA256SUMS.txt'), "$hash  $([System.IO.Path]::GetFileName($artifactPath))`r`n", [System.Text.Encoding]::ASCII)

Write-Host "NSIS installer compilation passed for $Architecture"
Write-Host "SHA-256 manifest: $(Join-Path $OutputDirectory 'SHA256SUMS.txt')"
