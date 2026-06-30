# AI 友好一键启动脚本 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将现有 `start.ps1` 改造成 AI 和人工都能一键启动、自动验证、失败时直接输出诊断信息的可靠入口。

**Architecture:** 继续保留单入口 `.start.ps1`（实际路径为 `start.ps1`），不新增第二套启动脚本。脚本使用 `Start-Process` 而不是 `Start-Job` 启动后端和前端，把日志、错误日志和 PID 写入 `logs/`，并通过 HTTP/端口等待函数验证服务真正可用。

**Tech Stack:** PowerShell 5.1、Go backend (`cmd/bastion-core`)、Vue/Vite frontend (`web`)、Windows 本地进程、`Invoke-WebRequest`、`Test-NetConnection`。

## Global Constraints

- 回答和脚本输出优先面向中文使用者，但保留服务名、URL、命令等英文/技术文本。
- 保留现有一键入口：用户和 AI 仍运行 `.start.ps1`（实际路径为 `start.ps1`）。
- 脚本必须在 Windows PowerShell 5.1 下可运行，不使用 PowerShell 7 专属语法。
- 脚本失败时必须以非 0 退出，并打印最近日志内容，便于 AI 判断失败原因。
- 不引入新的 npm/go 依赖。

---

## File Structure

- Modify: `start.ps1`
  - 负责清理旧进程、准备目录、构建后端、启动后端、启动前端、等待健康检查、输出诊断。
- No new production files.
- No persistent test file needed; verification is performed by running `.start.ps1` and checking exit code/output because this is a development bootstrap script.

### Task 1: Make `start.ps1` create stable runtime artifacts and launch processes with PID files

**Files:**
- Modify: `start.ps1:1-77`

**Interfaces:**
- Consumes: existing command `.start.ps1`.
- Produces:
  - `logs/backend.log`
  - `logs/backend.err.log`
  - `logs/backend.pid`
  - `logs/frontend.log`
  - `logs/frontend.err.log`
  - `logs/frontend.pid`

- [ ] **Step 1: Confirm current failure mode**

Run:

```powershell
Remove-Item -Recurse -Force logs -ErrorAction SilentlyContinue
.\start.ps1
```

Expected before implementation: the current script fails or gives unreliable state because `logs\backend.log` cannot be created when `logs/` is missing, or it reports started without proving service readiness.

- [ ] **Step 2: Replace the script body with directory setup and process helpers**

Replace `start.ps1` with this structure:

```powershell
# Jianmen one-click dev startup
# Usage: .\start.ps1
# First run will set up everything automatically

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
    Write-Host "启动失败: $Message" -ForegroundColor Red
    Show-LogTail "logs\backend.err.log" 80
    Show-LogTail "logs\backend.log" 80
    Show-LogTail "logs\frontend.err.log" 80
    Show-LogTail "logs\frontend.log" 80
    exit 1
}

try {
    Write-Host "=== Jianmen Dev Startup ===" -ForegroundColor Cyan
    Write-Host ""

    Write-Step "[1/6] Preparing directories..."
    New-Item -ItemType Directory -Force -Path "bin" | Out-Null
    New-Item -ItemType Directory -Force -Path "logs" | Out-Null
    Write-Ok "runtime directories ready"

    Write-Step "[2/6] Stopping old instances..."
    Stop-ProcessFromPidFile "logs\backend.pid" "bastion-core"
    Stop-ProcessFromPidFile "logs\frontend.pid" "vite-dev"
    Get-Process -Name "bastion-core" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    $oldNode = Get-Process -Name "node" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -like "*vite*" -or $_.CommandLine -like "*npm*run*dev*" }
    if ($oldNode) { $oldNode | Stop-Process -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 1
    Write-Ok "old instances stopped"

    Write-Step "[3/6] Preparing config..."
    if (-not (Test-Path config.local.json)) {
        Copy-Item config.example.json config.local.json
        Write-Ok "created config.local.json from template"
    } else {
        Write-Info "config.local.json already exists"
    }

    Write-Step "[4/6] Building backend..."
    go build -o bin\bastion-core.exe .\cmd\bastion-core
    if ($LASTEXITCODE -ne 0) { throw "Backend build failed" }
    Write-Ok "backend built: bin\bastion-core.exe"

    Write-Step "[5/6] Starting backend..."
    Remove-Item "logs\backend.log", "logs\backend.err.log" -Force -ErrorAction SilentlyContinue
    $backend = Start-Process -FilePath ".\bin\bastion-core.exe" -ArgumentList "-config", "config.local.json" -WorkingDirectory (Get-Location).Path -RedirectStandardOutput "logs\backend.log" -RedirectStandardError "logs\backend.err.log" -PassThru
    Set-Content -Path "logs\backend.pid" -Value $backend.Id -Encoding ascii
    Write-Ok "backend process started: PID $($backend.Id)"

    Wait-HttpOk "Admin API" "http://127.0.0.1:47100/api/health" 20 $null
    Wait-TcpPort "Database gateway" "127.0.0.1" 33060 10
    Wait-TcpPort "SSH gateway" "127.0.0.1" 47102 10

    Write-Step "[6/6] Starting frontend..."
    Push-Location web
    try {
        if (-not (Test-Path node_modules)) {
            Write-Info "installing npm dependencies..."
            npm install
            if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
        }
    } finally {
        Pop-Location
    }

    Remove-Item "logs\frontend.log", "logs\frontend.err.log" -Force -ErrorAction SilentlyContinue
    $npmCommand = (Get-Command npm.cmd -ErrorAction SilentlyContinue).Source
    if (-not $npmCommand) { $npmCommand = (Get-Command npm -ErrorAction Stop).Source }
    $frontend = Start-Process -FilePath $npmCommand -ArgumentList "run", "dev" -WorkingDirectory (Join-Path (Get-Location).Path "web") -RedirectStandardOutput "logs\frontend.log" -RedirectStandardError "logs\frontend.err.log" -PassThru
    Set-Content -Path "logs\frontend.pid" -Value $frontend.Id -Encoding ascii
    Write-Ok "frontend process started: PID $($frontend.Id)"

    Wait-HttpOk "Web UI" "http://127.0.0.1:47101/" 30 $null
    Wait-HttpOk "Frontend API proxy" "http://127.0.0.1:47101/api/hosts" 15 @{Authorization='Bearer dev-admin-token'}

    Write-Host ""
    Write-Host "=== All services started and verified ===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Admin API : http://127.0.0.1:47100/" -ForegroundColor White
    Write-Host "  Web UI    : http://127.0.0.1:47101/" -ForegroundColor White
    Write-Host "  SSH GW    : 127.0.0.1:47102" -ForegroundColor White
    Write-Host "  DB GW     : 127.0.0.1:33060" -ForegroundColor White
    Write-Host "  Token     : dev-admin-token" -ForegroundColor Gray
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
```

