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
- **Web RDP** — 通过 Apache Guacamole 在浏览器访问 Windows，凭据留在服务端，连接、剪贴板、上传、下载和磁盘映射分别授权。

### 审计与追溯

- **终端录像** — 使用 asciinema v2 兼容格式记录 SSH 会话并支持在线回放。
- **命令审计** — 解析交互式 Shell 命令，保留执行时间、会话和识别置信度。
- **文件审计** — 记录 SFTP 文件操作，并按文件句柄统计上传、下载和读写字节。
- **数据库审计** — 记录数据库连接和可观察的查询事件，支持按会话检索。
- **RDP 图形录像** — guacd 生成 `.guac` 录像，上传对象存储；审计页可按用户、主机账号、时间和结果筛选并回放。

### 其他

- **细粒度 RBAC** — 支持用户、用户组、角色、权限和资源授权，覆盖主机、数据库、账号、应用及资源分组。
- **跨平台构建** — 提供 Windows 与 Linux 构建脚本，可生成包含前端资源的独立二进制程序，提供docker部署。
- **开发计划** - 后续开发计划见仓库项目的看板

## 部署

Web RDP 需要独立的 `guacd`，其 4822 端口只能位于私有网络。完整权限边界、
对象存储配置和 Compose 示例见 [Web RDP 部署与安全边界](docs/web-rdp.md)。

### Docker 部署

拉取正式版本镜像：

```bash
docker pull ghcr.io/zhang-guo-wen/jianmen:latest
```
默认容器配置要求管理端使用 TLS；缺少 `/app/certs/admin.crt` 或
`/app/certs/admin.key` 时会安全退出，不会自动降级为明文 HTTP。首次本机评估可先在
Docker 数据卷中生成一套临时自签名证书：

```bash
docker volume create jianmen-certs
docker run --rm --user 0 \
  -v jianmen-certs:/certs \
  alpine:3.23 sh -c \
  'apk add --no-cache openssl &&
   openssl req -x509 -newkey rsa:3072 -nodes -days 30 \
     -keyout /certs/admin.key -out /certs/admin.crt \
     -subj "/CN=localhost" \
     -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" &&
   openssl req -x509 -newkey rsa:3072 -nodes -days 30 \
     -keyout /tmp/database-ca.key -out /certs/database-ca.crt \
     -subj "/CN=Jianmen local database CA" \
     -addext "basicConstraints=critical,CA:TRUE" \
     -addext "keyUsage=critical,keyCertSign,cRLSign" &&
   openssl req -new -newkey rsa:3072 -nodes \
     -keyout /certs/database.key -out /tmp/database.csr \
     -subj "/CN=localhost" &&
   printf "%s\n" \
     "basicConstraints=critical,CA:FALSE" \
     "keyUsage=critical,digitalSignature,keyEncipherment" \
     "extendedKeyUsage=serverAuth" \
     "subjectAltName=DNS:localhost,IP:127.0.0.1" >/tmp/database.ext &&
   openssl x509 -req -in /tmp/database.csr \
     -CA /certs/database-ca.crt -CAkey /tmp/database-ca.key -CAcreateserial \
     -out /certs/database.crt -days 30 -sha256 -extfile /tmp/database.ext &&
   rm -f /certs/database-ca.srl /tmp/database-ca.key /tmp/database.csr /tmp/database.ext &&
   chown 10001:10001 /certs/admin.key /certs/admin.crt /certs/database.key /certs/database.crt /certs/database-ca.crt &&
   chmod 600 /certs/admin.key /certs/database.key &&
   chmod 644 /certs/admin.crt /certs/database.crt /certs/database-ca.crt'
```

随后启动容器；管理端仍只映射到宿主机回环地址：

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 127.0.0.1:47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 54330:54330 \
  -p 63790:63790 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  -v jianmen-certs:/app/certs:ro \
  ghcr.io/zhang-guo-wen/jianmen:latest
