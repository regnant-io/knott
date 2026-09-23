# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0
# Build a Windows desktop installer and portable ZIP from the current checkout.
param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)(?:[-+][a-zA-Z0-9._-]+)?$') {
    throw 'Version must begin with three numeric components, for example 1.2.0-local.'
}
$numericVersion = "$($Matches[1]).$($Matches[2]).$($Matches[3])"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$dist = Join-Path $root 'dist'
$stage = Join-Path $dist 'win-local'
$portableName = "KNOTT-$Version-windows-x64-portable"
$portable = Join-Path $dist $portableName
$installer = Join-Path $dist "KNOTT-$Version-windows-x64-setup.exe"
$zip = Join-Path $dist "$portableName.zip"
$uiDist = Join-Path $root 'apps\designer\dist'
$embeddedUI = Join-Path $root 'internal\ui\dist'
$desktopDir = Join-Path $root 'desktop'
$nsis = @(
    (Join-Path ${env:ProgramFiles(x86)} 'NSIS\makensis.exe'),
    (Join-Path $env:ProgramFiles 'NSIS\makensis.exe')
) | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1
if (-not $nsis) { throw 'NSIS makensis.exe is required to build the installer.' }

function Run([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}
function Remove-Generated([string]$Path) {
    $full = [IO.Path]::GetFullPath($Path)
    $distRoot = [IO.Path]::GetFullPath($dist).TrimEnd('\') + '\'
    if (-not $full.StartsWith($distRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove a path outside dist: $full"
    }
    if (Test-Path -LiteralPath $full) { Remove-Item -LiteralPath $full -Recurse -Force }
}

New-Item -ItemType Directory -Force -Path $dist, $embeddedUI | Out-Null
Remove-Generated $stage
Remove-Generated $portable
New-Item -ItemType Directory -Force -Path (Join-Path $stage 'desktop'), (Join-Path $stage 'cli'), (Join-Path $portable 'bin'), (Join-Path $portable 'data') | Out-Null

Push-Location $root
try {
    Run 'npm.cmd' @('--prefix', 'apps/designer', 'ci', '--no-audit', '--no-fund')
    Run 'npm.cmd' @('--prefix', 'apps/designer', 'run', 'build')
    if (-not (Test-Path -LiteralPath (Join-Path $uiDist 'index.html'))) { throw 'Console build is missing index.html.' }
    foreach ($name in @('assets', 'index.html', 'favicon.svg')) {
        $target = Join-Path $embeddedUI $name
        if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force }
    }
    Copy-Item -Path (Join-Path $uiDist '*') -Destination $embeddedUI -Recurse -Force

    $commit = (& git rev-parse --short HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Could not read the Git commit.' }
    $ldflags = "-s -w -X main.version=$Version -X main.commit=$commit"
    Push-Location $desktopDir
    try {
        Run 'go.exe' @('run', 'github.com/tc-hib/go-winres@v0.3.3', 'make', '--in', 'winres/winres.json', '--out', 'rsrc', '--arch', 'amd64', '--product-version', "$numericVersion.0", '--file-version', "$numericVersion.0")
        Run 'go.exe' @('build', '-trimpath', '-tags', 'desktop,production', '-ldflags', "$ldflags -H windowsgui", '-o', (Join-Path $stage 'desktop\KNOTT.exe'), '.')
    } finally { Pop-Location }
    Run 'go.exe' @('build', '-trimpath', '-ldflags', $ldflags, '-o', (Join-Path $stage 'cli\knott.exe'), './cmd/knott')

    $moduleInfo = & go.exe version -m (Join-Path $stage 'desktop\KNOTT.exe')
    if ($LASTEXITCODE -ne 0 -or -not ($moduleInfo | Select-String -Pattern '^\s*path\s+github\.com/regnant/knott/desktop$' -Quiet)) {
        throw 'KNOTT.exe is not the native desktop module; refusing to package it.'
    }

    Copy-Item -LiteralPath (Join-Path $root 'LICENSE'), (Join-Path $root 'NOTICE') -Destination $stage
    Run $nsis @("-DVERSION=$numericVersion", '-DARCH=x64', "-DSOURCE=$stage", "-DOUTFILE=$installer", (Join-Path $root 'build\windows\installer.nsi'))

    Copy-Item -LiteralPath (Join-Path $stage 'desktop\KNOTT.exe') -Destination $portable
    Copy-Item -LiteralPath (Join-Path $root 'brand\icons\knott-64.png') -Destination (Join-Path $portable 'knott-notification.png')
    Copy-Item -LiteralPath (Join-Path $stage 'cli\knott.exe') -Destination (Join-Path $portable 'bin')
    Copy-Item -LiteralPath (Join-Path $root 'LICENSE'), (Join-Path $root 'NOTICE'), (Join-Path $root 'README.md') -Destination $portable
    Set-Content -LiteralPath (Join-Path $portable 'data\README.txt') -Value 'KNOTT keeps workflows, runs, and credentials in this portable folder.'
    if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
    Compress-Archive -LiteralPath $portable -DestinationPath $zip -CompressionLevel Optimal
    Remove-Generated $portable
    Remove-Generated $stage
    Get-FileHash -Algorithm SHA256 -LiteralPath $installer, $zip | ForEach-Object { "$($_.Hash)  $([IO.Path]::GetFileName($_.Path))" } | Set-Content -LiteralPath (Join-Path $dist "KNOTT-$Version-windows-x64-SHA256SUMS")
    Write-Host "Built $installer"
    Write-Host "Built $zip"
} finally { Pop-Location }
