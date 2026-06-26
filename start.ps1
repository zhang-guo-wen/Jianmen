# Jianmen one-click dev startup
# Usage: .\start.ps1
# First run will set up everything automatically

$ErrorActionPreference = "Stop"

Write-Host "=== Jianmen Dev Startup ===" -ForegroundColor Cyan
Write-Host ""

# ---- 0. Cleanup old instances ----
Write-Host "[1/5] Stopping old instances..." -ForegroundColor Yellow
Get-Process -Name "bastion-core" -ErrorAction SilentlyContinue | Stop-Process -Force
$oldNode = Get-Process -Name "node" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -like "*vite*" }
if ($oldNode) { $oldNode | Stop-Process -Force }
Start-Sleep -Seconds 1

# ---- 1. Config ----
Write-Host "[2/5] Preparing config..." -ForegroundColor Yellow
if (-not (Test-Path config.local.json)) {
    Copy-Item config.example.json config.local.json
    Write-Host "  Created config.local.json from template" -ForegroundColor Green
} else {
    Write-Host "  config.local.json already exists" -ForegroundColor Gray
}

# ---- 2. Build backend ----
Write-Host "[3/5] Building backend..." -ForegroundColor Yellow
go build -o bin\bastion-core.exe .\cmd\bastion-core
if ($LASTEXITCODE -ne 0) { throw "Backend build failed" }
Write-Host "  Backend built: bin\bastion-core.exe" -ForegroundColor Green

# ---- 3. Start backend ----
Write-Host "[4/5] Starting backend..." -ForegroundColor Yellow
$backendJob = Start-Job -Name "bastion-core" -ScriptBlock {
    Set-Location $using:PWD
    .\bin\bastion-core.exe -config config.local.json 2>&1 | Out-File "logs\backend.log" -Encoding utf8
}
Start-Sleep -Seconds 3
$backendStatus = Get-Job -Name "bastion-core"
if ($backendStatus.State -eq "Failed") {
    Write-Host "  Backend failed to start! Check logs\backend.log" -ForegroundColor Red
    Receive-Job -Name "bastion-core"
    throw "Backend startup failed"
}
Write-Host "  Backend running (PID $($backendStatus.ChildJobs[0].Process.Id))" -ForegroundColor Green

# ---- 4. Frontend deps ----
Write-Host "[5/5] Starting frontend..." -ForegroundColor Yellow
Push-Location web
if (-not (Test-Path node_modules)) {
    Write-Host "  Installing npm dependencies..." -ForegroundColor Gray
    npm install 2>&1 | Out-Null
}
$frontendJob = Start-Job -Name "vite-dev" -ScriptBlock {
    Set-Location "$using:PWD\web"
    npm run dev 2>&1 | Out-File "..\logs\frontend.log" -Encoding utf8
}
Pop-Location
Start-Sleep -Seconds 3
$frontendStatus = Get-Job -Name "vite-dev"
if ($frontendStatus.State -eq "Failed") {
    Write-Host "  Frontend failed to start! Check logs\frontend.log" -ForegroundColor Red
    Receive-Job -Name "vite-dev"
    throw "Frontend startup failed"
}
Write-Host "  Frontend running" -ForegroundColor Green

# ---- Done ----
Write-Host ""
Write-Host "=== All services started ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Admin API : http://127.0.0.1:47100/" -ForegroundColor White
Write-Host "  Web UI    : http://127.0.0.1:47101/" -ForegroundColor White
Write-Host "  SSH GW    : 127.0.0.1:47102" -ForegroundColor White
Write-Host "  Token     : dev-admin-token" -ForegroundColor Gray
Write-Host ""
Write-Host "Stop with: Get-Job | Stop-Job" -ForegroundColor Gray
