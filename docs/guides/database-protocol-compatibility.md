# 数据库真实协议兼容矩阵

更新时间：2026-07-20

本矩阵描述 Jianmen 数据库网关已经纳入自动化验证的协议与版本边界。“实库”表示测试会启动官方 Docker 镜像，并使用真实客户端通过网关完成认证和数据交互；“协议测试”表示使用有界解析、畸形帧和模糊测试验证协议适配器，但不等同于对应产品版本的实库认证。

## 入口模式与端口

| 模式 | MySQL | PostgreSQL | Redis | 适用场景 |
|---|---:|---:|---:|---|
| 统一入口 `unified`（默认） | `33060` | `33060` | `33060` | 只开放一个数据库端口；使用原生客户端，无需安装 Connector |
| 独立入口 `independent` | `33061` | `33062` | `33063` | 优先最低建连延迟，或希望按协议分别配置网络策略 |

统一入口只影响连接建立阶段。MySQL 客户端必须先等待服务端 Greeting，网关用默认 200ms 的静默探测窗口区分它与会主动发送首包的 PostgreSQL/Redis，因此每次 MySQL 新连接约增加 200ms；连接建立后的 SQL、事务和数据转发没有这段固定等待。独立 MySQL 入口会立即发送 Greeting。

统一入口不会使用账号名称猜测协议。路由只依据原生协议握手：静默到期且完全没有收到字节才进入 MySQL；`0x00` 首字节进入 PostgreSQL；`*` 首字节进入 Redis；TLS ClientHello 完成一次共享 TLS 握手后，再用 ALPN 与解密后的首包交叉确认 PostgreSQL Direct TLS 或 Redis TLS。只要已收到任何字节，后续超时或解析失败就关闭连接，不会回退为 MySQL。

TLS 用于加密客户端到 Jianmen 之间的认证信息与数据库流量，并验证当前连接的确是目标 Jianmen 网关。客户端 TLS 策略支持 `optional`（默认，同时接受明文和 TLS）与 `required`（只接受 TLS），并统一作用于 MySQL、PostgreSQL 和 Redis。两种模式未配置证书路径时，Jianmen 都会在数据目录自动生成并持续复用托管 TLS 身份，不区分监听地址；显式配置证书时优先使用用户证书。为兼容 PostgreSQL 驱动，`required` 仍接受独立连接上的明文 `CancelRequest` 控制包；该包不携带登录凭据或数据库数据，且必须匹配会话下发的进程 ID 与取消密钥才会转发。

## 客户端证书信任

数据库网关身份有两种派生信任来源：

- `custom`：显式配置私有 `ca_file`，或使用单张自签名叶证书固定信任；客户端必须使用 Jianmen 分发的 CA 材料。
- `system`：CA 签发的证书未配置 `ca_file`，Jianmen 使用运行环境系统根库验证；`cert_file` 必须是叶证书在前、包含全部中间证书的完整链，`server_name` 必须匹配 SAN。

DBeaver 的 MySQL 连接使用 Connector/J 默认信任库和 `VERIFY_IDENTITY`；PostgreSQL 连接使用 Java 默认信任库、`DefaultJavaSSLFactory` 和 `verify-full`。`psql` 仅从 libpq 16 开始支持 `sslrootcert=system`，旧版本需改用下载的 CA 文件。MySQL 原生命令行的 `VERIFY_IDENTITY` 仍需显式 `--ssl-ca` 或 `--ssl-capath`。Redis TLS 快速命令继续不可用，因为当前 `redis-cli` 路径不能提供所需的严格主机名验证。所有不兼容或验证失败场景都会关闭连接，不会降级为明文或只加密不验身份。

## 默认实库矩阵

