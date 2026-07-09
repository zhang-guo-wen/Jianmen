# MySQL 一键自动创建账号 — 设计规格

日期：2026-07-09

## 概述

管理员在堡垒机中已保存 MySQL 高权限凭据（如 root），无需登录目标服务器手动执行 SQL，直接在堡垒机页面一键创建低权限 MySQL 账号。堡垒机自动连接目标 MySQL、执行 `CREATE USER` / `GRANT`、并将新账号注册到堡垒机。

## 流程

```
管理员在数据库账号列表页 → 点击"自动创建账号"
  → 选择管理员凭据（默认第一个）
  → 连接目标 MySQL → SHOW DATABASES → 列出所有数据库
  → 为每个数据库选择：无 / 读 / 读写
  → 堡垒机自动生成随机密码
  → 执行 CREATE USER + 多条 GRANT
  → 注册到 database_account 表
  → 弹窗展示结果（账号名 + 密码，一次性复制）
```

## API

### GET /api/db/instances/{id}/databases

连接目标 MySQL 实例，用指定管理员账号执行 `SHOW DATABASES`，返回数据库名称列表。

查询参数：

| 参数 | 必填 | 说明 |
|------|------|------|
| `admin_account_id` | 是 | 用于连接目标 MySQL 的管理员凭据 ID |

响应：

```json
{
  "databases": ["information_schema", "mysql", "myapp", "logs"]
}
```

### POST /api/db/instances/{id}/provision-account

自动创建 MySQL 账号并注册到堡垒机。

请求体：

```json
{
  "admin_account_id": "xxx",
  "new_username": "dev_readonly",
  "password": "",
  "host": "%",
  "grants": [
    { "database": "myapp", "privilege": "readwrite" },
    { "database": "logs", "privilege": "read" }
  ],
  "group": "",
  "remark": "",
  "expires_at": null
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `admin_account_id` | string | 是 | 管理员凭据 ID |
| `new_username` | string | 否 | 新账号用户名，不填则自动生成 |
| `password` | string | 否 | 新账号密码，不填则自动生成 20 位随机密码 |
| `host` | string | 否 | 允许登录的主机，默认 `%` |
| `grants` | array | 是 | 授权列表，可以为空数组 |
| `grants[].database` | string | 是 | 数据库名 |
| `grants[].privilege` | string | 是 | `read` 或 `readwrite` |
| `group` | string | 否 | 账号分组 |
| `remark` | string | 否 | 备注 |
| `expires_at` | string | 否 | 有效期 ISO8601 |

权限映射：

| privilege | SQL |
|-----------|-----|
| `read` | `GRANT SELECT ON <db>.* TO '<user>'@'<host>'` |
| `readwrite` | `GRANT SELECT, INSERT, UPDATE, DELETE ON <db>.* TO '<user>'@'<host>'` |

响应：

```json
{
  "ok": true,
  "account": { /* DatabaseAccountView */ },
  "generated_password": "<明文密码>"
}
```

`generated_password` 仅此一次返回，后续 API 不再暴露。

错误情况：

| 场景 | HTTP 状态码 | 说明 |
|------|------------|------|
| 管理员账号不存在/已禁用/已过期 | 400 | 提示管理员凭据不可用 |
| 管理员凭据无法连接目标 MySQL | 502 | 连接失败 |
| 管理员凭据无 CREATE USER 权限 | 502 | SQL 执行被拒绝 |
| 新用户名已存在 | 409 | 目标 MySQL 或堡垒机中已有同名账号 |

## 权限模型

展开数据库列表，每行一个数据库，三个选项：无 / 读 / 读写。快捷按钮：

- **全部只读**：所有已展示数据库一键设为读
- **全部读写**：所有已展示数据库一键设为读写

每库权限映射：

| 选项 | GRANT SQL |
|------|-----------|
| 读 | `GRANT SELECT ON <db>.* TO '<user>'@'<host>'` |
| 读写 | `GRANT SELECT, INSERT, UPDATE, DELETE ON <db>.* TO '<user>'@'<host>'` |
| 无 | 不生成 GRANT 语句

## MySQL 协议执行

在 `test_db.go` 现有模式基础上扩展：

1. TCP 连接目标 MySQL
2. 读取 handshake packet
3. 使用管理员凭据（用户名 + 解密密码）完成 `mysql_native_password` 或 `caching_sha2_password` 认证
4. 发送 `SHOW DATABASES` 查询 → 解析结果集
5. 发送 `CREATE USER 'xxx'@'host' IDENTIFIED BY '<password>'`
6. 逐条发送 `GRANT ... ON db.* TO 'xxx'@'host'`
7. 关闭连接

## 后端分层

```
handler/db_provision.go     ← 新文件：GET databases + POST provision-account
    ↓
service/db_provision.go     ← 新文件：MySQL 连接、执行 SQL
    ↓
store/dbstore_databases.go  ← 已有：AddDatabaseAccount
```

service 层职责：
- `ListDatabases(ctx, instance, adminAccount) ([]string, error)` — 连接并执行 `SHOW DATABASES`
- `ProvisionAccount(ctx, instance, adminAccount, newUser, password, host, grants) error` — 连接并执行 `CREATE USER` + `GRANT`

## 前端变更

- 数据库账号列表页（`/databases` 下的实例详情 → 账号列表）新增"自动创建"按钮
- 弹窗三步：
  1. **选择凭据**：下拉选择管理员凭据，默认第一个
  2. **授权配置**：新用户名（可选）+ 数据库列表 + 权限选择（无/读/读写）+ 快捷按钮
  3. **确认并展示结果**：随机密码展示 + 复制按钮
- 更多设置折叠：host、分组、备注、有效期

## 安全考虑

- 明文密码仅在新创建响应中返回一次，不持久化到日志
- 管理员凭据的密码从 EncryptedField 解密后仅在内存中使用
- 新账号密码在堡垒机中加密存储（同现有 EncryptedField 机制）
- 仅超级管理员或有 `dbproxy:create` 权限的用户可使用此功能

## 不在此范围

- PostgreSQL / Redis 自动创建账号（后续再做）
- SSH 主机账号自动创建（后续再做）
- 批量创建（一次创建多个账号）
- 权限回收 / 账号删除联动目标 MySQL
