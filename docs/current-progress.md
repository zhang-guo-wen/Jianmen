# 当前项目进展（2026-06-23）

本文是 Jianmen 当前实现状态的权威记录。后续开发顺序和验收标准见 [phase2-roadmap.md](phase2-roadmap.md)。架构设计和调研资料分别见 [design.md](design.md) 与 [competitive-analysis.md](competitive-analysis.md)。

## 总体状态

Jianmen 当前处于 **一阶段核心代理原型 / MVP** 状态：SSH Shell 代理、SFTP 语义代理、基础录像审计、文件操作审计、Admin API、Vue 管理界面雏形、数据库 TCP 代理和明文 SQL 观察已经具备可运行基础。

但它还 **不能声明完整替代现有 Teleport**。主要缺口是：RBAC 尚未进入真实运行路径、元数据库尚未成为运行时主数据源、审计仍以文件落盘为主、Web Terminal 没有统一走 SSH proxy/SessionRecorder、主流客户端兼容性矩阵尚未完成、Web 管理后台仍是雏形。

## 默认端口

| 服务 | 默认地址 | 说明 |
| --- | --- | --- |
| Admin API | `127.0.0.1:47100` | 后端管理 API；`/` 返回 api-only JSON |
| Vue Web Admin | `127.0.0.1:47101` | Vite 开发服务，代理 `/api` 到 Admin API |
| SSH/SFTP Gateway | `0.0.0.0:47102` | SSH Shell 与 SFTP 客户端入口 |

## 已实现能力

### 启动与运行时

- `cmd/bastion-core` 负责加载配置、初始化日志、启动 SSH server、Admin server 和 DB proxy manager。
- 配置中启用 `database` 时，会打开元数据库并执行 GORM `AutoMigrate`。
- 当前真实运行路径仍使用 `access.StaticStore`，用户和目标资产主要来自配置与 `data/targets.json`。

### SSH Shell 代理

已具备真实 SSH 代理链路，不是占位实现：

- 堡垒机侧认证：密码、公钥、`authorized_keys`、keyboard-interactive。
- Host key：Ed25519，RSA fallback。
- 目标主机侧认证：密码、私钥文件、PEM 私钥、私钥口令、keyboard-interactive fallback。
- 可选目标 host key 校验。
- Channel/request 转发：`shell`、`exec`、`pty-req`、`window-change`、`env`、`signal`、stdout/stderr、`exit-status`、`exit-signal`、`eow`。
- `subsystem=sftp` 会进入 SFTP 语义代理。

当前限制：

- 只支持 `session` channel。
- 尚未看到 SSH port forwarding、X11 forwarding、agent forwarding。
- keepalive、idle timeout、session policy 等会话控制仍不完整。
- 命令审计仍是第一版启发式解析，复杂 TUI、多行粘贴、alias 展开等准确性不足。

### SFTP / 文件代理

SFTP 采用 `pkg/sftp` RequestServer 语义层代理，可以识别文件操作并写入审计。

已覆盖常见动作：`open`、`read`、`write`、`list`、`stat/lstat`、`rename`、`mkdir`、`rmdir`、`remove`、`link`、`symlink`、`setstat`、`chmod`、`chown`、`chtimes`、`realpath`，并按 handle 统计读写字节。

当前限制：

- OpenSSH 命令行客户端已完成一轮自动化冒烟：`ssh` 公钥认证、默认/指定资产、exec、shell、stdout/stderr、exit-status、审计产物通过；`sftp` 基础连接、指定资产、上传下载、目录、重命名删除、中文/空格路径、100MB 大文件、100 小文件批量和审计产物通过。详见 [compatibility-matrix.md](compatibility-matrix.md)。
- Xftp、WinSCP、FileZilla 等图形 SFTP 客户端还没有正式实测矩阵。
- OpenSSH 密码登录、keyboard-interactive、真实窗口 resize、Ctrl-C、TUI、SFTP 异常断线等场景还需要补测。
- 文件操作尚未接入完整 RBAC / 文件级策略。

### 录像与审计

SSH/SFTP 当前会输出以下文件：

- `meta.json`
- `terminal.cast`
- `terminal-events.jsonl`
- `commands.jsonl`
- `files.jsonl`
- `files-summary.json`

已具备：终端 asciinema 录像、终端事件记录、命令和输出预览记录、敏感提示场景抑制、文件操作事件记录、文件读写字节统计。

当前限制：

- 命令记录置信度仍偏 `partial`。
- 会话、命令、文件事件主要从 replay 文件系统读取，尚未完整索引到元数据库。
- hash chain 字段已有预留，但防篡改链路未产品化。
- Web 侧录像回放、命令检索、文件审计检索、文件传输摘要仍不完整。

### Admin API

后端 Admin API 已支持：

- Bearer token 认证。
- CORS。
- `GET /api/health`
- `GET /api/users`
- `GET /api/targets`
- `POST /api/targets`
- `GET /api/targets/{id}`
- `PUT /api/targets/{id}`
- `DELETE /api/targets/{id}`
- `GET /api/sessions`
- `GET /api/sessions/{id}/meta`
- `GET /api/sessions/{id}/commands`
- `GET /api/sessions/{id}/files`
- `GET /api/sessions/{id}/file-summary`
- `GET /api/db/connections`
- `GET /api/db/connections/{id}/meta`
- `GET /api/db/connections/{id}/queries`
- `GET /api/web-terminal` WebSocket 入口。

需要注意：Admin API 的 `/` 现在返回 api-only JSON，旧内嵌 HTML 管理页不是主路径。Vue 前端开发入口是 `http://127.0.0.1:47101/`。

