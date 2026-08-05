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
$nativePath = Join-Path $repositoryRoot (Join-Path 'rclib' $nativeName)
if (-not (Test-Path -LiteralPath $nativePath -PathType Leaf)) {
    if ($Architecture -eq '386') {
        Write-Output '::notice::rclib/grclib.dll is not present; x86 NSIS validation is skipped because the installer cannot ship a matching FFI library.'
        exit 0
    }
    throw "Required native library is missing: $nativePath"
}

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
$defines = if ($Architecture -eq 'amd64') {
    @(
        "-DARG_WAILS_AMD64_BINARY=$BinaryPath",
        "-DARG_GRCLIB_DLL=$nativePath",
        '-DWAILS_INSTALL_SCOPE=user',
        '-DREQUEST_EXECUTION_LEVEL=user',
        'project.nsi'
    )
}
else {
    @(
        "-DARG_WAILS_X86_BINARY=$BinaryPath",
        "-DARG_GRCLIB_DLL=$nativePath",
        '-DWAILS_INSTALL_SCOPE=user',
        '-DREQUEST_EXECUTION_LEVEL=user',
        'project.nsi'
    )
}

Push-Location $nsisDirectory
try {
    & $makensis.Source @defines
    if ($LASTEXITCODE -ne 0) {
        throw "makensis failed with exit code $LASTEXITCODE"
    }
}
finally {
    Pop-Location
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
