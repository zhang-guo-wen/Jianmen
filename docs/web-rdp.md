# Web RDP 部署与安全边界

Jianmen 的 Web RDP 使用 Apache Guacamole 的浏览器协议和 `guacd` RDP
代理。Go 服务仍是唯一控制面，负责用户身份、`host_account`、RBAC、审批、
一次性票据、会话状态、凭据解密、审计索引和录像访问授权。默认 Docker 部署将
Go 程序和固定版本的 `guacd` 放在同一容器中，并由 Go 进程管理其完整生命周期。

## 数据流

```text
浏览器 guacamole-common-js
        │ 一次性、用途/账号绑定的 WebSocket 票据
        ▼
Jianmen Go 控制面
        │ 二次 RBAC / 资源授权 / 审批复核
        │ 服务端下发账号凭据和通道策略
        ▼
同一容器内托管的 guacd
        │ 仅监听 127.0.0.1:4822
        │ RDP + 受控虚拟通道
        ▼
Windows 主机账号
```

浏览器不会收到 Windows 账号密码。Go 进程强制以前台模式启动 `guacd`，等待
4822 就绪、采集日志，并在退出时停止和回收它。`guacd` 只监听同一容器的回环
地址，Compose 不发布 4822。

## 权限模型

RDP 连接和高风险通道使用独立动作：

| 动作 | 能力 |
|---|---|
| `rdp:connect` | 建立 RDP 会话 |
| `rdp:clipboard:read` | 从远端读取剪贴板 |
| `rdp:clipboard:write` | 向远端写入剪贴板 |
| `rdp:file:upload` | 浏览器向远端上传文件 |
| `rdp:file:download` | 浏览器从远端下载文件 |
| `rdp:drive:map` | 映射受控虚拟盘 |
| `rdp:recording:view` | 查询并回放有权访问账号的录像 |
| `rdp:approval:manage` | 审批有权管理账号的 RDP 申请 |

每项实际能力都是“主机账号开关、RBAC 动作、账号资源授权”的交集。拥有
`rdp:connect` 不会自动获得剪贴板、文件或磁盘映射权限。账号要求审批时，
有效审批只是额外门禁，不能替代或绕过 RBAC。

Guacamole 的 RDP 文件传输建立在映射盘通道上，因此上传或下载还要求
`rdp:drive:map` 同时有效；上传、下载和磁盘映射仍是三个独立开关与 RBAC
动作，可以只允许映射盘而禁用上传、下载。

浏览器签发票据时检查一次授权，WebSocket 消费票据后、连接下游前再次检查。
票据只能使用一次，并同时绑定用途、主机账号和连接 ID。申请人不能审批自己的
访问申请；审批或账号有效期在会话中途到期时，在线会话会立即终止。即使连接因
RBAC 或审批被拒绝，也会保存一条 `denied` 审计会话。

代理仅接受规范的非负 31 位 Guacamole 对象/流编号，每个方向最多同时跟踪
256 个受控通道流；单个会话最多写入 4096 条高风险通道审计事件。超限会关闭
会话，以避免恶意客户端通过未结束的流或高频事件放大内存和数据库写入。

## 录像

`guacd` 先把 `.guac` 图形录像写入同一容器文件系统中的
`/app/data/rdp-spool`。会话结束后，Jianmen：

1. 等待 `guacd` 关闭并刷新录像；
2. 计算 SHA-256 和字节数；
3. 上传到对象存储；
4. 在数据库中保存对象键、大小、哈希和状态；
5. 上传成功后删除本机临时录像。

数据库不保存 RDP 临时目录，也不通过 API 返回对象键。录像只能通过
`/api/audit/rdp/{session_id}/recording` 读取，接口会重新校验
`rdp:recording:view` 和 `host_account` 资源授权，并支持 HTTP Range。返回录像
前还会重新核对对象大小和 SHA-256；对象被截断或篡改时拒绝回放。

录像默认不包含键盘按键事件。剪贴板内容和传输文件内容不会写入数据库；
通道审计只保存通道、方向、操作、字节数、结果和原因。

`allow_unrecorded` 默认为 `false`。无法准备审计索引或录像临时目录时，连接
会在访问 Windows 主机前失败关闭。

服务启动时会恢复上次异常退出留下的录像，运行期间也会定期重试上传状态为
`pending`、`uploading` 或 `failed` 且本地临时文件仍存在的录像；上传成功后才
删除本地文件。恢复任务会先原子占用录像，审计留存任务不会同时删除正在上传的
对象；留存到期时先删除对象存储录像，成功后才在同一数据库事务中删除录像索引、
通道事件和会话。对象删除失败时保留索引并延迟重试。
启动恢复会连续处理所有批次，不会把超过单批上限的中断会话遗留在 `active`
状态；失败录像至少间隔五分钟后再重试，避免单个故障对象阻塞后续批次。

`web_rdp.enabled: false` 只禁止签发新连接和建立 RDP 会话。历史审计查询、录像
回放、异常录像恢复和到期留存清理仍保持运行，因此对象存储配置在禁用连接时也
必须有效。

当前恢复逻辑按单个活动 Jianmen 控制面实例设计。多实例部署需先为会话和临时
文件增加实例所有权，不能让多个控制面共享同一录像临时目录。

## 配置

