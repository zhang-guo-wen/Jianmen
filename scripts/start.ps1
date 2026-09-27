# Jianmen startup / restart
# Usage:
#   .\scripts\start.ps1                       Windows local mode (default, no Web RDP)
#   .\scripts\start.ps1 -Mode Windows         Windows local mode (no Web RDP)
#   .\scripts\start.ps1 -Mode WSL             WSL Docker container mode (with Web RDP)
#   .\scripts\start.ps1 -Mode Windows -SkipBuild
#   .\scripts\start.ps1 -Mode WSL -SkipBuild
#   .\scripts\start.ps1 -Mode Windows -Stop
#   .\scripts\start.ps1 -Mode WSL -Stop

param(
    [ValidateSet("Windows", "WSL")]
    [string]$Mode = "Windows",
    [switch]$SkipBuild,
    [switch]$Stop
)

$ErrorActionPreference = "Stop"
$script:DockerPrefix = @()
$script:DockerDistribution = ""

function Write-Step($Message) {
    Write-Host $Message -ForegroundColor Yellow
}

function Write-Ok($Message) {
    Write-Host "  $Message" -ForegroundColor Green
}

function Write-Info($Message) {
    Write-Host "  $Message" -ForegroundColor Gray
}

function Resolve-WslDockerCommand([switch]$AllowMissing) {
    $script:DockerPrefix = @()
    $script:DockerDistribution = ""
    $wsl = Get-Command "wsl.exe" -ErrorAction SilentlyContinue
    if (-not $wsl) {
        if ($AllowMissing) {
            return $null
        }
        throw "WSL is not installed. Install WSL and Docker Engine in a WSL distribution"
    }

    $distributions = & $wsl.Source --list --quiet 2>$null
    foreach ($distribution in $distributions) {
        $name = ([string]$distribution).Replace([string][char]0, "").Trim()
        if ([string]::IsNullOrWhiteSpace($name)) {
            continue
        }
        & $wsl.Source -d $name -e docker info --format "{{.ServerVersion}}" *> $null
        if ($LASTEXITCODE -eq 0) {
            $script:DockerPrefix = @("-d", $name, "-e", "docker")
            $script:DockerDistribution = $name
            Write-Info "Using Docker Engine from WSL distribution $name"
            return $wsl.Source
        }
    }

    if ($AllowMissing) {
        return $null
    }
    throw "No working Docker Engine was found in WSL"
}

function Convert-ToDockerPath($Path) {
    if ([string]::IsNullOrWhiteSpace($script:DockerDistribution)) {
        throw "WSL Docker distribution is not selected"
    }
    $converted = (& wsl.exe -d $script:DockerDistribution -e wslpath -u $Path).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($converted)) {
        throw "Unable to convert Windows path for WSL Docker: $Path"
    }
    return $converted
}

function Invoke-Docker($Docker, [string[]]$Arguments, [switch]$AllowFailure) {
    $prefix = $script:DockerPrefix
    & $Docker @prefix @Arguments
    $exitCode = $LASTEXITCODE
    if (-not $AllowFailure -and $exitCode -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $exitCode"
    }
    return $exitCode
}

function Invoke-DockerWithRetry($Docker, [string[]]$Arguments, [int]$Attempts = 3) {
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        $prefix = $script:DockerPrefix
        & $Docker @prefix @Arguments
        if ($LASTEXITCODE -eq 0) {
            return
        }
        if ($attempt -eq $Attempts) {
            throw "docker $($Arguments -join ' ') failed after $Attempts attempts"
        }
        Write-Info "Docker command failed; retrying ($attempt/$Attempts)..."
        Start-Sleep -Seconds 3
    }
}

function Get-ProcessCommandLine($ProcessId) {
    $process = Get-CimInstance Win32_Process -Filter "ProcessId = $ProcessId" -ErrorAction SilentlyContinue
    if ($process) {
        return [string]$process.CommandLine
    }
    return ""
}

function Test-JianmenLocalProcess($ProcessId, $Root) {
    $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    if (-not $process) {
        return $false
    }
    if ($process.ProcessName -like "jianmen*") {
        return [string]$process.Path -like "$Root*"
    }
    if ($process.ProcessName -notin @("node", "cmd")) {
        return $false
    }
    $commandLine = Get-ProcessCommandLine $ProcessId
    return $commandLine -like "*$Root*" -and (
        $commandLine -like "*vite*" -or $commandLine -like "*npm*run*dev*"
    )
}

