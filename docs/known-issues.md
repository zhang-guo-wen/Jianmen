# 已知问题

## PostgreSQL SCRAM-SHA-256 认证未完全支持

**日期**: 2026-06-28

**问题**: PostgreSQL 10+ 默认使用 SCRAM-SHA-256 密码加密认证。当前数据库代理的 PG 路径在遇到 auth type 10 (SASL) 时，SCRAM 握手实现存在 bug，导致连接超时（~3秒后 auth denied）。

**影响范围**: 使用 PostgreSQL 17 等默认 `password_encryption = scram-sha-256` 的上游数据库。

**临时方案**: 将上游 PG 的 `password_encryption` 改为 `md5`，然后重置用户密码。

**相关代码**:
- `internal/server/dbproxy/server.go` — `pgSCRAMExchange()` 方法
- `internal/server/admin/test_db.go` — `testPGScramAuth()` 函数

**已实现部分**:
- SCRAM-SHA-256 的 PBKDF2、HMAC、XOR 等数学运算 ✅
- SASLInitialResponse 消息构造 ✅
- client-final-message 构造 ✅

**未解决**: PasswordMessage 的 SASL 消息格式（长度字段计算/末尾 null 问题）导致上游 PG 不响应。

**参考**: MySQL caching_sha2_password 类似问题已解决（AuthSwitch + raw auth response）。