```

默认端口：

| 端口 | 用途 |
|---|---|
| `47100` | Web 管理页面和管理 API |
| `47102` | SSH/SFTP 堡垒机入口 |
| `33060` | MySQL 数据库网关 |
| `54330` | PostgreSQL 数据库网关（TLS 必须） |
| `63790` | Redis 数据库网关（远程 AUTH 必须使用 TLS） |
| `47110-47199` | 内网应用动态代理端口范围 |

### 数据库网关 TLS 身份校验

MySQL 和 PostgreSQL 网关应同时配置服务端证书、私钥、公共 CA 文件与客户端验证名称。`server_name` 必须是证书 SAN 中的 DNS 名称或 IP；客户端连接命令使用该名称，而不是监听地址。`ca_file` 仅保存可公开分发的 CA PEM。只有单张、当前有效且可验证的自签名叶证书才允许省略 `ca_file`，此时按证书固定（pin）语义分发；普通 CA 签发的叶证书不能被当作根证书。`key_file` 永不会通过 API 返回。

```json
"postgresql": {
  "enabled": true,
  "listen_addr": "0.0.0.0:54330",
  "cert_file": "/app/certs/database.crt",
  "key_file": "/app/certs/database.key",
  "ca_file": "/app/certs/database-ca.crt",
  "server_name": "localhost"
}
```

快速连接会提供 CA 下载、CA 内容和证书 SHA-256 指纹，并且只生成强校验命令：PostgreSQL 使用 `sslmode=verify-full sslrootcert=...`，MySQL 使用 `--ssl-mode=VERIFY_IDENTITY --ssl-ca=...`。TLS 身份材料不完整时不会降级为 `require` 或 `REQUIRED` 命令。

上面的本机评估流程生成的数据库叶证书 SAN 包含 `localhost` 和 `127.0.0.1`，与默认配置的 `server_name: "localhost"` 一致。生产环境使用其他网关域名时，必须同时替换 `server_name`，并重新签发包含该 DNS 名称 SAN 的数据库叶证书。

浏览器访问：

```text
https://127.0.0.1:47100
```

自签名证书只适合本机评估，浏览器会提示该证书不受信任；生产环境应挂载由受信任 CA
签发的证书。应用代理在用户未登录时会自动跳转到 Jianmen 登录页。默认会使用当前访问的
主机名和 `admin.listen_addr` 的端口生成登录地址。

如在隔离 Docker 网络内由 Caddy 等反向代理终止 TLS，可使用仓库提供的
`config.docker.proxy.example.json`。下面的完整示例不会把容器内的明文 `47100` 发布到
宿主机；使用前请把示例域名替换为真实域名并完成 DNS 解析：

```bash
mkdir -p /opt/jianmen
cp config.docker.proxy.example.json /opt/jianmen/config.json
sed -i 's/jianmen\.example\.com/your.real.domain/g' /opt/jianmen/config.json
printf '%s\n' \
  'your.real.domain {' \
  '  reverse_proxy jianmen:47100' \
  '}' > /opt/jianmen/Caddyfile

docker network create jianmen-internal
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  --network jianmen-internal \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 54330:54330 \
  -p 63790:63790 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  -v jianmen-certs:/app/certs:ro \
  -v /opt/jianmen/config.json:/app/config.json:ro \
  ghcr.io/zhang-guo-wen/jianmen:latest
docker run -d \
  --name jianmen-caddy \
  --restart unless-stopped \
  --network jianmen-internal \
  -p 80:80 -p 443:443 \
  -v /opt/jianmen/Caddyfile:/etc/caddy/Caddyfile:ro \
  -v jianmen-caddy-data:/data \
  caddy:2-alpine
```

`admin.public_url` 只允许 HTTP/HTTPS 的站点根地址，不能包含路径、查询参数或片段。为了让登录 Cookie 在管理端口和应用代理端口之间共享，建议使用相同主机名。

Admin 管理端默认仅允许回环地址使用 HTTP，适合本机开发。非回环监听必须配置证书和私钥，或显式设置 `admin.tls.allow_insecure_http: true`；后者只适用于受控的开发环境，不应作为生产部署方案。启用内置 TLS 的配置示例：

```json
"admin": {
  "listen_addr": "0.0.0.0:47100",
  "public_url": "https://jianmen.example.com",
  "tls": {
    "cert_file": "/app/certs/admin.crt",
    "key_file": "/app/certs/admin.key",
    "allow_insecure_http": false
  }
}
```

镜像内置的 `config.docker.json` 默认要求证书，且不会启用
`allow_insecure_http`。只有类似上述代理示例、容器明文端口不离开受控内部网络时，才可在
挂载的自定义配置中显式打开该开关；不得把这种配置下的 `47100` 发布到公网。

新增或编辑应用时，只需填写完整应用地址，例如 `http://47.121.184.68:18848/nacos/#/login`。系统会自动解析协议、主机、端口和默认访问路径，并在应用列表中生成可复制、可直接打开的代理访问地址。

容器默认使用仓库中的 `config.docker.json`。如需自定义数据库、监听地址或端口，可以挂载自己的配置文件：

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 127.0.0.1:47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 54330:54330 \
  -p 63790:63790 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  -v jianmen-certs:/app/certs:ro \
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
