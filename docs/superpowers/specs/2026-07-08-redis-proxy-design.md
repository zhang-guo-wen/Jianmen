# Redis 代理支持设计文档

日期：2026-07-08

## 概述

为 Jianmen 堡垒机数据库网关新增 Redis 协议代理支持。当前网关已支持 MySQL 和 PostgreSQL，Redis 是第三个支持的数据库协议。

## 关键决策

| 决策 | 选择 | 说明 |
|---|---|---|
| 认证模式 | ACL 模式（`AUTH username password`） | Redis 6+ ACL，账号用户名不为空时使用；username 为空时回退单密码模式 |
| Compact username 前缀 | `R`（新前缀） | 独立于 MySQL/PG 的 `D` 前缀，网关通过前缀区分协议 |
| 端口策略 | 复用 `33060` | 在 `handleConn` 协议检测中加 RESP 分支，不需新增监听器 |

## 协议检测

Redis 客户端连接后直接发送 RESP 命令（以 `*` 开头），不等待服务端握手。

`handleConn` 新增分支：

```go
case firstByte[0] == '*':
    conn = g.handleRedis(ctx, client, firstByte[0])
```

与现有 MySQL（超时=客户端等握手）和 PG（首字节 `0x00`）协议检测正交，无冲突。

## 认证流程

```
Client连接 → 发 AUTH RXXXXYYYYY bastion_password
                ↓
代理解析 RESP → 提取紧凑用户名(R前缀) + 密码
                ↓
resolveAccount(compact_username) → 查找 DatabaseAccount + UserSession + User
                ↓
validateUserPassword(bcrypt) → 验证堡垒机密码
                ↓
RBAC 检查 + 账号/实例状态检查（禁用、过期）
                ↓
连接上游 Redis → 发 AUTH <upstream_user> <upstream_password>（或单密码模式）
                ↓
转发上游响应给客户端 → 返回 gatewayConn 开始透明转发
```

### AUTH 凭据替换逻辑

- 如果 `DatabaseAccount.Username` 不为空：发送 `AUTH <username> <password>`（ACL 模式）
- 如果 `DatabaseAccount.Username` 为空：发送 `AUTH <password>`（旧版单密码模式）

客户端发来的 AUTH 密码是堡垒机登录密码（bcrypt 验证），上游 Redis 密码从 `DatabaseAccount.Password.GetPlaintext()` 获取（AES-256-GCM 解密）。

## RESP 解析器

新增最小 RESP 解析器（约 80 行），支持：

- 读取 RESP 命令（`*N\r\n$len\r\narg\r\n...`）
- 提取命令名和参数列表
- 处理简单字符串、错误、整数回复用于上游响应解析

不需要完整 Redis 客户端协议栈（如 pub/sub、事务），只需要命令解析和简单回复解析。

## 命令审计

新增 `redisObserver`（`observer.go`），实现 `queryObserver` 接口。

### 命令分类（queryKind）

| 类别 | 命令示例 |
|---|---|
| `read` | GET, HGET, LRANGE, SMEMBERS, ZRANGE, STRLEN, EXISTS |
| `write` | SET, DEL, HSET, LPUSH, SADD, ZADD, EXPIRE |
| `admin` | CONFIG, FLUSHDB, FLUSHALL, SHUTDOWN, DEBUG |
| `other` | PING, SELECT, ECHO, QUIT, COMMAND |

审计记录字段复用：`DBConnectionMeta.Protocol = "redis"`，`DBQueryEvent.SQL` 存完整 RESP 命令文本，`QueryKind` 存命令分类。

## 改动清单

### 后端改动

| 文件 | 操作 | 内容 |
|---|---|---|
| `internal/store/dbstore_databases.go` | 修改 | `normalizeDBProtocol` 新增 `"redis"`；`upstreamAddress` 新增 redis → 6379 默认端口 |
| `internal/server/dbproxy/server.go` | 修改 | `handleConn` 协议检测新增 `case '*'`；`upstreamAddress` 和 `resolveAccount` 支持 R 前缀 |
| `internal/server/dbproxy/redis.go` | **新增** | RESP 解析器 + `handleRedis` 认证方法（~250-300 行） |
| `internal/server/dbproxy/observer.go` | 修改 | `newQueryObserver` 新增 `case "redis"`；新增 `redisObserver` |
| `internal/server/admin/test_db.go` | 修改 | `testDBAuth` 新增 `case "redis"` |
| `internal/util/compact.go` | 修改 | 新增 `PrefixRedis = "R"` |

### 前端改动

| 文件 | 操作 | 内容 |
|---|---|---|
| `web/src/views/DatabaseView.vue` | 修改 | 协议下拉加 "Redis" 选项；连接命令加 `redis-cli` 生成逻辑；账号表单用户名字段支持可选 |
| `web/src/api/client.ts` | 无需改动 | `protocol` 字段已是 `string` 类型 |

### 不需要改动的文件

- `internal/model/core.go` — `DatabaseInstance.Protocol` 已是字符串
- `internal/rbac/` — `ActionDBConnect` 通用
- `internal/store/store.go` — Store 接口不变
- `internal/config/config.go` — Redis 复用现有 `DatabaseGateway` 配置

## 连接命令（前端展示）

```bash
# ACL 模式（账号有用户名）
redis-cli -h <gemini_host> -p 33060 --no-auth-warning
# 进入后手动输入：AUTH RXXXXYYYYY <堡垒机密码>

# 或一行直连（Redis 6+ ACL）
redis-cli -h <gemini_host> -p 33060 --user RXXXXYYYYY --pass <堡垒机密码>
```

## 测试计划

- **单元测试**：RESP 解析器（各种命令格式）、compact username R 前缀编解码
- **集成测试**（`internal/integration/`）：Redis Docker 容器 → 代理 → 客户端全链路
- **测试连接**：`POST /api/db/accounts/test` 新增 Redis 认证验证
- **前端验证**：`npm run typecheck && npm run build`
- **后端验证**：`go build ./... && go test ./... -count=1`
