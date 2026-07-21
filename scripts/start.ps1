# Jianmen container startup / restart
# Usage:
#   .\scripts\start.ps1             Build artifacts and image, then recreate with local HTTP
#   .\scripts\start.ps1 -SkipBuild  Recreate the container from the existing image

param(
    [switch]$SkipBuild
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

function Resolve-DockerCommand {
    $command = Get-Command "docker.exe" -ErrorAction SilentlyContinue
    if (-not $command) {
        $command = Get-Command "docker" -ErrorAction SilentlyContinue
    }
    if ($command) {
        return $command.Source
    }

    $candidates = @(
        "C:\Program Files\Docker\Docker\resources\bin\docker.exe",
        "C:\ProgramData\DockerDesktop\version-bin\docker.exe"
    )
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate) {
            return $candidate
        }
    }

    $wsl = Get-Command "wsl.exe" -ErrorAction SilentlyContinue
    if ($wsl) {
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
    }

    throw "Docker CLI not found. Install Docker Desktop or Docker Engine in WSL, then run .\scripts\start.ps1 again"
}

function Convert-ToDockerPath($Path) {
    if ([string]::IsNullOrWhiteSpace($script:DockerDistribution)) {
        return $Path
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
    }

    foreach ($port in @(47100, 47101, 47102, 33060, 33061, 33062, 33063)) {
        $listeners = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
        foreach ($listener in $listeners) {
            Stop-JianmenLocalProcess $listener.OwningProcess "service on port $port" $Root
        }
    }
    Start-Sleep -Milliseconds 500
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
            Write-Ok "container healthy: $ContainerName"
            return
        } elseif ($lastState -like "exited/*" -or $lastState -like "dead/*") {
            throw "Jianmen container stopped unexpectedly: $lastState"
        }
        Start-Sleep -Seconds 1
    }
    throw "Jianmen container not healthy after ${TimeoutSeconds}s: $lastState"
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

$root = Split-Path -Parent $PSScriptRoot
$containerName = "jianmen"
$imageName = "jianmen:guacd-1.6.0"
$docker = $null
$webURL = "http://127.0.0.1:47100/"

try {
    Write-Host "=== Jianmen Container Startup ===" -ForegroundColor Cyan
    if ($SkipBuild) {
        Write-Host "(reuse existing artifacts and image)" -ForegroundColor DarkGray
    }
    $configFile = Join-Path $root "configs\config.docker.local.json"
    Write-Host ""

    Write-Step "[1/5] Stopping legacy local services..."
    Stop-JianmenLocalServices $root
    Write-Ok "no local Go or Vite service is running"

    Write-Step "[2/5] Checking Docker..."
    $docker = Resolve-DockerCommand
    Invoke-Docker $docker @("info", "--format", "{{.ServerVersion}}") | Out-Null
    Write-Ok "Docker engine is ready"

    if ($SkipBuild) {
        Write-Step "[3/5] Reusing existing container image..."
        Write-Ok "artifact and image build skipped"
    } else {
        Write-Step "[3/5] Building Jianmen artifacts..."
        & (Join-Path $root "scripts\build\build.ps1")
        if ($LASTEXITCODE -ne 0) {
            throw "Jianmen artifact build failed"
        }
        Write-Ok "frontend and Linux container binary built"

        $dockerRoot = Convert-ToDockerPath $root
        $dockerfile = Convert-ToDockerPath (Join-Path $root "deploy\docker\Dockerfile")
        Invoke-Docker $docker @(
            "build", "--platform", "linux/amd64",
            "--build-arg", "GUACD_IMAGE=guacamole/guacd:1.6.0@sha256:8974eaa9ba32f713daf311e7cc8cd7e4cdfba1edea39eed75524e78ef4b08f4f",
            "-f", $dockerfile,
            "-t", $imageName, $dockerRoot
        ) | Out-Null
        Write-Ok "container image built: $imageName"
    }

    Write-Step "[4/5] Preparing container runtime..."
    $dataDirectory = Join-Path $root "data"
    foreach ($directory in @(
        $dataDirectory,
        (Join-Path $dataDirectory "rdp-spool"),
        (Join-Path $dataDirectory "rdp-drive")
    )) {
        New-Item -ItemType Directory -Force -Path $directory | Out-Null
    }
    $dockerDataDirectory = Convert-ToDockerPath $dataDirectory
    $dockerConfigFile = Convert-ToDockerPath $configFile

    foreach ($oldContainer in @($containerName, "jianmen-jianmen-1", "jianmen-volume-init-1")) {
        $prefix = $script:DockerPrefix
        $oldContainerId = [string](& $docker @prefix ps --all --quiet --filter "name=^/$oldContainer$")
        if (-not [string]::IsNullOrWhiteSpace($oldContainerId)) {
            Invoke-Docker $docker @("rm", "-f", $oldContainer) | Out-Null
            Write-Info "removed previous container $oldContainer"
        }
    }
    Write-Ok "runtime directories and previous containers are ready"

    Write-Step "[5/5] Starting Jianmen with docker run..."
    $runArguments = @(
        "run", "-d",
        "--name", $containerName,
        "--restart", "unless-stopped",
        "--platform", "linux/amd64",
        "-p", "127.0.0.1:47100:47100",
        "-p", "47102:47102",
        "-p", "33060:33060",
        "-p", "47110-47199:47110-47199",
        "-v", "${dockerDataDirectory}:/app/data",
        "-v", "${dockerConfigFile}:/app/config.json:ro",
        $imageName
    )
    Invoke-Docker $docker $runArguments | Out-Null
    Wait-JianmenContainer $docker $containerName 90

    Write-Host ""
    $dockerPrefix = $script:DockerPrefix
    & $docker @dockerPrefix ps --filter "name=^/$containerName$"
    Write-Host ""
    Write-Host "=== Jianmen container started and verified ===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Web UI / API : $webURL" -ForegroundColor White
    Write-Host "  SSH / SFTP   : 127.0.0.1:47102" -ForegroundColor White
    Write-Host "  DB gateway   : 127.0.0.1:33060" -ForegroundColor White
    Write-Host "  App proxies  : 127.0.0.1:47110-47199" -ForegroundColor White
    Write-Host ""
    Write-Host "The local development backend and Vite server are no longer used." -ForegroundColor Gray
    Write-Host "Future starts: .\scripts\start.ps1" -ForegroundColor Gray
    Write-Host "Quick container restart: .\scripts\start.ps1 -SkipBuild" -ForegroundColor Gray
    Write-Host "Production HTTPS should terminate at the reverse proxy." -ForegroundColor Gray
    Write-Host "Container logs: docker logs -f $containerName" -ForegroundColor Gray
} catch {
    Write-Host ""
    Write-Host "Container startup failed: $($_.Exception.Message)" -ForegroundColor Red
    Show-ContainerDiagnostics $docker $containerName
    exit 1
}
