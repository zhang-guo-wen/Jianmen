# Jianmen — 轻量级堡垒机

**Jianmen**（剑门）是一个 Go 语言编写的轻量级堡垒机（Bastion Host），提供 SSH/SFTP 代理、数据库代理、终端录像、命令审计和 Web 管理界面。

> 当前处于 内测阶段，尚未发布正式版本。

## 功能特性

### 资源与账号管理

- **主机资源管理** — 统一维护主机及其登录账号，支持分组、状态、有效期、密码与私钥认证。
- **数据库资源管理** — 管理 MySQL、PostgreSQL、Redis 实例及数据库账号，资源变更可动态刷新代理配置。
- **应用与平台账号** — 支持内网应用代理，使内网应用走堡垒机鉴权后，能通过代理被外网访问。

### 安全连接

- **SSH Shell 代理** — 支持密码、公钥和 keyboard-interactive 认证，以及 PTY、窗口 Resize、Signal 转发。
- **SFTP 文件代理** — 提供语义层文件代理，兼容 Xftp、WinSCP、FileZilla 等主流客户端。
- **多协议数据库代理** — 支持 MySQL、PostgreSQL、Redis 连接代理，统一执行身份识别、资源授权和会话控制。
- **本地 SSH 客户端** — 可配置并调用系统默认客户端、Xshell、PuTTY 等本地程序快速发起连接。
- **云端 SSH 客户端** — 可通过web快速发起ssh连接，支持tab提示词。

### 审计与追溯

- **终端录像** — 使用 asciinema v2 兼容格式记录 SSH 会话并支持在线回放。
- **命令审计** — 解析交互式 Shell 命令，保留执行时间、会话和识别置信度。
- **文件审计** — 记录 SFTP 文件操作，并按文件句柄统计上传、下载和读写字节。
- **数据库审计** — 记录数据库连接和可观察的查询事件，支持按会话检索。

### 其他

- **细粒度 RBAC** — 支持用户、用户组、角色、权限和资源授权，覆盖主机、数据库、账号、应用及资源分组。
- **跨平台构建** — 提供 Windows 与 Linux 构建脚本，可生成包含前端资源的独立二进制程序，提供docker部署。
- **开发计划** - 后续开发计划见仓库项目的看板

## 部署

### Docker 部署

拉取正式版本镜像：

```bash
docker pull ghcr.io/zhang-guo-wen/jianmen:latest
```
启动容器：

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  ghcr.io/zhang-guo-wen/jianmen:latest
```

默认端口：

| 端口 | 用途 |
|---|---|
| `47100` | Web 管理页面和管理 API |
| `47102` | SSH/SFTP 堡垒机入口 |
| `33060` | MySQL、PostgreSQL、Redis 数据库网关 |
| `47110-47199` | 内网应用动态代理端口范围 |

浏览器访问：

```text
http://服务器IP:47100
```

应用代理在用户未登录时会自动跳转到 Jianmen 登录页。默认会使用当前访问的主机名和 `admin.listen_addr` 的端口生成登录地址。如果通过域名、HTTPS 反向代理或开发服务访问，请显式配置对外管理地址：

```json
"admin": {
  "listen_addr": "0.0.0.0:47100",
  "public_url": "https://jianmen.example.com"
}
```

`admin.public_url` 只允许 HTTP/HTTPS 的站点根地址，不能包含路径、查询参数或片段。为了让登录 Cookie 在管理端口和应用代理端口之间共享，建议使用相同主机名。

容器默认使用仓库中的 `config.docker.json`。如需自定义数据库、监听地址或端口，可以挂载自己的配置文件：

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  -v /opt/jianmen/config.json:/app/config.json:ro \
  ghcr.io/zhang-guo-wen/jianmen:latest
```

升级容器时不要删除 `jianmen-data` 数据卷：

```bash
docker pull ghcr.io/zhang-guo-wen/jianmen:latest
docker rm -f jianmen
# 使用上面的 docker run 命令重新创建容器
```

建议定期备份数据卷，尤其是 `/app/data/encryption.key` 和 `/app/data/bastion.db`。加密密钥丢失后，数据库中保存的主机、数据库和平台账号凭据将无法解密。

### GitHub Release 包部署

在仓库的 GitHub Releases 页面下载与服务器架构匹配的压缩包：

| 系统 | amd64 | arm64 |
|---|---|---|
| Windows | `jianmen-vX.Y.Z-windows-amd64.zip` | `jianmen-vX.Y.Z-windows-arm64.zip` |
| Linux | `jianmen-vX.Y.Z-linux-amd64.tar.gz` | `jianmen-vX.Y.Z-linux-arm64.tar.gz` |

同时需要在服务器防火墙或云安全组中放行实际使用的端口。

## 截图
快速连接
![img.png](docs/picture/img.png)
主机和数据库管理
![img_1.png](docs/picture/img_1.png)
web终端功能
![img_7.png](docs/picture/img_7.png)
审计回放功能
![img_6.png](docs/picture/img_6.png)
ssh和xftp审计日志
![img10.png](docs/picture/img10.png)
数据库审计功能
![img_3.png](docs/picture/img_3.png)
内网应用代理功能
![img_2.png](docs/picture/img_2.png)

权限管理
![img_4.png](docs/picture/img_4.png)
![img_5.png](docs/picture/img_5.png)

## 许可证

[MIT](LICENSE)

## 贡献

欢迎贡献，或者其他合作可以加我微信 v353107440