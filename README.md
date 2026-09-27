# Jianmen（剑门）— 轻量化高性能堡垒机

[English](README.en.md) | 简体中文

单二进制 · 零外部依赖 · 内置 SQLite · Apache-2.0 · v1.0.0

## 背景与现状

堡垒机集中管控资产账号、代理运维访问并全程留痕，等保 2.0 要求审计留存不少于六个月。商业产品按资产计费，开源方案组件多、维护重；AI Agent 访问内网又缺凭据签发与吊销闭环。

## 功能特点

- SSH/SFTP 代理，MySQL、PostgreSQL、Redis 统一网关，浏览器 RDP
- 会话录像与命令、文件、数据库审计，支持保留期与脱敏
- 细粒度 RBAC，内置 SQLite，单二进制，0.5 核 1G 可运行
- AI Bastion API：30 分钟临时凭据、即时吊销、查询也进审计

## 界面截图

以下截图取自本地演示环境：主机、数据库与 SSH 目标均为 WSL 中运行的真实容器（MySQL 8.4 / PostgreSQL 16 / Redis 7 / OpenSSH），业务数据为演示数据。

| 登录 | 快速连接（SSH / SFTP / Web RDP） |
| --- | --- |
| ![登录页](docs/images/login.png) | ![快速连接](docs/images/quick-connect.png) |
| **主机管理** | **数据库管理（MySQL / PostgreSQL / Redis）** |
| ![主机管理](docs/images/hosts.png) | ![数据库管理](docs/images/databases.png) |
| **Web 终端（真实 SSH 会话）** | **在线 SQL（MySQL 8.4）** |
| ![Web 终端](docs/images/web-terminal.png) | ![在线 SQL - MySQL](docs/images/sql-console-mysql.png) |
| **在线 SQL（PostgreSQL 16）** | **会话命令审计** |
| ![在线 SQL - PostgreSQL](docs/images/sql-console-postgres.png) | ![会话命令审计](docs/images/audit-commands.png) |
| **SSH 会话审计** | **数据库审计** |
| ![SSH 会话审计](docs/images/audit-sessions.png) | ![数据库审计](docs/images/audit-db.png) |
| **在线会话** | **容器管理** |
| ![在线会话](docs/images/audit-online.png) | ![容器管理](docs/images/containers.png) |
| **统一权限管理** | **系统配置** |
| ![统一权限管理](docs/images/rbac.png) | ![系统配置](docs/images/system-settings.png) |

## 安装

### Docker

```shell
docker run -d --name jianmen --restart unless-stopped \
  -p 127.0.0.1:47100:47100 -p 47102:47102 -p 33060:33060 \
  -p 47110-47199:47110-47199 -v jianmen-data:/app/data \
  ghcr.io/zhang-guo-wen/jianmen:latest
```

打开 <http://127.0.0.1:47100> 初始化管理员；需 Web RDP 改用 `vX.Y.Z-rdp` 标签。

### Linux 安装包

```bash
tar -xzf jianmen-vX.Y.Z-linux-amd64-lite.tar.gz
cd jianmen-vX.Y.Z-linux-amd64-lite
cp config.example.json config.json
./jianmen -config config.json
```

RDP 包改用 `config.rdp.example.json`。勿将 47100 暴露公网；审计留存默认 30 天，等保需调至 180 天以上。

[Apache License 2.0](LICENSE)
