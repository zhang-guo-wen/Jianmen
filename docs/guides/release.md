# 容器与版本发布

## 容器镜像

普通分支推送和拉取请求只执行 CI 检查，不会发布容器镜像或 GitHub Release。推送
`v1.2.3`、`v1.2.3-rc.1` 这类语义化版本 Tag 后，发布流程会生成多架构容器镜像和
版本压缩包。镜像支持 `linux/amd64` 和 `linux/arm64`。

容器镜像自动发布以下两类变体：

- Lite：默认镜像，不带 Web RDP 运行时
- RDP：带 Web RDP / guacd 运行时

标签规则如下：

- `vX.Y.Z` 等价于 `vX.Y.Z-lite`
- `vX.Y.Z-rdp` 表示带远程桌面运行时的镜像
- 只有 Lite 会更新 `latest`
- 预发布版本不会更新 `latest`，也不会生成 `major.minor` 兼容标签

默认部署不需要准备或挂载证书。管理端在容器内提供 HTTP，生产环境应由 Nginx、Caddy
等反向代理终止 HTTPS；本地评估时只将管理端端口绑定到宿主机回环地址。默认示例使用
Lite 镜像，也就是无后缀版本标签或 `latest`：

```bash
docker run -d \
  --name jianmen \
  --restart unless-stopped \
  -p 127.0.0.1:47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  ghcr.io/zhang-guo-wen/jianmen:latest
```

如需浏览器远程桌面，请改用同版本的 `-rdp` 标签，例如：

```bash
docker run -d \
  --name jianmen-rdp \
  --restart unless-stopped \
  -p 127.0.0.1:47100:47100 \
  -p 47102:47102 \
  -p 33060:33060 \
  -p 47110-47199:47110-47199 \
  -v jianmen-data:/app/data \
  ghcr.io/zhang-guo-wen/jianmen:vX.Y.Z-rdp
```

容器默认入口如下：

- Web 管理端（仅限本地评估）：`http://127.0.0.1:47100`
- SSH 网关：`主机地址:47102`
- 统一数据库网关（默认，MySQL/PostgreSQL/Redis）：`主机地址:33060`
- 独立 MySQL 网关：`主机地址:33061`
- 独立 PostgreSQL 网关：`主机地址:33062`
- 独立 Redis 网关：`主机地址:33063`
- 应用代理入口：`主机地址:47110-47199`

如果 `configs/config.docker.json` 的默认设置不适用，请将自定义配置文件挂载到
`/app/config.json`。使用反向代理部署时，将 Jianmen 容器和代理置于隔离的 Docker
网络中，不要向公网直接发布 Jianmen 的 `47100` 端口。完整的 Caddy 命令和 Nginx
Stream 数据库网关注意事项见 `README.md`。

默认 `unified` 模式允许 MySQL、PostgreSQL 和 Redis 原生客户端共用 `33060` 端口。
MySQL 新连接需要等待 200 毫秒的协议探测窗口，但已建立会话的吞吐量不受影响。
`independent` 模式分别使用 `33061`、`33062` 和 `33063`，系统只监听当前选定模式的
端口。默认容器命令只发布 `33060`；切换到 `independent` 模式时，必须增加
`33061:33061`、`33062:33062` 和 `33063:33063` 端口映射，再重启容器。

面向客户端的 TLS 支持两种策略：默认的 `optional` 同时接受 MySQL、PostgreSQL 和
Redis 明文连接及 TLS 连接，`required` 会拒绝明文认证和数据库流量。两种模式没有
配置证书时，Jianmen 都会在 `/app/data` 中自动生成并持续复用托管证书，因此无需单独
的证书卷；显式配置证书时优先使用用户证书。PostgreSQL 的明文 `CancelRequest` 控制包
仍保持兼容，因为它不携带登录凭据或数据库数据，并且必须匹配对应会话的取消密钥。

使用公共 CA 签发的证书时，应配置叶证书在前的完整证书链 `cert_file`、匹配的
`key_file`，以及被证书 SAN 覆盖的 `server_name`，同时省略 `ca_file`。Jianmen 启动
时会使用运行环境的系统证书池验证证书链；如果证书链、有效期、密钥用途或主机名无效，
服务将安全失败关闭。证书文件必须包含验证所需的全部中间证书。

网关 API 会报告已验证身份使用的是 `custom` 还是 `system` 信任模式，但绝不会暴露
私钥材料。DBeaver 使用 Java 默认信任库验证系统信任证书。`psql` 使用系统信任需要
libpq 16 或更高版本；旧版客户端和 MySQL 原生命令行仍需要显式指定 CA 文件。任何
客户端连接路径都不会静默降级为“只加密但不验证身份”。

## GitHub Release

创建并推送语义化版本 Tag，即可构建和发布版本压缩包：

```bash
git tag v0.1.0
git push origin v0.1.0
```

发布工作流会将 Vue 前端嵌入 Go 二进制文件，并生成以下压缩包：

- Windows amd64
- Windows arm64
- Linux amd64 Lite（不内嵌 guacd 运行时）
- Linux amd64 RDP（内嵌可自解压的 guacd 运行时）
- Linux arm64 Lite（不内嵌 guacd 运行时）
- Linux arm64 RDP（内嵌可自解压的 guacd 运行时）

每个压缩包都包含可执行文件、`config.example.json`、`README.md` 和 `LICENSE`。RDP
压缩包还包含启用内嵌 guacd 的 `config.rdp.example.json` 和 `THIRD_PARTY_NOTICES.md`。
Release 中的 `checksums.txt` 保存所有文件的
SHA-256 校验值。RDP 压缩包基于锁定版本的 guacd 官方镜像构建，目标主机无需安装
Docker，也无需预先安装 guacd。
