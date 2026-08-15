[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InstallerPath,
    [int]$StartupTimeoutSeconds = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $InstallerPath -PathType Leaf)) {
    throw "Installer is missing: $InstallerPath"
}

$programFilesDirectory = [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)
$installDirectory = Join-Path $programFilesDirectory 'RuanFernandes\Graal Remote Control'
$applicationPath = Join-Path $installDirectory 'graal-rc.exe'
$uninstallerPath = Join-Path $installDirectory 'uninstall.exe'
$applicationProcess = $null

try {
    Write-Host 'Installing the machine-wide NSIS package silently'
    $installerProcess = Start-Process -FilePath $InstallerPath -ArgumentList '/S' -Verb RunAs -Wait -PassThru
    if ($installerProcess.ExitCode -ne 0) {
        throw "NSIS installer exited with code $($installerProcess.ExitCode)"
    }
    if (-not (Test-Path -LiteralPath $applicationPath -PathType Leaf)) {
        throw "Installed application was not found: $applicationPath"
    }

    Write-Host "Starting the installed application and waiting $StartupTimeoutSeconds seconds"
    $applicationProcess = Start-Process -FilePath $applicationPath -PassThru
    Start-Sleep -Seconds $StartupTimeoutSeconds
    if ($applicationProcess.HasExited) {
        throw "Installed application exited during the WebView2 smoke test with code $($applicationProcess.ExitCode)"
    }

    Write-Host 'WebView2/installer smoke test passed: application stayed running'
}
finally {
    if ($null -ne $applicationProcess -and -not $applicationProcess.HasExited) {
        Stop-Process -Id $applicationProcess.Id -Force -ErrorAction SilentlyContinue
    }

    if (Test-Path -LiteralPath $uninstallerPath -PathType Leaf) {
        Write-Host 'Removing the machine-wide installation after the smoke test'
        $uninstallerProcess = Start-Process -FilePath $uninstallerPath -ArgumentList '/S' -Verb RunAs -Wait -PassThru
        if ($uninstallerProcess.ExitCode -ne 0) {
            Write-Output "::warning::Uninstaller exited with code $($uninstallerProcess.ExitCode)"
        }
    }
}
