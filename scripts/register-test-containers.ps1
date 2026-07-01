# 自动将测试容器注册到 Jianmen 的配置脚本
# 前提：Jianmen 后端已启动，测试容器已运行
# 用法：.\scripts\register-test-containers.ps1

$ErrorActionPreference = "Continue"

$API_BASE = "http://127.0.0.1:47100/api"
$API_TOKEN = "577055e91143607e27758ce38059e828d2b5f22dc6212397ce067a91e9b8d61b"
$AUTH_HEADER = @{ Authorization = "Bearer $API_TOKEN" }

$MYSQL_ADDRESS = "127.0.0.1"
$MYSQL_PORT = 13306
$PG_PORT = 15432
$SSH_PORT = 12222

function Register-Containers {
    Write-Host "=== 注册测试容器到 Jianmen ===" -ForegroundColor Cyan
    Write-Host ""

    # ---- 1. 注册 MySQL 实例 ----
    Write-Host "[1/6] 注册 MySQL 数据库实例..." -ForegroundColor Green
    $mysqlBody = @{
        name     = "test-mysql"
        protocol = "mysql"
        address  = $MYSQL_ADDRESS
        port     = $MYSQL_PORT
        group    = ""
        remark   = "Docker MySQL 8.0 test instance"
    } | ConvertTo-Json

    $mysqlInstance = $null
    try {
        $resp = Invoke-WebRequest -Uri "$API_BASE/db/instances" -Method POST -Headers $AUTH_HEADER -Body $mysqlBody -ContentType "application/json" -UseBasicParsing
        $mysqlInstance = $resp.Content | ConvertFrom-Json
        Write-Host "  MySQL instance created: id=$($mysqlInstance.id)" -ForegroundColor Green
    } catch {
        if ($_.Exception.Response) {
            $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
            $errBody = $reader.ReadToEnd()
            $reader.Close()
            if ($errBody -match "already exists|UNIQUE|unique|Duplicate") {
                Write-Host "  MySQL instance may already exist, fetching..." -ForegroundColor Yellow
                $listResp = Invoke-WebRequest -Uri "$API_BASE/db/instances" -Headers $AUTH_HEADER -UseBasicParsing
                $list = $listResp.Content | ConvertFrom-Json
                $mysqlInstance = $list | Where-Object { $_.name -eq "test-mysql" } | Select-Object -First 1
                if ($mysqlInstance) { Write-Host "  Found existing: id=$($mysqlInstance.id)" -ForegroundColor Green }
            } else {
                Write-Host "  MySQL instance failed: $errBody" -ForegroundColor Red
            }
        }
    }

    # ---- 2. 注册 MySQL 账号 ----
    if ($mysqlInstance) {
        Write-Host "[2/6] 注册 MySQL 账号..." -ForegroundColor Green
        $mysqlAcctBody = @{
            instance_id = $mysqlInstance.id
            username    = "app"
            password    = "app123"
            group       = ""
            remark      = "Docker MySQL test account"
        } | ConvertTo-Json

        $mysqlAcct = $null
        try {
            $resp = Invoke-WebRequest -Uri "$API_BASE/db/accounts" -Method POST -Headers $AUTH_HEADER -Body $mysqlAcctBody -ContentType "application/json" -UseBasicParsing
            $mysqlAcct = $resp.Content | ConvertFrom-Json
            Write-Host "  MySQL account created: resource_id=$($mysqlAcct.resource_id), unique_name=$($mysqlAcct.unique_name)" -ForegroundColor Green
        } catch {
            $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
            $errBody = $reader.ReadToEnd()
            $reader.Close()
            if ($errBody -match "already exists|UNIQUE|unique|Duplicate") {
                Write-Host "  MySQL account may already exist" -ForegroundColor Yellow
            } else {
                Write-Host "  MySQL account failed: $errBody" -ForegroundColor Red
            }
        }
    }

    # ---- 3. 注册 PostgreSQL 实例 ----
    Write-Host "[3/6] 注册 PostgreSQL 数据库实例..." -ForegroundColor Green
    $pgBody = @{
        name     = "test-postgres"
        protocol = "postgres"
        address  = $MYSQL_ADDRESS
        port     = $PG_PORT
        group    = ""
        remark   = "Docker PostgreSQL 16 test instance"
    } | ConvertTo-Json

    $pgInstance = $null
    try {
        $resp = Invoke-WebRequest -Uri "$API_BASE/db/instances" -Method POST -Headers $AUTH_HEADER -Body $pgBody -ContentType "application/json" -UseBasicParsing
        $pgInstance = $resp.Content | ConvertFrom-Json
        Write-Host "  PostgreSQL instance created: id=$($pgInstance.id)" -ForegroundColor Green
    } catch {
        if ($_.Exception.Response) {
            $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
            $errBody = $reader.ReadToEnd()
            $reader.Close()
            if ($errBody -match "already exists|UNIQUE|unique|Duplicate") {
                Write-Host "  PostgreSQL instance may already exist, fetching..." -ForegroundColor Yellow
                $listResp = Invoke-WebRequest -Uri "$API_BASE/db/instances" -Headers $AUTH_HEADER -UseBasicParsing
                $list = $listResp.Content | ConvertFrom-Json
                $pgInstance = $list | Where-Object { $_.name -eq "test-postgres" } | Select-Object -First 1
                if ($pgInstance) { Write-Host "  Found existing: id=$($pgInstance.id)" -ForegroundColor Green }
            } else {
                Write-Host "  PostgreSQL instance failed: $errBody" -ForegroundColor Red
            }
        }
    }

    # ---- 4. 注册 PostgreSQL 账号 ----
    if ($pgInstance) {
        Write-Host "[4/6] 注册 PostgreSQL 账号..." -ForegroundColor Green
        $pgAcctBody = @{
            instance_id = $pgInstance.id
            username    = "app"
            password    = "app123"
            group       = ""
            remark      = "Docker PostgreSQL test account"
        } | ConvertTo-Json

        $pgAcct = $null
        try {
            $resp = Invoke-WebRequest -Uri "$API_BASE/db/accounts" -Method POST -Headers $AUTH_HEADER -Body $pgAcctBody -ContentType "application/json" -UseBasicParsing
            $pgAcct = $resp.Content | ConvertFrom-Json
            Write-Host "  PostgreSQL account created: resource_id=$($pgAcct.resource_id), unique_name=$($pgAcct.unique_name)" -ForegroundColor Green
        } catch {
            $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
            $errBody = $reader.ReadToEnd()
            $reader.Close()
            if ($errBody -match "already exists|UNIQUE|unique|Duplicate") {
                Write-Host "  PostgreSQL account may already exist" -ForegroundColor Yellow
            } else {
                Write-Host "  PostgreSQL account failed: $errBody" -ForegroundColor Red
            }
        }
    }

    # ---- 5. 注册 SSH 主机 ----
    Write-Host "[5/6] 注册 SSH 主机..." -ForegroundColor Green
    $sshHostBody = @{
        name    = "test-ssh"
        address = $MYSQL_ADDRESS
        port    = $SSH_PORT
        group   = ""
        remark  = "Docker OpenSSH test host"
    } | ConvertTo-Json

    $sshHost = $null
    try {
        $resp = Invoke-WebRequest -Uri "$API_BASE/hosts" -Method POST -Headers $AUTH_HEADER -Body $sshHostBody -ContentType "application/json" -UseBasicParsing
        $sshHost = $resp.Content | ConvertFrom-Json
        Write-Host "  SSH host created: id=$($sshHost.id)" -ForegroundColor Green
    } catch {
        if ($_.Exception.Response) {
            $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
            $errBody = $reader.ReadToEnd()
            $reader.Close()
            if ($errBody -match "already exists|UNIQUE|unique|Duplicate") {
                Write-Host "  SSH host may already exist, fetching..." -ForegroundColor Yellow
                $listResp = Invoke-WebRequest -Uri "$API_BASE/hosts" -Headers $AUTH_HEADER -UseBasicParsing
                $list = $listResp.Content | ConvertFrom-Json
                $sshHost = $list | Where-Object { $_.name -eq "test-ssh" -or $_.address -eq $MYSQL_ADDRESS } | Select-Object -First 1
                if ($sshHost) { Write-Host "  Found existing: id=$($sshHost.id)" -ForegroundColor Green }
            } else {
                Write-Host "  SSH host failed: $errBody" -ForegroundColor Red
            }
        }
    }

    # ---- 6. 注册 SSH 目标账号 ----
    if ($sshHost) {
        Write-Host "[6/6] 注册 SSH 账号 (target)..." -ForegroundColor Green
        $targetBody = @{
            host_id                  = $sshHost.id
            host                     = $MYSQL_ADDRESS
            port                     = $SSH_PORT
            username                 = "app"
            password                 = "target123"
            insecure_ignore_host_key = $true
            name                     = "test-ssh-app"
            group                    = ""
            remark                   = "Docker OpenSSH test account"
        } | ConvertTo-Json

        try {
            $resp = Invoke-WebRequest -Uri "$API_BASE/targets" -Method POST -Headers $AUTH_HEADER -Body $targetBody -ContentType "application/json" -UseBasicParsing
            $target = $resp.Content | ConvertFrom-Json
            Write-Host "  SSH target created: resource_id=$($target.resource_id)" -ForegroundColor Green
        } catch {
            $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
            $errBody = $reader.ReadToEnd()
            $reader.Close()
            if ($errBody -match "already exists|UNIQUE|unique|Duplicate") {
                Write-Host "  SSH target may already exist" -ForegroundColor Yellow
            } else {
                Write-Host "  SSH target failed: $errBody" -ForegroundColor Red
            }
        }
    }

    # ---- 测试连接 ----
    Write-Host ""
    Write-Host "=== 测试连接 ===" -ForegroundColor Cyan

    # 测试 MySQL
    if ($mysqlInstance) {
        $accts = $null
        try {
            $acctsResp = Invoke-WebRequest -Uri "$API_BASE/db/instances/$($mysqlInstance.id)/accounts" -Headers $AUTH_HEADER -UseBasicParsing
            $accts = $acctsResp.Content | ConvertFrom-Json
        } catch {}
        if ($accts -and $accts.Count -gt 0) {
            $acctId = $accts[0].id
            Write-Host "[测试] MySQL 连接 (account: $acctId)..." -ForegroundColor Yellow
            try {
                $resp = Invoke-WebRequest -Uri "$API_BASE/db/accounts/test/$acctId" -Method POST -Headers $AUTH_HEADER -UseBasicParsing
                $result = $resp.Content | ConvertFrom-Json
                if ($result.ok) {
                    Write-Host "  MySQL OK! latency: $($result.latency_ms)ms" -ForegroundColor Green
                } else {
                    Write-Host "  MySQL FAIL: $($result.error)" -ForegroundColor Red
                }
            } catch {
                Write-Host "  MySQL test request failed: $_" -ForegroundColor Red
            }
        }
    }

    # 测试 PostgreSQL
    if ($pgInstance) {
        $accts = $null
        try {
            $acctsResp = Invoke-WebRequest -Uri "$API_BASE/db/instances/$($pgInstance.id)/accounts" -Headers $AUTH_HEADER -UseBasicParsing
            $accts = $acctsResp.Content | ConvertFrom-Json
        } catch {}
        if ($accts -and $accts.Count -gt 0) {
            $acctId = $accts[0].id
            Write-Host "[测试] PostgreSQL 连接 (account: $acctId)..." -ForegroundColor Yellow
            try {
                $resp = Invoke-WebRequest -Uri "$API_BASE/db/accounts/test/$acctId" -Method POST -Headers $AUTH_HEADER -UseBasicParsing
                $result = $resp.Content | ConvertFrom-Json
                if ($result.ok) {
                    Write-Host "  PostgreSQL OK! latency: $($result.latency_ms)ms" -ForegroundColor Green
                } else {
                    Write-Host "  PostgreSQL FAIL: $($result.error)" -ForegroundColor Red
                }
            } catch {
                Write-Host "  PostgreSQL test request failed: $_" -ForegroundColor Red
            }
        }
    }

    # 测试 SSH
    $targets = $null
    try {
        $targetsResp = Invoke-WebRequest -Uri "$API_BASE/targets" -Headers $AUTH_HEADER -UseBasicParsing
        $targets = $targetsResp.Content | ConvertFrom-Json
    } catch {}
    if ($targets) {
        $sshTarget = $targets | Where-Object { $_.host -eq "$MYSQL_ADDRESS" -and $_.port -eq $SSH_PORT } | Select-Object -First 1
        if ($sshTarget) {
            Write-Host "[测试] SSH 连接 (target: $($sshTarget.id))..." -ForegroundColor Yellow
            try {
                $resp = Invoke-WebRequest -Uri "$API_BASE/targets/test-connection" -Method POST -Headers $AUTH_HEADER -Body ($sshTarget | ConvertTo-Json) -ContentType "application/json" -UseBasicParsing
                $result = $resp.Content | ConvertFrom-Json
                if ($result.ok) {
                    Write-Host "  SSH OK! latency: $($result.latency_ms)ms" -ForegroundColor Green
                } else {
                    Write-Host "  SSH FAIL: $($result.message)" -ForegroundColor Red
                }
            } catch {
                Write-Host "  SSH test request failed: $_" -ForegroundColor Red
            }
        }
    }

    Write-Host ""
    Write-Host "=== Summary ===" -ForegroundColor Cyan
    Write-Host "  MySQL:     mysql -u app -papp123 -h 127.0.0.1 -P $MYSQL_PORT testdb"
    Write-Host "  PostgreSQL: psql -U app -h 127.0.0.1 -p $PG_PORT -d app"
    Write-Host "  SSH:       ssh app@127.0.0.1 -p $SSH_PORT  (password: target123)"
    Write-Host ""
    Write-Host "  通过堡垒机连接 (需先获取 session):"
    Write-Host "    MySQL:  mysql -u <prefix><resource_id><session_id> -p -h 127.0.0.1 -P 33060"
    Write-Host "    SSH:    ssh <prefix><resource_id><session_id>@127.0.0.1 -p 47102"
}

Register-Containers