### CLI

`bastionctl` 当前支持：

- `health`
- `users`
- `targets`
- `target-add`
- `sessions`
- `session <id> <meta|commands|files|file-summary>`
- `db`
- `db-connection <id> <meta|queries>`

仍缺少：

- `login`
- `ssh`
- `sftp`
- 完整用户、资产、权限、审计管理命令。

### Vue Web Admin

前端已使用 Vue 3 + Vite + Element Plus + Vue Router，已有以下页面：

| 页面 | 当前状态 |
| --- | --- |
| Login | 可保存 token 并跳转 |
| Dashboard | 可调用 health/users/targets/sessions/db connections 展示统计 |
| Hosts | 相对完整，支持目标资产列表、新增、编辑、删除 |
| Sessions | 列表 + artifact drawer，可查看 meta/commands/files/file-summary |
| RBAC | 静态/占位说明为主 |
| Audit | 汇总展示 session artifacts 与 DB query artifacts，仍偏轻量 |
| Web Terminal | 已有 xterm/WebSocket UI，但后端链路未统一审计 |

### Web Terminal

前后端已有 Web Terminal 雏形：前端有 xterm 风格 WebSocket 终端 UI，后端有 `/api/web-terminal` WebSocket handler。

关键限制：当前后端直接连接目标并创建 `ssh.Session`，没有走统一的 `sshproxy` / `SessionRecorder` 路径。因此 Web Terminal 与普通 SSH/SFTP 的代理、授权、录像和审计链路不一致。

后续必须调整为：

- Web Terminal 也走统一 SSH proxy。
- Web Terminal 也走统一 SessionRecorder。
- 命令、录像、文件事件与普通 SSH/SFTP 使用同一套审计模型。
- 权限检查接入统一 RBAC。

### 数据库代理

数据库代理已具备基础链路：

- 多 listener manager。
- TCP 透明转发。
- MySQL 明文协议观察：`COM_QUERY`、`COM_STMT_PREPARE`。
- PostgreSQL 明文协议观察：simple Query、Parse message。
- 可选数据库账号 allowlist。

当前限制：

- TLS 下无法看到 SQL 文本。
- MySQL SSL negotiation 场景下账号解析可能失败或拒绝。
- 未记录完整结果状态、耗时、影响行数等查询指标。
- 未实现 SQL policy engine。
- 未和 SSH/SFTP/DB 统一授权模型打通。

### RBAC 与元数据库

已有脚手架：

- GORM storage，支持 SQLite / MySQL / PostgreSQL。
- `AutoMigrate` 覆盖用户、角色、权限、资源、资源组、主机、账号、会话、命令、文件事件、录像、临时账号等模型。
- RBAC checker 支持 allow/deny、资源组成员关系，并有单元测试。

尚未进入真实运行路径：

- `main.go` 打开并 migrate metadata DB 后，没有把 DB 注入 SSH/Admin/DB proxy 的运行时 store。
- SSH server、Admin server 仍使用 `StaticStore`。
- Admin API 的 sessions/db audit 仍扫描 replay 文件系统。
- RBAC checker 尚未接入 SSH/SFTP/Admin/DB 授权流程。

## 与 Teleport 替代目标的差距

当前已覆盖 Teleport 的核心子集：

- SSH Shell 代理。
- SFTP 文件代理。
- 基础终端录像。
- 基础命令审计。
- 基础文件审计。
- 基础管理 API。
- 基础数据库明文 SQL 观察。

尚未完整覆盖：

- 完整用户管理。
- 完整角色和授权体系。
- 资产、资源组、会话、文件级策略。
- 元数据库作为真实运行数据源。
- 完整审计索引、检索和录像回放。
- Web Terminal 统一审计路径。
- 主流客户端完整兼容性验证。
- 数据库完整审计与 SQL 策略。
- MFA / OIDC / LDAP / AD 等认证集成。
- 配置迁移和旧数据兼容。
- 生产级并发、超时、异常恢复、监控告警。

## 当前状态评级

| 模块 | 当前状态 |
| --- | --- |
| SSH 基础代理 | 较完整，可系统测试 |
| SSH 高级兼容 | 不完整 |
| SSH 命令审计 | 第一版，启发式 |
| SFTP 基础代理 | 较完整，可系统测试 |
| SFTP 主流客户端兼容性 | 代码覆盖较好，验证不足 |
| 文件操作审计 | 第一版可用 |
| 终端录像 | 第一版可用 |
| Admin API | 基础可用 |
| Vue Web UI | 雏形，Hosts/Sessions 相对可用 |
| Web Terminal | 有 UI/WS 雏形，但未统一审计 |
| 数据库代理 | 明文观察可用，管控不足 |
| RBAC 模型 | 有脚手架和测试 |
| RBAC 运行时执行 | 基本未接入 |
| 元数据库产品化 | 基本未接入 |
| 完整替代 Teleport | 未达到 |

## 结论

Jianmen 目前不是空壳，SSH/SFTP 主链路、SFTP 语义代理、基础录像和审计已经做出来了，管理 API、Web 前端和数据库代理也有可运行雏形。

下一阶段应优先做四件事：

1. 完成 SSH/SFTP 主流客户端兼容性矩阵。
2. 让元数据库成为真实运行数据源。
3. 把 RBAC 接入 SSH/SFTP/Admin/DB 的真实授权路径。
4. 把 Web Terminal 纳入统一代理、录像和审计链路。

详细计划见 [phase2-roadmap.md](phase2-roadmap.md)。
