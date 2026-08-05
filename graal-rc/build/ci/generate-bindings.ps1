[CmdletBinding()]
param(
    [string]$BuildFlags = '-tags production',
    [string]$OutputDirectory = 'frontend/bindings'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $repositoryRoot
try {
    $wails = Get-Command wails3 -CommandType Application -ErrorAction Stop

    Write-Host "Generating Wails TypeScript bindings with: $BuildFlags"
    & $wails.Source generate bindings -f $BuildFlags -clean=true -ts -i
    if ($LASTEXITCODE -ne 0) {
        throw "wails3 generate bindings failed with exit code $LASTEXITCODE"
    }

    $requiredFiles = @(
        (Join-Path $OutputDirectory 'graal-rc\index.ts'),
        (Join-Path $OutputDirectory 'graal-rc\app.ts'),
        (Join-Path $OutputDirectory 'graal-rc\models.ts')
    )

    foreach ($file in $requiredFiles) {
        if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
            throw "Bindings generation did not produce the required file: $file"
        }
    }

    $bindingCount = @(Get-ChildItem -LiteralPath $OutputDirectory -Recurse -File -Include '*.ts', '*.d.ts').Count
    if ($bindingCount -lt 3) {
        throw "Bindings generation produced too few TypeScript files: $bindingCount"
    }

    Write-Host "Generated $bindingCount TypeScript binding files under $OutputDirectory"
}
finally {
    Pop-Location
}