| 协议 | 官方镜像 | 客户端/认证路径 | 每个版本的必测场景 |
|---|---|---|---|
| MySQL | `mysql:5.7`、`mysql:8.0`、`mysql:8.4` | `go-sql-driver/mysql`；堡垒机 `mysql_native_password`；上游 5.7 `mysql_native_password`、8.x `caching_sha2_password` | 初始数据库、普通查询、预处理语句、提交/回滚、审计脱敏、超过 `0xFFFFFF` 的多物理包响应及恶意边界字节 |
| PostgreSQL | `postgres:14-alpine` 至 `postgres:18-alpine` | `pgx/v5`、`database/sql` 和原始协议客户端；网关 TLS；上游 SCRAM-SHA-256（RFC 4013 SASLprep） | Startup 参数、简单/扩展查询、预处理语句、提交/回滚、ErrorResponse 后恢复、COPY、大 DataRow、CancelRequest |
| Redis | `redis:6.2-alpine`、`redis:7.4-alpine`、`redis:8.8-alpine` | 原始 RESP/TLS 客户端；统一和独立入口均验证 TLS、双参数 `AUTH`、`HELLO 2 AUTH`、`HELLO 3 AUTH` | RESP2/RESP3、流水线、MULTI/EXEC、SELECT、Map/Set/Boolean/Double、Pub/Sub 与 Push、多主题批量退订、临界大主题 ACK、大 Bulk String、审计脱敏 |

默认矩阵由以下测试实现：

- [MySQL 实库矩阵](../internal/integration/mysql_proxy_integration_test.go)
- [PostgreSQL 实库矩阵](../internal/integration/postgres_proxy_integration_test.go)
- [Redis 实库矩阵](../internal/integration/redis_proxy_integration_test.go)

每个镜像版本都会分别经过统一入口与独立入口；监听器的启动、关闭、部分绑定失败回收、握手超时、协议误判防护和活动连接关闭另由 [监听器测试](../internal/server/dbproxy/listeners_test.go) 覆盖。

## 协议能力边界

| 能力 | MySQL | PostgreSQL | Redis |
|---|---|---|---|
| 基础协议 | Protocol 4.1 | Protocol 3.0 | RESP2、RESP3 |
| 新版本协商 | 8.x `caching_sha2_password`，完整认证仅在已验证 TLS 上发送明文口令 | 3.2 客户端通过 `NegotiateProtocolVersion` 降级到 3.0；保留普通 Startup 参数并报告 `_pq_.*` 不支持项 | `HELLO 2/3`，网关凭据不会转发给上游 |
| 客户端加密 | `optional` 接受明文或 MySQL SSLRequest/TLS；`required` 仅接受 TLS | `optional` 接受明文、SSLRequest/TLS 或 Direct TLS；`required` 仅接受 TLS，Direct TLS 要求 ALPN `postgresql` | `optional` 接受明文或 TLS；`required` 仅接受 TLS |
| 上游加密 | 默认 `disable`；8.0/8.4 实库矩阵另使用 `verify-ca` 验证 TLS，5.7 使用明文基线 | 默认 `disable`，可配置 `verify-ca` / `verify-full`；本矩阵的官方镜像使用明文上游 | 默认 `disable`，可配置 `verify-ca` / `verify-full`；明文模式下 `AUTH` 凭据也不加密 |
| 查询形态 | COM_QUERY、COM_STMT_PREPARE/EXECUTE、事务 | 简单/扩展查询、COPY、事务、异步 CancelRequest | 普通命令、流水线、事务、Pub/Sub |
| 大响应 | 验证跨 `0xFFFFFF` 物理包边界、零长终止片及续片首字节 `0xff`/`0xfe` | 验证 300 KB DataRow 和 COPY | 验证 300 KB Bulk String、超大 Pub/Sub 消息及超过审计缓冲区的订阅 ACK |
| 畸形输入 | 包头、长度、截断帧模糊测试 | StartupMessage/类型消息模糊测试；取消密钥长度边界测试 | RESP2/3 类型、嵌套、长度、命令和认证模糊测试 |

PostgreSQL 18 的协议 3.2 使用可变长度取消密钥。网关能够安全解析和路由 4–256 字节密钥，但当前会把 3.2 会话协商到 3.0，因此官方 PostgreSQL 18 实库路径实际使用 3.0 的 4 字节密钥。Direct TLS、3.2 协商和可变密钥分别有原始协议实测或协议测试，不能把“可协商连接”解读为“网关原生运行 3.2”。