function Stop-JianmenLocalProcess($ProcessId, $Name, $Root) {
    if (Test-JianmenLocalProcess $ProcessId $Root) {
        Write-Info "Stopping local $Name PID $ProcessId"
        Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
    }
}

function Stop-JianmenLocalServices($Root) {
    foreach ($pidFile in @("logs\backend.pid", "logs\frontend.pid")) {
        $path = Join-Path $Root $pidFile
        if (-not (Test-Path -LiteralPath $path)) {
            continue
        }
        $rawPid = Get-Content -LiteralPath $path -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($rawPid -and $rawPid -match "^\d+$") {
            Stop-JianmenLocalProcess ([int]$rawPid) $pidFile $Root
        }
        Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
    }

    foreach ($port in @(47100, 47101, 47102, 33060, 33061, 33062, 33063)) {
        $listeners = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
        foreach ($listener in $listeners) {
            Stop-JianmenLocalProcess $listener.OwningProcess "service on port $port" $Root
        }
    }
    Start-Sleep -Milliseconds 500
}

function Remove-JianmenContainers($Docker) {
    foreach ($oldContainer in @("jianmen", "jianmen-jianmen-1", "jianmen-volume-init-1")) {
        $prefix = $script:DockerPrefix
        $oldContainerId = [string](& $Docker @prefix ps --all --quiet --filter "name=^/$oldContainer$")
        if (-not [string]::IsNullOrWhiteSpace($oldContainerId)) {
            Invoke-Docker $Docker @("rm", "-f", $oldContainer) | Out-Null
            Write-Info "Removed previous container $oldContainer"
        }
    }
}

function Stop-WslContainerIfAvailable {
    $docker = Resolve-WslDockerCommand -AllowMissing
    if ($docker) {
        Remove-JianmenContainers $docker
    }
}

function Assert-LocalPortsAvailable {
    foreach ($port in @(47100, 47102, 33060)) {
        $listener = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($listener) {
            $process = Get-Process -Id $listener.OwningProcess -ErrorAction SilentlyContinue
            $processName = if ($process) { $process.ProcessName } else { "PID $($listener.OwningProcess)" }
            throw "Port $port is already used by $processName"
        }
    }
}

function Wait-JianmenLocalProcess($Process, $TimeoutSeconds) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $Process.Refresh()
        if ($Process.HasExited) {
            throw "Windows local process exited with code $($Process.ExitCode)"
        }
        try {
            $response = Invoke-WebRequest -Uri "http://127.0.0.1:47100/api/init/status" -UseBasicParsing -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                Write-Ok "Windows local service is healthy"
                return
            }
        } catch {
            Start-Sleep -Milliseconds 500
        }
    }
    throw "Windows local service did not become healthy after ${TimeoutSeconds}s"
}

function Wait-JianmenContainer($Docker, $ContainerName, $TimeoutSeconds) {
    $prefix = $script:DockerPrefix
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastState = "unknown"
    while ((Get-Date) -lt $deadline) {
        $lastState = (& $Docker @prefix inspect --format "{{.State.Status}}/{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}" $ContainerName 2>$null).Trim()
        if ($LASTEXITCODE -ne 0) {
            $lastState = "inspect-failed"
        } elseif ($lastState -eq "running/healthy") {
            Write-Ok "Container healthy: $ContainerName"
            return
        } elseif ($lastState -like "exited/*" -or $lastState -like "dead/*") {
            throw "Jianmen container stopped unexpectedly: $lastState"
        }
        Start-Sleep -Seconds 1
    }
    throw "Jianmen container not healthy after ${TimeoutSeconds}s: $lastState"
}

function Show-LocalDiagnostics($Root) {
    foreach ($name in @("backend.error.log", "backend.log")) {
        $path = Join-Path $Root "logs\$name"
        if (Test-Path -LiteralPath $path) {
            Write-Host "--- $name (last 80 lines) ---" -ForegroundColor DarkGray
            Get-Content -LiteralPath $path -Tail 80 -ErrorAction SilentlyContinue
        }
    }
}

