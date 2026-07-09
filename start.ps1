# Jianmen dev startup / restart
# Usage:
#   .\start.ps1           - Full: stop old, build, launch, verify
#   .\start.ps1 -SkipBuild - Skip build step (quick restart after code changes)

param([switch]$SkipBuild)

$ErrorActionPreference = "Stop"

function Write-Step($Message) {
    Write-Host $Message -ForegroundColor Yellow
}

function Write-Ok($Message) {
    Write-Host "  $Message" -ForegroundColor Green
}

function Write-Info($Message) {
    Write-Host "  $Message" -ForegroundColor Gray
}

function Show-LogTail($Path, $Lines) {
    if (Test-Path $Path) {
        Write-Host "--- $Path (last $Lines lines) ---" -ForegroundColor DarkGray
        Get-Content $Path -Tail $Lines
    } else {
        Write-Host "--- $Path not found ---" -ForegroundColor DarkGray
    }
}

function Stop-ProcessFromPidFile($PidFile, $Name) {
    if (Test-Path $PidFile) {
        $rawPid = (Get-Content $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
        if ($rawPid) {
            $proc = Get-Process -Id ([int]$rawPid) -ErrorAction SilentlyContinue
            if ($proc) {
                Write-Info "Stopping $Name PID $rawPid"
                Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
            }
        }
        Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    }
}

function Stop-ProcessOnPort($Port, $Name) {
    $connections = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    foreach ($connection in $connections) {
        $proc = Get-Process -Id $connection.OwningProcess -ErrorAction SilentlyContinue
        if ($proc) {
            Write-Info "Stopping $Name port $Port PID $($proc.Id) ($($proc.ProcessName))"
            Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        }
    }
}

function Wait-HttpOk($Name, $Url, $TimeoutSeconds, $Headers) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = $null
    while ((Get-Date) -lt $deadline) {
        try {
            if ($Headers) {
                $response = Invoke-WebRequest $Url -UseBasicParsing -TimeoutSec 2 -Headers $Headers
            } else {
                $response = Invoke-WebRequest $Url -UseBasicParsing -TimeoutSec 2
            }
            if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 500) {
                Write-Ok "$Name ready: $Url ($($response.StatusCode))"
                return
            }
        } catch {
            $lastError = $_.Exception.Message
            if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
                $statusCode = [int]$_.Exception.Response.StatusCode
                if ($statusCode -ge 200 -and $statusCode -lt 500) {
                    Write-Ok "$Name ready: $Url ($statusCode)"
                    return
                }
            }
        }
        Start-Sleep -Milliseconds 500
    }
    throw "$Name not ready after ${TimeoutSeconds}s: $Url; last error: $lastError"
}

function Wait-TcpPort($Name, $HostName, $Port, $TimeoutSeconds) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $client = New-Object System.Net.Sockets.TcpClient
        try {
            $iar = $client.BeginConnect($HostName, $Port, $null, $null)
            if ($iar.AsyncWaitHandle.WaitOne(1000, $false)) {
                $client.EndConnect($iar)
                Write-Ok "$Name ready: ${HostName}:$Port"
                return
            }
        } catch {
        } finally {
            $client.Close()
        }
        Start-Sleep -Milliseconds 500
    }
    throw "$Name not ready after ${TimeoutSeconds}s: ${HostName}:$Port"
}

function Fail-WithDiagnostics($Message) {
    Write-Host ""
    Write-Host "Startup failed: $Message" -ForegroundColor Red
    Show-LogTail "logs\backend.err.log" 80
    Show-LogTail "logs\backend.log" 80
    Show-LogTail "logs\frontend.err.log" 80
    Show-LogTail "logs\frontend.log" 80
    exit 1
}

