# Jianmen build script (Windows PowerShell)
# Build frontend + compile Windows/Linux binaries with embedded frontend

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$frontendDist = Join-Path $root "web\dist"
$embedDir = Join-Path $root "internal\frontend\dist"
$outputDir = Join-Path $root "dist"

Write-Host "============================================"
Write-Host "  Jianmen Build (with embedded frontend)"
Write-Host "============================================"
Write-Host ""

# === 1. Build frontend ===
Write-Host "[1/4] Building frontend..." -ForegroundColor Cyan
Push-Location (Join-Path $root "web")
try {
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
    Write-Host "  OK frontend built" -ForegroundColor Green
} finally {
    Pop-Location
}

# === 2. Copy frontend dist to embed directory ===
Write-Host "[2/4] Copying frontend to embed dir..." -ForegroundColor Cyan
if (Test-Path $embedDir) {
    Remove-Item -Recurse -Force $embedDir
}
Copy-Item -Recurse $frontendDist $embedDir
$fileCount = (Get-ChildItem -Recurse $embedDir | Where-Object { -not $_.PSIsContainer }).Count
Write-Host "  OK $fileCount files copied" -ForegroundColor Green

# === 3. Build Windows amd64 ===
Write-Host "[3/4] Building Windows amd64..." -ForegroundColor Cyan
Push-Location $root
New-Item -ItemType Directory -Force $outputDir | Out-Null
$winOut = Join-Path $outputDir "bastion-core-windows-amd64.exe"
go build -o $winOut ./cmd/bastion-core/
if ($LASTEXITCODE -ne 0) { throw "Windows build failed" }
$winSize = [math]::Round((Get-Item $winOut).Length / 1MB, 1)
Write-Host "  OK bastion-core-windows-amd64.exe ($($winSize)MB)" -ForegroundColor Green
Pop-Location

# === 4. Build Linux amd64 ===
Write-Host "[4/4] Building Linux amd64..." -ForegroundColor Cyan
Push-Location $root
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$linuxOut = Join-Path $outputDir "bastion-core-linux-amd64"
go build -o $linuxOut ./cmd/bastion-core/
if ($LASTEXITCODE -ne 0) { throw "Linux build failed" }
$env:GOOS = ""
$env:GOARCH = ""
$env:CGO_ENABLED = ""
$linuxSizeDerp = [math]::Round((Get-Item $linuxOut).Length / 1MB, 1)
Write-Host "  OK bastion-core-linux-amd64 ($($linuxSizeDerp)MB)" -ForegroundColor Green
Pop-Location

# === Done ===
Write-Host ""
Write-Host "============================================"
Write-Host "  Build complete"
Write-Host "============================================"
Write-Host ""
Get-ChildItem $outputDir -File | ForEach-Object {
    $size = [math]::Round($_.Length / 1MB, 1)
    Write-Host "  $($_.Name) ($($size)MB)"
}
Write-Host ""
Write-Host "Linux deploy: scp dist/bastion-core-linux-amd64 user@server:/opt/jianmen/" -ForegroundColor Yellow
