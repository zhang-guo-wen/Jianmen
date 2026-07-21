# Jianmen container startup / restart
# Usage:
#   .\start.ps1             Build artifacts and image, then recreate with local HTTP
#   .\start.ps1 -SkipBuild  Recreate the container from the existing image
#   .\start.ps1 -EnableTLS  Use the certificate-backed HTTPS configuration

param(
    [switch]$SkipBuild,
    [switch]$EnableTLS
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
                $wslEnvironment = @([string]$env:WSLENV -split ":" | Where-Object { $_ })
                if ($wslEnvironment -notcontains "JIANMEN_CONFIG_FILE") {
                    $env:WSLENV = (@($wslEnvironment) + "JIANMEN_CONFIG_FILE") -join ":"
                }
                Write-Info "Using Docker Engine from WSL distribution $name"
                return $wsl.Source
            }
        }
    }

    throw "Docker CLI not found. Install Docker Desktop or Docker Engine in WSL, then run .\start.ps1 again"
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
    if ($process.ProcessName -eq "bastion-core") {
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

function Initialize-CertificateVolume($Docker) {
    $prefix = $script:DockerPrefix
    & $Docker @prefix volume inspect jianmen-certs *> $null
    $inspectExitCode = $LASTEXITCODE
    if ($inspectExitCode -ne 0) {
        Invoke-Docker $Docker @("volume", "create", "jianmen-certs") | Out-Null
        Write-Ok "created certificate volume jianmen-certs"
    }

    $certificateScript = @'
set -eu
if [ -s /certs/admin.key ] && [ -s /certs/admin.crt ] && \
   [ -s /certs/database.key ] && [ -s /certs/database.crt ] && \
   [ -s /certs/database-ca.crt ]; then
  exit 0
fi
apk add --no-cache openssl >/dev/null
rm -f /certs/admin.key /certs/admin.crt /certs/database.key \
  /certs/database.crt /certs/database-ca.crt /certs/database-ca.srl
openssl req -x509 -newkey rsa:3072 -nodes -days 3650 \
  -keyout /certs/admin.key -out /certs/admin.crt \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" >/dev/null 2>&1
openssl req -x509 -newkey rsa:3072 -nodes -days 3650 \
  -keyout /tmp/database-ca.key -out /certs/database-ca.crt \
  -subj "/CN=Jianmen local database CA" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" >/dev/null 2>&1
openssl req -new -newkey rsa:3072 -nodes \
  -keyout /certs/database.key -out /tmp/database.csr \
  -subj "/CN=localhost" >/dev/null 2>&1
printf '%s\n' \
  'basicConstraints=critical,CA:FALSE' \
  'keyUsage=critical,digitalSignature,keyEncipherment' \
  'extendedKeyUsage=serverAuth' \
  'subjectAltName=DNS:localhost,IP:127.0.0.1' >/tmp/database.ext
openssl x509 -req -in /tmp/database.csr \
  -CA /certs/database-ca.crt -CAkey /tmp/database-ca.key -CAcreateserial \
  -out /certs/database.crt -days 3650 -sha256 -extfile /tmp/database.ext >/dev/null 2>&1
rm -f /certs/database-ca.srl /tmp/database-ca.key /tmp/database.csr /tmp/database.ext
chown 10001:10001 /certs/admin.key /certs/admin.crt /certs/database.key \
  /certs/database.crt /certs/database-ca.crt
chmod 600 /certs/admin.key /certs/database.key
chmod 644 /certs/admin.crt /certs/database.crt /certs/database-ca.crt
'@

    Invoke-Docker $Docker @(
        "run", "--rm", "--user", "0",
        "-v", "jianmen-certs:/certs",
        "alpine:3.23", "sh", "-ec", $certificateScript
    ) | Out-Null
    Write-Ok "certificate volume ready"
}

function Wait-JianmenContainer($Docker, $ComposeFile, $LocalComposeFile, $TimeoutSeconds) {
    $prefix = $script:DockerPrefix
    $containerId = (& $Docker @prefix compose -f $ComposeFile -f $LocalComposeFile ps -q jianmen).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($containerId)) {
        throw "Jianmen container was not created"
    }

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastState = "unknown"
    while ((Get-Date) -lt $deadline) {
        $lastState = (& $Docker @prefix inspect --format "{{.State.Status}}/{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}" $containerId).Trim()
        if ($LASTEXITCODE -ne 0) {
            $lastState = "inspect-failed"
        } elseif ($lastState -eq "running/healthy") {
            Write-Ok "container healthy: $containerId"
            return
        } elseif ($lastState -like "exited/*" -or $lastState -like "dead/*") {
            throw "Jianmen container stopped unexpectedly: $lastState"
        }
        Start-Sleep -Seconds 1
    }
    throw "Jianmen container not healthy after ${TimeoutSeconds}s: $lastState"
}

