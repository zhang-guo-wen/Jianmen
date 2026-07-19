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

function Wait-HttpOk($Name, $Url, $TimeoutSeconds, $Headers, $WebSession) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = $null
    while ((Get-Date) -lt $deadline) {
        try {
            if ($Headers) {
                $response = Invoke-WebRequest $Url -UseBasicParsing -TimeoutSec 2 -Headers $Headers -WebSession $WebSession
            } else {
                $response = Invoke-WebRequest $Url -UseBasicParsing -TimeoutSec 2 -WebSession $WebSession
            }
            if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 300) {
                Write-Ok "$Name ready: $Url ($($response.StatusCode))"
                return
            }
        } catch {
            $lastError = $_.Exception.Message
            if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
                $statusCode = [int]$_.Exception.Response.StatusCode
                if ($statusCode -ge 200 -and $statusCode -lt 300) {
                    Write-Ok "$Name ready: $Url ($statusCode)"
                    return
                }
            }
        }
        Start-Sleep -Milliseconds 500
    }
    throw "$Name not ready after ${TimeoutSeconds}s: $Url; last error: $lastError"
}

function New-LoginCaptchaPayload($Challenge) {
    foreach ($property in @('algorithm', 'challenge', 'maxNumber', 'salt', 'signature')) {
        if ($null -eq $Challenge.$property -or [string]::IsNullOrWhiteSpace([string]$Challenge.$property)) {
            throw "Login challenge is missing $property"
        }
    }

    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    try {
        $solutionNumber = -1
        for ($number = 0; $number -le [int64]$Challenge.maxNumber; $number++) {
            $bytes = [System.Text.Encoding]::UTF8.GetBytes("$($Challenge.salt)$number")
            $hash = $sha256.ComputeHash($bytes)
            $hex = ([System.BitConverter]::ToString($hash)).Replace('-', '').ToLowerInvariant()
            if ($hex -eq [string]$Challenge.challenge) {
                $solutionNumber = $number
                break
            }
        }
    } finally {
        $sha256.Dispose()
    }

    if ($solutionNumber -lt 0) {
        throw "Unable to solve login challenge"
    }

    $payload = [ordered]@{
        algorithm = [string]$Challenge.algorithm
        challenge = [string]$Challenge.challenge
        number    = [int64]$solutionNumber
        salt      = [string]$Challenge.salt
        signature = [string]$Challenge.signature
    } | ConvertTo-Json -Compress
    return [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($payload))
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

    # Wait for TCP ports first
    Wait-TcpPort "Admin API port" "127.0.0.1" 47100 20
    Wait-TcpPort "Database gateway" "127.0.0.1" 33060 10
    Wait-TcpPort "SSH gateway" "127.0.0.1" 47102 10

    # Login through the production challenge flow and retain the HttpOnly session cookie.
    $webSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $challenge = Invoke-RestMethod -Uri "http://127.0.0.1:47100/api/login/challenge" -Method Get -WebSession $webSession -TimeoutSec 5 -ErrorAction Stop
    $captchaPayload = New-LoginCaptchaPayload $challenge
    $loginBody = [ordered]@{
        username       = "admin"
        password       = "admin"
        captcha_payload = $captchaPayload
    } | ConvertTo-Json -Compress
    $loginResp = Invoke-RestMethod -Uri "http://127.0.0.1:47100/api/login" -Method Post -Body $loginBody -ContentType "application/json" -WebSession $webSession -TimeoutSec 5 -ErrorAction Stop
    if ([string]::IsNullOrWhiteSpace([string]$loginResp.csrf_token)) {
        throw "Login response did not include csrf_token"
    }
    $sessionCookie = $webSession.Cookies.GetCookies("http://127.0.0.1:47100/") | Where-Object { $_.Name -eq "jianmen_session" }
    if (-not $sessionCookie) {
        throw "Login response did not establish a browser session"
    }
    Write-Ok "Admin login OK (browser session established)"

    # Health check with real token
    Wait-HttpOk "Admin API" "http://127.0.0.1:47100/api/health" 5 $null $webSession

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

    # Verify API proxy with the authenticated browser session.
    Wait-HttpOk "Frontend API proxy" "http://127.0.0.1:47101/api/hosts" 15 $null $webSession

    Write-Host ""
    Write-Host "=== All services started and verified ===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Admin API : http://127.0.0.1:47100/" -ForegroundColor White
    Write-Host "  Web UI    : http://127.0.0.1:47101/" -ForegroundColor White
    Write-Host "  SSH GW    : 127.0.0.1:47102" -ForegroundColor White
    Write-Host "  DB GW     : 127.0.0.1:33060" -ForegroundColor White
    Write-Host "  Auth      : browser session established" -ForegroundColor Gray
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
