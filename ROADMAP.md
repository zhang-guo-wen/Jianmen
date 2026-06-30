# Roadmap

详细计划见 [docs/phase2-roadmap.md](docs/phase2-roadmap.md)。本文提供高层摘要。

## 当前阶段：MVP / 一阶段核心代理

SSH Shell 代理、SFTP 语义代理、基础录像审计、Admin API、Vue 管理界面、数据库 TCP 代理已具备可运行基础。

## 二阶段目标：可内测的可管理堡垒机

### M1：可系统测试 ✅ 基本完成
- [x] 默认端口 47100/47101/47102
- [x] SSH/SFTP 兼容性矩阵文档
- [ ] 修复兼容性矩阵中的 P0 阻断问题
- [x] Go 测试、前端 build 通过

### M2：可内测（进行中）
- [ ] 元数据库成为主数据源
- [ ] 用户、资产、角色、权限基础 CRUD
- [ ] RBAC 接入 SSH/SFTP/Admin 真实路径
- [ ] 审计索引写入 DB
- [ ] Web Terminal 统一审计路径

### M3：可生产试点
- [ ] 审计检索和录像回放产品化
- [ ] 数据库代理 SQL 策略
- [ ] 防篡改校验
- [ ] 长连接稳定性测试
- [ ] MFA / OIDC 企业认证集成
- [ ] 运维监控和告警

## P1：重要增强
- 完整管理面（用户/资产/账号/RBAC CRUD）
- bastionctl 增强（login/ssh/sftp 子命令）
- 数据库代理增强（SQL 策略、TLS 策略）
- 会话稳定性（keepalive、timeout、异常清理）

## P2：生产化
- 高并发压测
- Prometheus metrics
- HA 部署方案
- 安全加固和威胁建模
- 录像对象存储（S3/MinIO/OSS）

## 暂不纳入
- Linux Agent / eBPF / auditd
- RDP / VNC
- Kubernetes 协议代理