本地开发可以使用文件系统对象存储：

```json
{
  "web_rdp": {
    "enabled": true,
    "guacd_address": "127.0.0.1:4822",
    "managed_guacd": {
      "enabled": true,
      "binary_path": "/opt/guacamole/sbin/guacd",
      "work_dir": "/opt/guacamole",
      "startup_timeout_seconds": 15
    },
    "connect_timeout_seconds": 15,
    "spool_dir": "/app/data/rdp-spool",
    "guacd_recording_root": "/app/data/rdp-spool",
    "local_drive_root": "/app/data/rdp-drive",
    "guacd_drive_root": "/app/data/rdp-drive",
    "allow_unrecorded": false
  },
  "object_storage": {
    "provider": "filesystem",
    "local_dir": "data/objects",
    "prefix": "jianmen"
  }
}
```

生产环境建议使用独立的 S3 兼容存储：

```json
{
  "object_storage": {
    "provider": "s3",
    "endpoint": "s3.internal.example:9000",
    "access_key_id": "由密钥管理系统注入",
    "secret_access_key": "由密钥管理系统注入",
    "session_token": "",
    "bucket": "jianmen-recordings",
    "region": "us-east-1",
    "prefix": "production",
    "secure": true,
    "path_style": true,
    "auto_create_bucket": false
  }
}
```

`endpoint` 只填写 `主机名:端口`，不要包含 `http://` 或 `https://`；是否使用
TLS 由 `secure` 控制。生产环境应保持 `secure: true`，仅在隔离的开发网络中
使用明文 S3 连接。`auto_create_bucket` 默认关闭，生产环境应预先创建存储桶并
给 Jianmen 分配只允许该存储桶和前缀所需操作的专用凭据。

托管模式下，`spool_dir` 与 `guacd_recording_root` 使用同一个
`/app/data/rdp-spool`，虚拟盘两项路径使用同一个 `/app/data/rdp-drive`。
Go 和 `guacd` 在同一容器内以相同 UID/GID 运行，因此无需独立共享卷。

## Docker Compose

仓库中的 `docker-compose.web-rdp.yml` 装配并运行一个同时包含 Jianmen 和
`guacd` 的镜像。Dockerfile 基于以下不可变运行层：

```text
guacamole/guacd:1.6.0
sha256:8974eaa9ba32f713daf311e7cc8cd7e4cdfba1edea39eed75524e78ef4b08f4f
```

部署顺序不能省略：

1. 在 Windows PowerShell 的仓库根目录执行 `.\build.ps1`，生成
   `dist/bastion-core-linux-amd64`。
2. 在 WSL Docker 中按 README 的证书步骤创建并填充外部卷
   `jianmen-certs`。
3. 在 WSL 的仓库根目录执行：

   ```bash
   docker compose -f docker-compose.web-rdp.yml up -d --build
   ```

Dockerfile 只把预编译的 Linux amd64 程序装配到固定 guacd 运行层，不在 Docker
构建阶段重新下载 Go/npm 依赖。Compose 中一次性的 `volume-init` 只初始化数据
卷权限，运行时的 Go 和 `guacd` 都位于 `jianmen` 服务容器中。

示例使用文件系统对象存储，适合单机验收。生产环境应复制
`config.docker.web-rdp.example.json`，切换为 S3 配置，通过只读挂载或密钥
管理系统提供凭据。文件系统模式的 `/app/data/objects` 位于 `jianmen-data`
持久卷中，不适合作为多实例共享存储；单机使用时也必须把该卷纳入备份。

同一容器内两个进程使用完全相同的路径，必须与
`config.docker.web-rdp.example.json` 保持一致：

| 用途 | Go 进程 | guacd 进程 |
|---|---|---|
| 录像临时目录 | `/app/data/rdp-spool` | `/app/data/rdp-spool` |
| 虚拟盘目录 | `/app/data/rdp-drive` | `/app/data/rdp-drive` |

托管模式通过 `managed_guacd` 启动 `/opt/guacamole/sbin/guacd`，并从
`guacd_address` 强制生成 `-f -b <回环地址> -l <端口> -L info`。它不需要
sidecar、独立共享卷、Compose 内部网络
或 4822 端口映射。

如需复用外部 `guacd`，可将 `managed_guacd.enabled` 设为 `false`，并把
`guacd_address` 改为其私有地址。外部模式下应通过防火墙或安全组只允许
Jianmen 访问 4822，禁止发布到公网；录像与虚拟盘路径也必须在两个运行环境中
映射到同一持久存储。

## Windows 目标要求

- Windows 主机已启用远程桌面，并允许 guacd 所在网络访问目标端口。
- 主机协议选择 RDP，默认端口为 3389。
- RDP 账号仅支持密码认证，可配置域、NLA/TLS/RDP 安全模式和证书策略。
- 优先配置可信证书或固定证书指纹；只有受控环境才启用忽略证书校验。

管理 API 中的 RDP“连接测试”只能确认 `guacd` 已接受连接配置，因为标准
Guacamole 协议会在下游 FreeRDP/NLA 完成前返回 `ready`。响应会明确返回
`verification_scope: "guacd_handshake"` 和
`authentication_verified: false`；目标 Windows 账号是否可登录，以实际受审计
的 Web RDP 会话结果为准。
