# 二阶段开发计划（2026-06-23）

本文是 Jianmen 后续开发计划的权威记录。当前已实现能力和限制见 [current-progress.md](current-progress.md)。本文不重复维护详细状态，只描述接下来要做什么、按什么顺序做、做到什么程度算完成。

## 阶段目标

二阶段目标：把一阶段 SSH/SFTP/DB 代理原型推进为 **可内测的可管理堡垒机**。

可内测定义：

- 代理链路稳定，主流客户端完成兼容性验证。
- 用户、资产、角色、权限进入元数据库。
- SSH/SFTP/Admin/DB 的关键操作都经过 RBAC 授权。
- 审计事件可查询、可回放、可追溯。
- Web Terminal 与普通 SSH 客户端使用统一代理和审计路径。

## 默认本地端口

| 服务 | 默认地址 | 用途 |
| --- | --- | --- |
| Admin API | `127.0.0.1:47100` | 后端管理 API |
| Vue Web Admin | `127.0.0.1:47101` | 前端开发服务 |
| SSH/SFTP Gateway | `0.0.0.0:47102` | SSH/SFTP 客户端入口 |

## P0：替代 Teleport 前必须补齐

### 1. 客户端兼容性矩阵

目标：确认 SSH/SFTP 主链路在主流客户端中可用，并把结果固化成手工/自动测试记录。测试结果统一维护在 [compatibility-matrix.md](compatibility-matrix.md)，执行步骤统一维护在 [compatibility-test-plan.md](compatibility-test-plan.md)。

待验证客户端：

- OpenSSH `ssh` / `sftp`
- PuTTY
- Xshell
- SecureCRT
- Xftp
- WinSCP
- FileZilla
- VS Code Remote / JetBrains 远程类客户端（至少验证可连接性和已知限制）

重点场景：

- 密码认证。
- 公钥认证。
- keyboard-interactive。
- Shell 登录、命令执行、PTY resize。
- SFTP 上传、下载、覆盖、删除、重命名、目录操作。
- 中文路径、空格路径、特殊字符路径。
- 大文件、批量文件、异常断线。

验收标准：

- `docs/compatibility-matrix.md` 记录每个客户端、版本、系统、测试项和结论。
- 关键失败项有 issue/TODO 和可复现步骤。
- 至少 OpenSSH、Xftp、WinSCP、FileZilla 的核心路径通过。

### 2. 元数据库成为真实运行数据源

目标：从 `StaticStore` / 文件索引过渡到 DB-backed store。

工作项：

- 定义 store/repository 接口，隔离配置源、文件源和 DB 源。
- 用户、用户公钥、资产、账号、角色、权限、资源组写入元数据库。
- Admin API 从元数据库读写用户、资产、角色和权限。
- 会话、命令、文件、DB 查询审计索引写入元数据库。
- replay 大文件继续留在文件系统或对象存储，DB 存路径、大小、hash、时间等索引。
- 提供从 `config.example.json` / `data/targets.json` 导入初始数据的迁移命令或启动逻辑。

验收标准：

- `bastion-core` 启动后，SSH/Admin/DB proxy 不再依赖 `StaticStore` 作为主运行数据源。
- 关闭 replay 文件扫描后，Admin API 仍能列出会话和审计索引。
- SQLite 默认可用，MySQL/PostgreSQL 连接和 migration 有测试覆盖。

### 3. RBAC 接入真实代理路径

目标：权限不只存在于 checker 和测试中，而是成为运行时强制执行路径。

授权点：

- SSH 登录授权：用户是否允许登录网关。
- 资产连接授权：用户是否允许连接目标 host / host group。
- 账号使用授权：用户是否允许使用目标 host account。
- SFTP 操作授权：读、写、删除、重命名、chmod/chown 等。
- DB 连接授权：用户是否允许访问 database proxy / database account。
- Admin API 授权：用户是否允许查看、创建、更新、删除资源。

验收标准：

- 没有授权的用户无法连接默认目标。
- SFTP 写入/删除可以被策略阻断并产生审计事件。
- Admin API 不再只有全局 token，至少具备用户身份和动作权限检查的基础模型。
- RBAC checker 有集成测试覆盖 SSH/SFTP/Admin 的关键路径。