function Show-ContainerDiagnostics($Docker, $ContainerName) {
    if (-not $Docker) {
        return
    }
    $prefix = $script:DockerPrefix
    Write-Host ""
    Write-Host "--- Docker container status ---" -ForegroundColor DarkGray
    & $Docker @prefix ps --all --filter "name=^/$ContainerName$"
    $containerId = [string](& $Docker @prefix ps --all --quiet --filter "name=^/$ContainerName$")
    if (-not [string]::IsNullOrWhiteSpace($containerId)) {
        Write-Host "--- Jianmen container logs (last 120 lines) ---" -ForegroundColor DarkGray
        & $Docker @prefix logs --tail 120 $ContainerName
    }
}

function Start-WindowsLocal($Root, [switch]$ReuseBuild) {
    $binary = Join-Path $Root "dist\jianmen-windows-amd64.exe"
    $configFile = Join-Path $Root "configs\config.example.json"
    $logsDirectory = Join-Path $Root "logs"
    $stdoutLog = Join-Path $logsDirectory "backend.log"
    $stderrLog = Join-Path $logsDirectory "backend.error.log"
    $pidFile = Join-Path $logsDirectory "backend.pid"

    Write-Host "=== Jianmen Windows Local Startup (Web RDP disabled) ===" -ForegroundColor Cyan
    Write-Step "[1/5] Stopping previous Jianmen runtimes..."
    Stop-JianmenLocalServices $Root
    Stop-WslContainerIfAvailable
    Assert-LocalPortsAvailable
    Write-Ok "Required local ports are available"

    if ($ReuseBuild) {
        Write-Step "[2/5] Reusing Windows binary..."
        if (-not (Test-Path -LiteralPath $binary)) {
            throw "Windows binary not found: $binary. Run without -SkipBuild first"
        }
        Write-Ok "Build skipped"
    } else {
        Write-Step "[2/5] Building Windows local artifacts..."
        & (Join-Path $Root "scripts\build\build.ps1") -WindowsOnly
        if (-not (Test-Path -LiteralPath $binary)) {
            throw "Windows build did not produce $binary"
        }
        Write-Ok "Windows frontend and binary built"
    }

    Write-Step "[3/5] Validating local configuration..."
    $localConfig = Get-Content -LiteralPath $configFile -Raw | ConvertFrom-Json
    if ($localConfig.web_rdp.enabled -or $localConfig.web_rdp.managed_guacd.enabled) {
        throw "Windows local config must keep web_rdp and managed_guacd disabled"
    }
    New-Item -ItemType Directory -Force -Path $logsDirectory | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $Root "data") | Out-Null
    Remove-Item -LiteralPath $stdoutLog, $stderrLog -Force -ErrorAction SilentlyContinue
    Write-Ok "Local configuration uses embedded Web UI without guacd"

    Write-Step "[4/5] Starting Windows local process..."
    $startArguments = @{
        FilePath               = $binary
        ArgumentList           = @("-config", "`"$configFile`"", "-disable-web-rdp")
        WorkingDirectory       = $Root
        RedirectStandardOutput = $stdoutLog
        RedirectStandardError  = $stderrLog
        WindowStyle            = "Hidden"
        PassThru               = $true
    }
    $process = Start-Process @startArguments
    Set-Content -LiteralPath $pidFile -Value $process.Id -Encoding ASCII
    Write-Ok "Started PID $($process.Id)"

    Write-Step "[5/5] Verifying Windows local service..."
    try {
        Wait-JianmenLocalProcess $process 60
    } catch {
        Stop-JianmenLocalProcess $process.Id "failed Windows local service" $Root
        throw
    }

    Write-Host ""
    Write-Host "=== Jianmen Windows local service started ===" -ForegroundColor Cyan
    Write-Host "  Web UI / API : http://127.0.0.1:47100/" -ForegroundColor White
    Write-Host "  SSH / SFTP   : 127.0.0.1:47102" -ForegroundColor White
    Write-Host "  DB gateway   : 127.0.0.1:33060" -ForegroundColor White
    Write-Host "  Web RDP      : disabled (guacd is not started)" -ForegroundColor White
    Write-Host "  Logs         : .\logs\backend.log" -ForegroundColor Gray
    Write-Host "  Restart      : .\scripts\start.ps1 -Mode Windows -SkipBuild" -ForegroundColor Gray
}

function Start-WslContainer($Root, [switch]$ReuseBuild) {
    $containerName = "jianmen"
    $imageName = "jianmen:guacd-1.6.0"
    $dataVolume = "jianmen-data"
    $configFile = Join-Path $Root "configs\config.docker.local.json"
    $docker = $null

    try {
        Write-Host "=== Jianmen WSL Container Startup (Web RDP enabled) ===" -ForegroundColor Cyan
        Write-Step "[1/5] Stopping Windows local services..."
        Stop-JianmenLocalServices $Root
        Write-Ok "No Jianmen Windows process is running"

        Write-Step "[2/5] Checking WSL Docker..."
        $docker = Resolve-WslDockerCommand
        Invoke-Docker $docker @("info", "--format", "{{.ServerVersion}}") | Out-Null
        Write-Ok "WSL Docker Engine is ready"

        if ($ReuseBuild) {
            Write-Step "[3/5] Reusing existing container image..."
            Write-Ok "Artifact and image build skipped"
        } else {
            Write-Step "[3/5] Building Jianmen artifacts and container image..."
            & (Join-Path $Root "scripts\build\build.ps1")
            $dockerRoot = Convert-ToDockerPath $Root
            $dockerfile = Convert-ToDockerPath (Join-Path $Root "deploy\docker\Dockerfile")
            Invoke-DockerWithRetry $docker @(
                "build", "--platform", "linux/amd64",
                "--build-arg", "GUACD_IMAGE=guacamole/guacd:1.6.0@sha256:8974eaa9ba32f713daf311e7cc8cd7e4cdfba1edea39eed75524e78ef4b08f4f",
                "-f", $dockerfile,
                "-t", $imageName, $dockerRoot
            )
            Write-Ok "Container image built: $imageName"
        }

        Write-Step "[4/5] Preparing WSL container runtime..."
        $dockerConfigFile = Convert-ToDockerPath $configFile
        Remove-JianmenContainers $docker
        Invoke-Docker $docker @("volume", "create", $dataVolume) | Out-Null
        Write-Ok "Persistent volume and previous containers are ready"

        Write-Step "[5/5] Starting Jianmen in WSL Docker..."
        $runArguments = @(
            "run", "-d",
            "--name", $containerName,
            "--restart", "unless-stopped",
            "--platform", "linux/amd64",
            "-p", "127.0.0.1:47100:47100",
            "-p", "47102:47102",
            "-p", "33060:33060",
            "-p", "47110-47199:47110-47199",
            "-v", "${dataVolume}:/app/data",
            "-v", "${dockerConfigFile}:/app/config.json:ro",
            $imageName
        )
        Invoke-Docker $docker $runArguments | Out-Null
        Wait-JianmenContainer $docker $containerName 90

        Write-Host ""
        $dockerPrefix = $script:DockerPrefix
        & $docker @dockerPrefix ps --filter "name=^/$containerName$"
        Write-Host ""
        Write-Host "=== Jianmen WSL container started ===" -ForegroundColor Cyan
        Write-Host "  Web UI / API : http://127.0.0.1:47100/" -ForegroundColor White
        Write-Host "  SSH / SFTP   : 127.0.0.1:47102" -ForegroundColor White
        Write-Host "  DB gateway   : 127.0.0.1:33060" -ForegroundColor White
        Write-Host "  App proxies  : 127.0.0.1:47110-47199" -ForegroundColor White
        Write-Host "  Web RDP      : enabled through managed guacd" -ForegroundColor White
        Write-Host "  Data volume  : $dataVolume" -ForegroundColor White
        Write-Host "  Restart      : .\scripts\start.ps1 -Mode WSL -SkipBuild" -ForegroundColor Gray
        Write-Host "  Logs         : wsl docker logs -f $containerName" -ForegroundColor Gray
    } catch {
        Write-Host ""
        Write-Host "WSL container startup failed: $($_.Exception.Message)" -ForegroundColor Red
        Show-ContainerDiagnostics $docker $containerName
        throw
    }
}

$root = Split-Path -Parent $PSScriptRoot

try {
    if ($Stop -and $Mode -eq "Windows") {
        Stop-JianmenLocalServices $root
        Write-Ok "Windows local service stopped"
    } elseif ($Stop) {
        $docker = Resolve-WslDockerCommand
        Remove-JianmenContainers $docker
        Write-Ok "WSL container stopped"
    } elseif ($Mode -eq "Windows") {
        Start-WindowsLocal $root -ReuseBuild:$SkipBuild
    } else {
        Start-WslContainer $root -ReuseBuild:$SkipBuild
    }
} catch {
    if ($Mode -eq "Windows") {
        Write-Host ""
        Write-Host "Windows local startup failed: $($_.Exception.Message)" -ForegroundColor Red
        Show-LocalDiagnostics $root
    }
    exit 1
}