try {
    $root = (Get-Location).Path
    $logsDir = Join-Path $root "logs"
    $binDir = Join-Path $root "bin"
    $backendLog = Join-Path $logsDir "backend.log"
    $backendErrLog = Join-Path $logsDir "backend.err.log"
    $backendPid = Join-Path $logsDir "backend.pid"
    $frontendLog = Join-Path $logsDir "frontend.log"
    $frontendErrLog = Join-Path $logsDir "frontend.err.log"
    $frontendPid = Join-Path $logsDir "frontend.pid"

    Write-Host "=== Jianmen Dev Startup ===" -ForegroundColor Cyan
    if ($SkipBuild) { Write-Host "(skip build)" -ForegroundColor DarkGray }
    Write-Host ""

    Write-Step "[1/5] Preparing directories..."
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    New-Item -ItemType Directory -Force -Path $logsDir | Out-Null
    Write-Ok "runtime directories ready"

    Write-Step "[2/5] Stopping old instances..."
    Stop-ProcessFromPidFile $backendPid "bastion-core"
    Stop-ProcessFromPidFile $frontendPid "vite-dev"
    Stop-ProcessOnPort 47100 "Admin API"
    Stop-ProcessOnPort 47101 "Web UI"
    Stop-ProcessOnPort 47102 "SSH gateway"
    Stop-ProcessOnPort 33060 "Database gateway"
    Get-Process -Name "bastion-core" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    $oldNode = Get-Process -Name "node" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -like "*vite*" -or $_.CommandLine -like "*npm*run*dev*" }
    if ($oldNode) { $oldNode | Stop-Process -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 1
    Write-Ok "old instances stopped"

    if (-not $SkipBuild) {
        Write-Step "[3/5] Building backend..."
        go build -o "bin\bastion-core.exe" ".\cmd\bastion-core"
        if ($LASTEXITCODE -ne 0) { throw "Backend build failed" }
        Write-Ok "backend built: bin\bastion-core.exe"
    } else {
        Write-Step "[3/5] Skipping build..."
        if (-not (Test-Path "bin\bastion-core.exe")) {
            Write-Info "binary not found, building anyway..."
            go build -o "bin\bastion-core.exe" ".\cmd\bastion-core"
            if ($LASTEXITCODE -ne 0) { throw "Backend build failed" }
            Write-Ok "backend built: bin\bastion-core.exe"
        } else {
            Write-Ok "using existing binary"
        }
    }

    Write-Step "[4/5] Starting backend..."
    Remove-Item $backendLog, $backendErrLog -Force -ErrorAction SilentlyContinue
    $backend = Start-Process -FilePath (Join-Path $binDir "bastion-core.exe") -ArgumentList "-config", "config.local.json" -WorkingDirectory $root -RedirectStandardOutput $backendLog -RedirectStandardError $backendErrLog -PassThru
    Set-Content -Path $backendPid -Value $backend.Id -Encoding ascii
    Write-Ok "backend process started: PID $($backend.Id)"

    Wait-HttpOk "Admin API" "http://127.0.0.1:47100/api/health" 20 $null
    Wait-TcpPort "Database gateway" "127.0.0.1" 33060 10
    Wait-TcpPort "SSH gateway" "127.0.0.1" 47102 10

    # 获取 admin 用户的真实 token（用 admin/admin 登录）
    try {
        $loginResp = Invoke-RestMethod -Uri "http://127.0.0.1:47100/api/login" -Method Post -Body '{"username":"admin","password":"admin"}' -ContentType "application/json" -TimeoutSec 5
        $realToken = $loginResp.token
        Write-Ok "Admin login OK, token: $($realToken.Substring(0,16))..."
    } catch {
        $realToken = $null
        Write-Info "Admin auto-login failed, login manually"
    }

    Write-Step "[5/5] Starting frontend..."
    Push-Location "web"
    try {
        if (-not (Test-Path "node_modules")) {
            Write-Info "installing npm dependencies..."
            npm install
            if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
        }
    } finally {
        Pop-Location
    }

    Remove-Item $frontendLog, $frontendErrLog -Force -ErrorAction SilentlyContinue
    $npmCommand = (Get-Command "npm.cmd" -ErrorAction SilentlyContinue).Source
    if (-not $npmCommand) { $npmCommand = (Get-Command "npm" -ErrorAction Stop).Source }
    $frontend = Start-Process -FilePath $npmCommand -ArgumentList "run", "dev", "--", "--host", "127.0.0.1", "--strictPort" -WorkingDirectory (Join-Path $root "web") -RedirectStandardOutput $frontendLog -RedirectStandardError $frontendErrLog -PassThru
    Set-Content -Path $frontendPid -Value $frontend.Id -Encoding ascii
    Write-Ok "frontend process started: PID $($frontend.Id)"

    Wait-HttpOk "Web UI" "http://127.0.0.1:47101/" 30 $null
    # 用真实 token 验证 API 代理
    if ($realToken) {
        Wait-HttpOk "Frontend API proxy" "http://127.0.0.1:47101/api/hosts" 15 @{Authorization="Bearer $realToken"}
    } else {
        Wait-HttpOk "Frontend API proxy" "http://127.0.0.1:47101/api/hosts" 15 @{Authorization='Bearer dev-admin-token'}
    }

    Write-Host ""
    Write-Host "=== All services started and verified ===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Admin API : http://127.0.0.1:47100/" -ForegroundColor White
    Write-Host "  Web UI    : http://127.0.0.1:47101/" -ForegroundColor White
    Write-Host "  SSH GW    : 127.0.0.1:47102" -ForegroundColor White
    Write-Host "  DB GW     : 127.0.0.1:33060" -ForegroundColor White
    if ($realToken) {
        Write-Host "  Token     : $realToken" -ForegroundColor Gray
    } else {
        Write-Host "  Token     : (login with admin/admin)" -ForegroundColor Gray
    }
    Write-Host ""
    Write-Host "Logs:" -ForegroundColor Gray
    Write-Host "  logs\backend.log" -ForegroundColor Gray
    Write-Host "  logs\backend.err.log" -ForegroundColor Gray
    Write-Host "  logs\frontend.log" -ForegroundColor Gray
    Write-Host "  logs\frontend.err.log" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Stop with:" -ForegroundColor Gray
    Write-Host "  Stop-Process -Id (Get-Content logs\backend.pid) -Force" -ForegroundColor Gray
    Write-Host "  Stop-Process -Id (Get-Content logs\frontend.pid) -Force" -ForegroundColor Gray
} catch {
    Fail-WithDiagnostics $_.Exception.Message
}
