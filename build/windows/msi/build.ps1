# Builds the per-user memo MSI with WiX Toolset 3.14.
#
# The WiX binaries are not committed. Download wix314-binaries.zip from
# https://github.com/wixtoolset/wix3/releases/tag/wix3141rtm and point -WixDir
# (or WIX_DIR) at the extracted directory. CI does this automatically.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$SourceExe = "",
    [string]$Out = "",
    [string]$WixDir = $env:WIX_DIR
)

$ErrorActionPreference = "Stop"

$appDir = (Resolve-Path (Join-Path $PSScriptRoot "..\..\..")).Path
$binDir = Join-Path $appDir "bin"
$iconPath = Join-Path $appDir "build\windows\icon.ico"
if (-not $SourceExe) { $SourceExe = Join-Path $binDir "memo.exe" }
if (-not $Out) { $Out = Join-Path $binDir "memo-windows-amd64.msi" }
if (-not $WixDir) {
    throw "WiX 3.14 not found. Pass -WixDir or set WIX_DIR to the directory containing candle.exe and light.exe."
}

$candle = Join-Path $WixDir "candle.exe"
$light = Join-Path $WixDir "light.exe"
foreach ($tool in @($candle, $light, $SourceExe, $iconPath)) {
    if (-not (Test-Path $tool)) { throw "Missing required file: $tool" }
}

$sourceDir = Split-Path -Parent (Resolve-Path $SourceExe).Path
$objDir = Join-Path ([System.IO.Path]::GetTempPath()) ("memo-msi-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $objDir | Out-Null
$outDir = Split-Path -Parent $Out
if ($outDir) { New-Item -ItemType Directory -Force -Path $outDir | Out-Null }

try {
    & $candle -nologo -arch x64 "-dVersion=$Version" "-dSourceDir=$sourceDir" "-dIconPath=$iconPath" `
        -out (Join-Path $objDir "memo.wixobj") (Join-Path $PSScriptRoot "memo.wxs")
    if ($LASTEXITCODE -ne 0) { throw "candle failed with exit code $LASTEXITCODE" }

    & $light -nologo -ext WixUIExtension (Join-Path $objDir "memo.wixobj") -out $Out
    if ($LASTEXITCODE -ne 0) { throw "light failed with exit code $LASTEXITCODE" }
}
finally {
    Remove-Item -Recurse -Force $objDir -ErrorAction SilentlyContinue
}

Write-Host "MSI written to $Out"