- [ ] **Step 3: Verify script creates artifacts and reports readiness**

Run:

```powershell
.\start.ps1
```

Expected: exit code `0` and output includes:

```text
=== All services started and verified ===
Admin API ready
Database gateway ready
SSH gateway ready
Web UI ready
Frontend API proxy ready
```

Expected files:

```text
logs\backend.pid
logs\frontend.pid
logs\backend.log
logs\backend.err.log
logs\frontend.log
logs\frontend.err.log
```

- [ ] **Step 4: Verify repeated run cleans old processes and restarts cleanly**

Run:

```powershell
$oldBackendPid = Get-Content logs\backend.pid
$oldFrontendPid = Get-Content logs\frontend.pid
.\start.ps1
$newBackendPid = Get-Content logs\backend.pid
$newFrontendPid = Get-Content logs\frontend.pid
if ($oldBackendPid -eq $newBackendPid) { throw "backend PID did not change" }
if ($oldFrontendPid -eq $newFrontendPid) { throw "frontend PID did not change" }
```

Expected: command exits successfully, proving the script can be rerun by AI without manual cleanup.

- [ ] **Step 5: Commit**

```bash
git add start.ps1
git commit -m "chore: improve Jianmen dev startup script"
```

### Task 2: Keep the run skill aligned with the optimized startup path

**Files:**
- Modify: `.claude/skills/run-jianmen/SKILL.md` if present in the worktree. If the skill is only loaded externally and not present as a repository file, skip this task and mention that no repo-local skill file exists.

**Interfaces:**
- Consumes: `.start.ps1` output from Task 1.
- Produces: documentation that tells AI to use `.start.ps1` and trust its built-in verification.

- [ ] **Step 1: Check whether the repo-local skill file exists**

Run:

```powershell
Test-Path ".claude\skills\run-jianmen\SKILL.md"
```

Expected in this worktree: likely `False`; the skill content may be injected from the user command rather than stored in the repository.

- [ ] **Step 2: If the file exists, update verification wording**

Edit `.claude/skills/run-jianmen/SKILL.md` so the Quick Start says:

```markdown
### One-Click (Recommended)

```powershell
.\start.ps1
```

This script now handles cleanup, config, build, dependency install, service startup, and readiness checks. It exits non-zero and prints recent logs when any service fails.
```

- [ ] **Step 3: Verify documentation path**

Run:

```powershell
if (Test-Path ".claude\skills\run-jianmen\SKILL.md") { Select-String -Path ".claude\skills\run-jianmen\SKILL.md" -Pattern "readiness checks" }
```

Expected: if file exists, output includes `readiness checks`; if it does not exist, command produces no error.

- [ ] **Step 4: Commit if a repo-local skill file changed**

```bash
git add .claude/skills/run-jianmen/SKILL.md
git commit -m "docs: align Jianmen run skill with startup checks"
```

Skip this commit if `.claude/skills/run-jianmen/SKILL.md` does not exist.

## Self-Review

- Spec coverage: The plan keeps `start.ps1` as the single AI entrypoint, creates missing directories, writes logs/PIDs, uses `Start-Process`, verifies backend/frontend/DB/SSH readiness, and prints diagnostics on failure.
- Placeholder scan: No TBD/TODO/placeholder steps remain.
- Type consistency: PowerShell helper names are consistent across the plan: `Wait-HttpOk`, `Wait-TcpPort`, `Fail-WithDiagnostics`, `Stop-ProcessFromPidFile`, `Show-LogTail`.
