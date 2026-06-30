# Jianmen — 轻量级堡垒机

**Jianmen**（剑门）是一个 Go 语言编写的轻量级堡垒机（Bastion Host），提供 SSH/SFTP 代理、数据库代理、终端录像、命令审计和 Web 管理界面。

> 当前处于 MVP / 一阶段核心代理状态，尚未发布正式版本。

## 功能特性

- **SSH Shell 代理** — 密码、公钥、keyboard-interactive 认证；支持 PTY、窗口 resize、signal 转发
- **SFTP 文件代理** — 语义层代理，兼容 Xftp、WinSCP、FileZilla 等主流客户端
- **数据库代理** — MySQL/PostgreSQL TCP 透明转发 + 明文 SQL 观察
- **终端录像** — asciinema v2 兼容格式，支持回放
- **命令审计** — 交互式 Shell 命令解析，confidence 标记
- **文件审计** — SFTP 文件操作记录，按 handle 统计读写字节
- **Web 管理界面** — Vue 3 + Element Plus，主机/账号/会话/审计管理
- **REST API** — Bearer token 认证，支持 CLI 和第三方集成
- **bastionctl** — 命令行管理客户端
- **RBAC** — 角色/权限/资源绑定模型

## 快速开始

### 环境要求

- Go 1.23+
- Node.js 18+（前端开发）
- 目标主机需运行 SSH Server（用于代理连接）

### 安装与运行

```bash
# 克隆项目
git clone https://github.com/your-org/jianmen.git
cd jianmen

# 编译
go build -o bin/bastion-core.exe ./cmd/bastion-core
go build -o bin/bastionctl.exe ./cmd/bastionctl

# 准备配置
cp config.example.json config.local.json
# 编辑 config.local.json，配置堡垒机用户和目标主机信息

# 启动服务
./bin/bastion-core.exe -config config.local.json
```

启动后：

| 服务 | 地址 |
|------|------|
| Admin API | `http://127.0.0.1:47100` |
| Vue Web Admin | `http://127.0.0.1:47101` |
| SSH/SFTP Gateway | `0.0.0.0:47102` |

### 验证代理

```bash
# SSH 连接堡垒机（默认资产）
ssh -p 47102 admin@127.0.0.1

# 指定资产 ID
ssh -p 47102 admin+web01@127.0.0.1

# SFTP 连接
sftp -P 47102 admin@127.0.0.1
```

### 前端开发

```bash
cd web
npm install
npm run dev        # 开发服务 http://127.0.0.1:47101
npm run typecheck  # TypeScript 类型检查
npm run build      # 生产构建
```

## 截图

> 截图待补充。欢迎提交 PR 添加界面截图。

## 架构概览

```
SSH / SFTP / DB 客户端
        │
        ▼
┌─ Jianmen Gateway ─────────────────────┐
│  ┌─ 访问控制层 ──────────────────────┐ │
│  │  用户认证 · MFA · 资产解析 · RBAC │ │
│  ├─ 协议代理层 ──────────────────────┤ │
│  │  SSH Server · SFTP · DB Proxy     │ │
│  ├─ 审计层 ──────────────────────────┤ │
│  │  终端录像 · 命令解析 · 文件审计    │ │
│  ├─ 存储层 ──────────────────────────┤ │
│  │  元数据库 · 录像文件 · 索引        │ │
│  └────────────────────────────────────┘ │
└────────────────────────────────────────┘
        │
        ▼
   目标主机 / 数据库
```

## 项目结构

```
jianmen/
├── cmd/
│   ├── bastion-core/    # 主服务入口
│   └── bastionctl/      # CLI 管理客户端
├── internal/
│   ├── server/          # SSH/Admin/DB 服务
│   ├── proxy/           # SSH/SFTP/DB 协议代理
│   ├── audit/           # 审计事件与录像
│   ├── access/          # 认证与授权
│   ├── store/           # 数据存储接口与实现
│   └── model/           # 数据模型
├── web/                 # Vue 3 前端
├── docs/                # 设计文档与开发指南
└── config.example.json  # 配置示例
```

## 文档

- [docs/current-progress.md](docs/current-progress.md) — 当前实现进展
- [docs/phase2-roadmap.md](docs/phase2-roadmap.md) — 后续开发计划
- [docs/development.md](docs/development.md) — 本地开发与调试
- [docs/design.md](docs/design.md) — 架构设计
- [docs/competitive-analysis.md](docs/competitive-analysis.md) — 竞品调研
- [docs/compatibility-matrix.md](docs/compatibility-matrix.md) — 客户端兼容性验证

## 许可证

[MIT](LICENSE)

## 贡献

欢迎 Issue 和 PR。详见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 安全

漏洞报告请参阅 [SECURITY.md](SECURITY.md)。
