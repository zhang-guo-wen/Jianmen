# Jianmen

这是 Teleport 核心代理服务的 Go 重写项目目录。

第一版目标：

- 支持 SSH Shell 代理。
- 支持 SFTP 代理，兼容 Xftp、WinSCP、FileZilla 等文件客户端。
- 支持终端会话录像。
- 支持命令与响应审计。
- 支持文件操作审计。
- 支持管理 Web/API。
- 支持数据库 TCP 代理和 MySQL/PostgreSQL 明文 SQL 观察。
- 支持命令行管理客户端 `bastionctl`。

当前文档入口：

- [docs/README.md](docs/README.md) — 文档索引和维护规则
- [docs/current-progress.md](docs/current-progress.md) — 当前实现进展
- [docs/phase2-roadmap.md](docs/phase2-roadmap.md) — 后续开发计划
- [docs/development.md](docs/development.md) — 本地开发与调试
- [docs/testing.md](docs/testing.md) — 单元测试与 Docker 集成测试分层
- [docs/design.md](docs/design.md) — 架构设计
- [docs/competitive-analysis.md](docs/competitive-analysis.md) — 竞品/开源实现调研
