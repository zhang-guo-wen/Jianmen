# Jianmen 测试环境一键创建脚本
# 功能：在 WSL Debian 中启动 MySQL、PostgreSQL、SSH 容器
# 用法：在 PowerShell 中运行: .\scripts\setup-test-containers.ps1 [start|stop|clean|status]

param(
    [string]$Action = "start"
)

$ErrorActionPreference = "Continue"

$MYSQL_PORT = 13306
$MYSQL_ROOT_PASSWORD = "root123"
$MYSQL_DATABASE = "testdb"
$MYSQL_USER = "app"
$MYSQL_PASSWORD = "app123"

$PG_PORT = 15432
$PG_USER = "app"

$SSH_PORT = 12222
$SSH_USER = "app"
$SSH_PASSWORD = "target123"

$CONTAINER_PREFIX = "jianmen-dev"

# 把 shell 脚本写到 Windows 临时文件，通过 wslpath 转成 WSL 路径后执行
function Invoke-WslScript {
    param([string]$ScriptContent)
    $tmpFile = [System.IO.Path]::GetTempFileName() + ".sh"
    try {
        [System.IO.File]::WriteAllText($tmpFile, $ScriptContent, [System.Text.Encoding]::ASCII)
        # Get the WSL path for this file
        $wslPath = (wsl -d Debian -- wslpath -a "$($tmpFile -replace '\\', '/')" 2>&1).Trim()
        if (-not $wslPath) { throw "wslpath failed" }
        wsl -d Debian -- bash $wslPath
        return $LASTEXITCODE
    } finally {
        Remove-Item $tmpFile -ErrorAction SilentlyContinue
    }
}

function Start-Containers {
    Write-Host "=== 启动 Jianmen 测试容器 ===" -ForegroundColor Cyan

    Write-Host "清理旧容器..." -ForegroundColor Yellow
    wsl -d Debian -- bash -c "docker rm -f ${CONTAINER_PREFIX}-mysql ${CONTAINER_PREFIX}-postgres ${CONTAINER_PREFIX}-ssh 2>/dev/null; echo done"
    Write-Host ""

    # ---- MySQL ----
    Write-Host "[1/3] 启动 MySQL 8.0..." -ForegroundColor Green
    $mysqlScript = @"
#!/bin/bash
set -e
docker run -d --name ${CONTAINER_PREFIX}-mysql \
  -e MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD} \
  -e MYSQL_DATABASE=${MYSQL_DATABASE} \
  -e MYSQL_USER=${MYSQL_USER} \
  -e MYSQL_PASSWORD=${MYSQL_PASSWORD} \
  -p 127.0.0.1:${MYSQL_PORT}:3306 \
  --restart unless-stopped \
  mysql:8.0 --default-authentication-plugin=mysql_native_password
"@
    $rc = Invoke-WslScript $mysqlScript
    if ($rc -ne 0) { Write-Host "MySQL 启动失败!" -ForegroundColor Red; return }

    # ---- PostgreSQL ----
    Write-Host "[2/3] 启动 PostgreSQL 16..." -ForegroundColor Green
    $pgScript = @"
#!/bin/bash
set -e
docker run -d --name ${CONTAINER_PREFIX}-postgres \
  -e POSTGRES_USER=${PG_USER} \
  -e POSTGRES_PASSWORD=${MYSQL_PASSWORD} \
  -e POSTGRES_DB=${PG_USER} \
  -p 127.0.0.1:${PG_PORT}:5432 \
  --restart unless-stopped \
  postgres:16-alpine
"@
    $rc = Invoke-WslScript $pgScript
    if ($rc -ne 0) { Write-Host "PostgreSQL 启动失败!" -ForegroundColor Red; return }

    # ---- SSH ----
    Write-Host "[3/3] 启动 SSH 服务器..." -ForegroundColor Green
    $sshScript = @"
#!/bin/bash
set -e
docker run -d --name ${CONTAINER_PREFIX}-ssh \
  -e PUID=1000 \
  -e PGID=1000 \
  -e TZ=Etc/UTC \
  -e USER_NAME=${SSH_USER} \
  -e USER_PASSWORD=${SSH_PASSWORD} \
  -e PASSWORD_ACCESS=true \
  -e SUDO_ACCESS=true \
  -p 127.0.0.1:${SSH_PORT}:2222 \
  --restart unless-stopped \
  lscr.io/linuxserver/openssh-server:latest
