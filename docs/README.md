# Jianmen 文档索引

本文是 `docs/` 目录入口，用来说明每份文档的职责，避免进展和计划分散在多个文件里重复维护。

## 必读文档

1. [current-progress.md](current-progress.md) — 当前项目进展和真实实现状态。
2. [phase2-roadmap.md](phase2-roadmap.md) — 后续开发计划、优先级和里程碑。
3. [development.md](development.md) — 本地开发、运行、调试和手工验证命令。
4. [compatibility-matrix.md](compatibility-matrix.md) — SSH/SFTP 客户端兼容性验证结果台账。
5. [compatibility-test-plan.md](compatibility-test-plan.md) — OpenSSH、PuTTY、Xshell、SecureCRT、Xftp、WinSCP、FileZilla、IDE Remote 的手工测试步骤。
6. [compatibility-evidence-openssh-2026-06-23.md](compatibility-evidence-openssh-2026-06-23.md) — OpenSSH 命令行客户端自动化冒烟测试证据。

## 设计和参考文档

- [design.md](design.md) — 架构设计、协议代理、审计、存储和策略模型。它描述目标架构，不作为当前完成度来源。
- [competitive-analysis.md](competitive-analysis.md) — JumpServer/Teleport/NextTerminal/Warpgate 等竞品和开源实现调研。
- [implementation-plan.md](implementation-plan.md) — 早期阶段拆分和并行开发建议。当前进展与后续计划以 `current-progress.md` 和 `phase2-roadmap.md` 为准。

## 当前端口约定

| 服务 | 默认地址 |
| --- | --- |
| Admin API | `127.0.0.1:47100` |
| Vue Web Admin | `127.0.0.1:47101` |
| SSH/SFTP Gateway | `0.0.0.0:47102` |

## 维护规则

- 当前实现状态只更新 [current-progress.md](current-progress.md)。
- 后续开发计划只更新 [phase2-roadmap.md](phase2-roadmap.md)。
- 本地运行命令只更新 [development.md](development.md)。
- 客户端兼容性结论只更新 [compatibility-matrix.md](compatibility-matrix.md)；测试步骤只更新 [compatibility-test-plan.md](compatibility-test-plan.md)。
- 架构原则和长期设计放 [design.md](design.md)，不要在里面维护每日进展。
