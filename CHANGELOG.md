# Changelog

## [0.1.0] - 2026-06-30

### Added
- SSH Shell 代理（密码、公钥、keyboard-interactive 认证）
- SFTP 语义代理（覆盖常见文件操作，兼容 Xftp/WinSCP/FileZilla）
- 终端会话录像（asciinema v2 格式）
- 命令审计（启发式解析，confidence 标记）
- 文件操作审计（按 handle 统计读写字节）
- 数据库 TCP 代理（MySQL/PostgreSQL 明文 SQL 观察）
- Vue 3 + Element Plus Web 管理界面
- Admin REST API（Bearer token 认证）
- bastionctl 命令行管理客户端
- RBAC 权限模型（角色、资源、绑定）
- Store 接口抽象（DBStore / StaticAdapter 双实现）
- GORM 元数据库（SQLite/MySQL/PostgreSQL）
- Web Terminal（xterm + WebSocket）
- SSH/SFTP 客户端兼容性测试框架

### Changed
- 默认端口改为 47100/47101/47102 系列
- Admin API 从嵌入式 HTML 迁移到独立 Vue 前端

### Known Issues
- 命令审计对 TUI/多行粘贴场景准确性不足
- 数据库代理 TLS 下无法解析 SQL
- Web Terminal 尚未走统一 SSH proxy 审计链路
- RBAC 尚未接入 SFTP/DB 代理路径
