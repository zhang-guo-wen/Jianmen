# Quick restart: kill, build, launch, verify (logs to data/backend.log)
$ErrorActionPreference = "Stop"
Get-Process -Name "bastion-core" -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process -Name "node" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1

Set-Location $PSScriptRoot

# Ensure config
if (-not (Test-Path config.local.json)) { Copy-Item config.example.json config.local.json }

# Build
Write-Host "Building..." -NoNewline
go build -o bin\bastion-core.exe .\cmd\bastion-core
Write-Host " done"

# Backend with log redirection
Start-Process -FilePath "$PSScriptRoot\bin\bastion-core.exe" `
  -ArgumentList "-config","config.local.json" `
  -WorkingDirectory $PSScriptRoot `
  -WindowStyle Hidden `
  -RedirectStandardOutput "$PSScriptRoot\data\backend.log" `
  -RedirectStandardError "$PSScriptRoot\data\backend_err.log"

Start-Sleep -Seconds 2

# Frontend
Start-Process -FilePath "cmd.exe" -ArgumentList "/c","cd /d `"$PSScriptRoot\web`" && npx vite --host 127.0.0.1 --port 47101" -WindowStyle Hidden
Start-Sleep -Seconds 4

# Verify
$headers = @{Authorization = "Bearer dev-admin-token"}
try { Invoke-RestMethod 'http://127.0.0.1:47100/api/health' -Headers $headers -TimeoutSec 3 | Out-Null; Write-Host "后端 OK" } catch { Write-Host "后端 FAIL" }
try { Invoke-WebRequest 'http://127.0.0.1:47101/' -UseBasicParsing -TimeoutSec 5 | Out-Null; Write-Host "前端 OK" } catch { Write-Host "前端 FAIL" }

# Tail log
Write-Host "`n--- 后端日志 ---"
Get-Content "$PSScriptRoot\data\backend.log" -Tail 12 -ErrorAction SilentlyContinue