function Show-ContainerDiagnostics($Docker, $ComposeFile, $LocalComposeFile) {
    if (-not $Docker) {
        return
    }
    $prefix = $script:DockerPrefix
    Write-Host ""
    Write-Host "--- Docker Compose status ---" -ForegroundColor DarkGray
    & $Docker @prefix compose -f $ComposeFile -f $LocalComposeFile ps --all
    Write-Host "--- Jianmen container logs (last 120 lines) ---" -ForegroundColor DarkGray
    & $Docker @prefix compose -f $ComposeFile -f $LocalComposeFile logs --tail 120 jianmen
}

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$composeFile = Join-Path $root "docker-compose.web-rdp.yml"
$localComposeFile = Join-Path $root "docker-compose.local.yml"
$docker = $null
$webURL = "http://127.0.0.1:47100/"

try {
    Write-Host "=== Jianmen Container Startup ===" -ForegroundColor Cyan
    if ($SkipBuild) {
        Write-Host "(reuse existing artifacts and image)" -ForegroundColor DarkGray
    }
    if ($EnableTLS) {
        $env:JIANMEN_CONFIG_FILE = "./config.docker.web-rdp.example.json"
        $webURL = "https://127.0.0.1:47100/"
        Write-Host "(HTTPS configuration enabled)" -ForegroundColor DarkGray
    } else {
        $env:JIANMEN_CONFIG_FILE = "./config.docker.local.json"
    }
    Write-Host ""

    Write-Step "[1/5] Stopping legacy local services..."
    Stop-JianmenLocalServices $root
    Write-Ok "no local Go or Vite service is running"

    Write-Step "[2/5] Checking Docker..."
    $docker = Resolve-DockerCommand
    Invoke-Docker $docker @("info", "--format", "{{.ServerVersion}}") | Out-Null
    Invoke-Docker $docker @("compose", "version") | Out-Null
    $composeFile = Convert-ToDockerPath $composeFile
    $localComposeFile = Convert-ToDockerPath $localComposeFile
    Write-Ok "Docker engine and Compose are ready"

    if ($SkipBuild) {
        Write-Step "[3/5] Reusing existing container image..."
        Write-Ok "artifact and image build skipped"
    } else {
        Write-Step "[3/5] Building Jianmen artifacts..."
        & (Join-Path $root "build.ps1")
        if ($LASTEXITCODE -ne 0) {
            throw "Jianmen artifact build failed"
        }
        Write-Ok "frontend and Linux container binary built"
    }

    Write-Step "[4/5] Preparing container runtime..."
    Initialize-CertificateVolume $docker

    Write-Step "[5/5] Recreating Jianmen container..."
    $composeArguments = @(
        "compose", "-f", $composeFile, "-f", $localComposeFile,
        "up", "-d", "--force-recreate", "--remove-orphans"
    )
    if ($SkipBuild) {
        $composeArguments += "--no-build"
    } else {
        $composeArguments += "--build"
    }
    Invoke-Docker $docker $composeArguments | Out-Null
    Wait-JianmenContainer $docker $composeFile $localComposeFile 90

    Write-Host ""
    $dockerPrefix = $script:DockerPrefix
    & $docker @dockerPrefix compose -f $composeFile -f $localComposeFile ps
    Write-Host ""
    Write-Host "=== Jianmen container started and verified ===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Web UI / API : $webURL" -ForegroundColor White
    Write-Host "  SSH / SFTP   : 127.0.0.1:47102" -ForegroundColor White
    Write-Host "  DB gateway   : 127.0.0.1:33060" -ForegroundColor White
    Write-Host "  App proxies  : 127.0.0.1:47110-47199" -ForegroundColor White
    Write-Host ""
    Write-Host "The local development backend and Vite server are no longer used." -ForegroundColor Gray
    Write-Host "Future starts: .\start.ps1" -ForegroundColor Gray
    Write-Host "Quick container restart: .\start.ps1 -SkipBuild" -ForegroundColor Gray
    Write-Host "HTTPS container start: .\start.ps1 -EnableTLS" -ForegroundColor Gray
    Write-Host "Container logs: docker compose -f docker-compose.web-rdp.yml -f docker-compose.local.yml logs -f jianmen" -ForegroundColor Gray
} catch {
    Write-Host ""
    Write-Host "Container startup failed: $($_.Exception.Message)" -ForegroundColor Red
    Show-ContainerDiagnostics $docker $composeFile $localComposeFile
    exit 1
}
