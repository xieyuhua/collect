# 跨平台打包脚本：bin\collect.exe（当前平台）+ dist\ 下 Windows / Linux 产物
param(
    [string]$OutDir = "dist"
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$env:CGO_ENABLED = "0"

if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Path $OutDir | Out-Null }
if (-not (Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }

function Build-Target([string]$goos, [string]$goarch, [string]$out) {
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    Write-Host ("[build] {0}/{1} -> {2}" -f $goos, $goarch, $out) -ForegroundColor Cyan
    go build -trimpath -ldflags "-s -w" -o $out .
    if ($LASTEXITCODE -ne 0) { throw "编译失败: $goos/$goarch" }
}

Build-Target "windows" "amd64" "bin\collect.exe"
Build-Target "windows" "amd64" (Join-Path $OutDir "collect-windows-amd64.exe")
Build-Target "linux" "amd64" (Join-Path $OutDir "collect-linux-amd64")
Build-Target "linux" "arm64" (Join-Path $OutDir "collect-linux-arm64")

$env:GOOS = ""
$env:GOARCH = ""

Write-Host ""
Write-Host "产物：" -ForegroundColor Green
Get-ChildItem $OutDir, "bin\collect.exe" | ForEach-Object {
    "{0,-30} {1,8:N1} MB" -f $_.FullName.Replace($PSScriptRoot + "\", ""), ($_.Length / 1MB)
}
Write-Host ""
Write-Host "部署提示：Linux 首次运行前把 config.json 的 browser.no_sandbox 设为 true（无 GUI 环境）。"