上游 TLS 指 `Jianmen → 实际数据库`，与客户端到 Jianmen 的网关 TLS 无关。新建数据库实例默认不启用上游 TLS，以兼容只提供 IP 或没有证书的数据库；该模式只适合受信任内网。MySQL `caching_sha2_password` 完整认证与 PostgreSQL 明文密码认证仍要求经过验证的 TLS，不会因为默认关闭上游 TLS 而发送裸密码。

## 明确不在支持矩阵内

- MySQL：pre-4.1、压缩协议、`LOCAL INFILE`、`sha256_password`、MariaDB 专有扩展。
- PostgreSQL：Protocol 2.0、GSSAPI/SSPI、OAuth、逻辑/物理复制模式；GSSENC 请求会被明确拒绝并允许客户端回退到 TLS。
- Redis：单参数 `AUTH <password>`、Inline Command、Cluster/Sentinel 路由、RESP3 streamed string/streamed aggregate、模块私有协议扩展。网关需要双参数 `AUTH <compact-user> <bastion-password>` 才能唯一定位账号资源。

未列入默认矩阵的版本或扩展不代表一定无法工作，只表示不承诺回归兼容。新增版本进入支持范围前，必须先加入默认实库镜像并通过同一组场景。

## 协议依据

- [MySQL 8.4 Reference Manual：发布模型](https://dev.mysql.com/doc/refman/8.4/en/mysql-releases.html)
- [MySQL 8.4 Reference Manual：连接选项](https://dev.mysql.com/doc/refman/8.4/en/connection-options.html)
- [MySQL Connector/J：安全连接属性](https://dev.mysql.com/doc/connector-j/en/connector-j-connp-props-security.html)
- [PostgreSQL 18：协议概览](https://www.postgresql.org/docs/18/protocol-overview.html)、[消息格式](https://www.postgresql.org/docs/18/protocol-message-formats.html) 与 [SASL 认证](https://www.postgresql.org/docs/18/sasl-authentication.html)
- [PostgreSQL 16：libpq 连接参数](https://www.postgresql.org/docs/16/libpq-connect.html)
- [pgJDBC：SSL 配置](https://jdbc.postgresql.org/documentation/ssl/)
- [DBeaver：信任库设置](https://dbeaver.com/docs/dbeaver/Managing-Truststore-Settings/)
- [Redis：RESP 协议规范](https://redis.io/docs/latest/develop/reference/protocol-spec/)

## 本地与 CI 执行

需要 Docker 的完整实库测试：

```powershell
$env:JIANMEN_REQUIRE_DOCKER = '1'
go test -tags=integration ./internal/integration -count=1 -timeout=35m
```

如果本机没有 Docker，集成测试会显式跳过并给出原因；设置 `JIANMEN_REQUIRE_DOCKER=1` 后，缺少 Docker 会直接失败，CI 使用这一严格模式。

可用逗号分隔的镜像变量缩小诊断范围：

```powershell
$env:JIANMEN_MYSQL_IMAGES = 'mysql:8.4'
$env:JIANMEN_POSTGRES_IMAGES = 'postgres:18-alpine'
$env:JIANMEN_REDIS_IMAGES = 'redis:8.8-alpine'
```

协议模糊入口：

- 统一入口：`FuzzDetectUnifiedPreface`
- MySQL：`FuzzMySQLPacketFrames`
- PostgreSQL：`FuzzReadPostgresStartupMessage`、`FuzzReadPostgresTypedMessage`
- Redis：`FuzzRedisRESPFrameLength`、`FuzzRedisObserverClientFrames`、`FuzzRedisAuthenticationCommandParser`

CI 会对每个入口执行短时模糊冒烟；常规 `go test ./...` 还会执行所有种子语料。发现的新崩溃样本必须固化为种子或确定性回归测试。