"@
    $rc = Invoke-WslScript $sshScript
    if ($rc -ne 0) { Write-Host "SSH 启动失败!" -ForegroundColor Red; return }

    Write-Host ""
    Write-Host "等待容器就绪..." -ForegroundColor Yellow
    Start-Sleep -Seconds 5

    # 等待 MySQL
    Write-Host "  等待 MySQL (最多 60s)..." -ForegroundColor DarkGray
    $mysqlReady = $false
    for ($i = 0; $i -lt 30; $i++) {
        $result = wsl -d Debian -- bash -c "docker exec ${CONTAINER_PREFIX}-mysql mysqladmin ping -h localhost -u root -p${MYSQL_ROOT_PASSWORD} 2>&1"
        if ($result -match "mysqld is alive") { $mysqlReady = $true; break }
        Start-Sleep -Seconds 2
    }
    if ($mysqlReady) { Write-Host "  MySQL 就绪" -ForegroundColor Green }
    else { Write-Host "  MySQL 可能尚未就绪" -ForegroundColor Red }

    # 等待 PostgreSQL
    Write-Host "  等待 PostgreSQL (最多 60s)..." -ForegroundColor DarkGray
    $pgReady = $false
    for ($i = 0; $i -lt 30; $i++) {
        $result = wsl -d Debian -- bash -c "docker exec ${CONTAINER_PREFIX}-postgres pg_isready -U ${PG_USER} 2>&1"
        if ($result -match "accepting connections") { $pgReady = $true; break }
        Start-Sleep -Seconds 2
    }
    if ($pgReady) { Write-Host "  PostgreSQL 就绪" -ForegroundColor Green }
    else { Write-Host "  PostgreSQL 可能尚未就绪" -ForegroundColor Red }

    # 等待 SSH
    Write-Host "  等待 SSH (最多 30s)..." -ForegroundColor DarkGray
    $sshReady = $false
    for ($i = 0; $i -lt 15; $i++) {
        try {
            $tcp = New-Object System.Net.Sockets.TcpClient
            if ($tcp.ConnectAsync("127.0.0.1", $SSH_PORT).Wait(2000)) { $tcp.Close(); $sshReady = $true; break }
            $tcp.Close()
        } catch {}
        Start-Sleep -Seconds 2
    }
    if ($sshReady) { Write-Host "  SSH 就绪" -ForegroundColor Green }
    else { Write-Host "  SSH 可能尚未就绪" -ForegroundColor Red }

    # ---- 输出摘要 ----
    Write-Host ""
    Write-Host "=== 容器已启动 ===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "--- MySQL ---" -ForegroundColor Magenta
    Write-Host "  地址:     127.0.0.1:${MYSQL_PORT}"
    Write-Host "  协议:     mysql"
    Write-Host "  数据库:   ${MYSQL_DATABASE}"
    Write-Host "  用户名:   ${MYSQL_USER}"
    Write-Host "  密码:     ${MYSQL_PASSWORD}"
    Write-Host "  root密码: ${MYSQL_ROOT_PASSWORD}"
    Write-Host ""
    Write-Host "--- PostgreSQL ---" -ForegroundColor Magenta
    Write-Host "  地址:     127.0.0.1:${PG_PORT}"
    Write-Host "  协议:     postgres"
    Write-Host "  数据库:   ${PG_USER}"
    Write-Host "  用户名:   ${PG_USER}"
    Write-Host "  密码:     ${MYSQL_PASSWORD}"
    Write-Host ""
    Write-Host "--- SSH ---" -ForegroundColor Magenta
    Write-Host "  地址:     127.0.0.1:${SSH_PORT}"
    Write-Host "  用户名:   ${SSH_USER}"
    Write-Host "  密码:     ${SSH_PASSWORD}"
    Write-Host ""
    Write-Host "=== 数据库实例配置（通过页面或 scripts\register-test-containers.ps1 自动注册）===" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  MySQL 实例:  名称=test-mysql, 协议=mysql, 地址=127.0.0.1, 端口=${MYSQL_PORT}"
    Write-Host "  MySQL 账号:  用户名=app, 密码=app123"
    Write-Host ""
    Write-Host "  PG 实例:     名称=test-postgres, 协议=postgres, 地址=127.0.0.1, 端口=${PG_PORT}"
    Write-Host "  PG 账号:     用户名=app, 密码=app123"
    Write-Host ""
    Write-Host "  SSH 主机:    名称=test-ssh, 地址=127.0.0.1, 端口=${SSH_PORT}"
    Write-Host "  SSH 账号:    用户名=app, 密码=target123"
}

function Stop-Containers {
    Write-Host "停止测试容器..." -ForegroundColor Yellow
    wsl -d Debian -- bash -c "docker stop ${CONTAINER_PREFIX}-mysql ${CONTAINER_PREFIX}-postgres ${CONTAINER_PREFIX}-ssh 2>&1"
    Write-Host "已停止" -ForegroundColor Green
}

function Clean-Containers {
    Write-Host "删除测试容器..." -ForegroundColor Red
    wsl -d Debian -- bash -c "docker rm -f ${CONTAINER_PREFIX}-mysql ${CONTAINER_PREFIX}-postgres ${CONTAINER_PREFIX}-ssh 2>&1"
    Write-Host "已删除" -ForegroundColor Green
}

function Show-Status {
    Write-Host "容器状态:" -ForegroundColor Cyan
    wsl -d Debian -- bash -c "docker ps --filter name=${CONTAINER_PREFIX} --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' 2>&1"
}

switch ($Action) {
    "start"  { Start-Containers }
    "stop"   { Stop-Containers }
    "clean"  { Clean-Containers }
    "status" { Show-Status }
    default  { Write-Host "用法: .\scripts\setup-test-containers.ps1 [start|stop|clean|status]" }
}