### 4. Web Terminal 统一代理与审计

目标：Web Terminal 不再绕过普通 SSH 代理和 SessionRecorder。

工作项：

- Web Terminal 连接应进入统一 SSH proxy，而不是直接 `ssh.Dial` 目标主机后 `NewSession`。
- Web Terminal 会话写入同一套 `meta.json`、`terminal.cast`、`commands.jsonl`。
- Web Terminal 使用同一套 RBAC 授权。
- 前端支持选择目标资产、resize、断开、错误提示。

验收标准：

- Web Terminal 产生的会话能在 Sessions/Audit 中查询和回放。
- Web Terminal 与 OpenSSH 客户端的审计格式一致。
- 未授权目标无法在 Web Terminal 中连接。

### 5. 审计产品化

目标：审计不仅能落文件，还能被管理端可靠查询和回放。

工作项：

- 会话索引列表：分页、过滤、按用户/资产/时间查询。
- 命令审计：按命令、用户、资产、时间查询。
- 文件审计：按路径、动作、用户、资产、时间查询。
- DB 查询审计：按 SQL、用户、代理、时间查询。
- 录像回放：基于 `terminal.cast` 的播放器。
- 防篡改：hash chain 或至少 session artifact sha256 校验。

验收标准：

- Web UI 可完成基本审计检索和录像回放。
- 审计数据路径清晰：DB 存索引，文件/对象存储存大文件。
- 关键审计 artifact 有 hash 或完整性校验。

## P1：重要增强

### 1. 完整管理面

- 用户 CRUD。
- 资产 CRUD。
- 目标账号 CRUD。
- 角色/权限/资源组管理。
- 数据库代理配置管理。
- 操作审计：记录谁修改了用户、资产、策略。

### 2. `bastionctl` 增强

- `bastionctl login`，本地保存 token/session。
- `bastionctl ssh <asset>`。
- `bastionctl sftp <asset>`。
- 用户、资产、角色、权限、审计查询命令。

### 3. 数据库代理增强

- MySQL 登录阶段解析用户名、默认库、连接属性。
- PostgreSQL startup 参数解析用户名、database、application_name。
- SQL 审计补充结果：成功/失败、耗时、影响行数。
- SQL 策略：只读、禁止 DDL、禁止 DROP/TRUNCATE、敏感表拦截。
- TLS 策略：禁止 TLS、透传 TLS、企业证书中间人三种模式择一或组合。

### 4. 会话稳定性

- keepalive。
- idle timeout。
- 最大会话时长。
- 异常断线清理。
- 后台 goroutine 生命周期管理。
- 录像 writer 背压和失败处理。

## P2：生产化能力

- 高并发压测。
- 长连接稳定性测试。
- Prometheus metrics / health checks。
- 结构化日志和告警。
- HA 部署方案。
- 配置迁移和旧 Teleport 数据兼容。
- MFA / OIDC / LDAP / AD。
- 安全加固和威胁建模。
- 录像对象存储：S3/MinIO/OSS。

## 推荐里程碑

### M1：可系统测试

目标：代理链路和当前功能可以被系统性验证。

- 默认端口改为 47100/47101/47102。
- 完成 SSH/SFTP 兼容性矩阵文档。
- 修复兼容性矩阵中的 P0 阻断问题。
- Go 单元测试、Web build 通过。

### M2：可内测

目标：管理和权限进入真实运行路径。

- 元数据库成为主数据源。
- 用户、资产、角色、权限基础 CRUD。
- RBAC 接入 SSH/SFTP/Admin。
- 审计索引写入 DB。
- Web Terminal 统一审计路径。

### M3：可生产试点

目标：具备小范围生产试点能力。

- 审计检索和录像回放产品化。
- 数据库代理 SQL 策略基础版。
- 防篡改校验。
- 稳定性和长连接测试通过。
- MFA/OIDC 至少完成一种企业认证集成。
- 运维监控、日志和备份恢复方案。

## 暂不进入二阶段主线

以下能力价值高，但不应阻塞二阶段内测目标：

- Linux Agent。
- eBPF / auditd。
- RDP / VNC。
- Kubernetes 协议代理。
- Web 文件管理器。
- 完整数据库结果集审计和脱敏。

这些能力可以作为三阶段或独立插件规划。
