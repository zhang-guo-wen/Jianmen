# Web RDP 部署与安全边界

Jianmen 的 Web RDP 使用 Apache Guacamole 的浏览器协议和 `guacd` RDP
代理。Go 服务仍是唯一控制面，负责用户身份、`host_account`、RBAC、审批、
一次性票据、会话状态、凭据解密、审计索引和录像访问授权。

## 数据流

```text
浏览器 guacamole-common-js
        │ 一次性、用途/账号绑定的 WebSocket 票据
        ▼
Jianmen Go 控制面
        │ 二次 RBAC / 资源授权 / 审批复核
        │ 服务端下发账号凭据和通道策略
        ▼
私有网络中的 guacd
        │ RDP + 受控虚拟通道
        ▼
Windows 主机账号
```

浏览器不会收到 Windows 账号密码。`guacd` 的 4822 端口不得发布到宿主机或
公网，只允许 Jianmen 服务访问。

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

`guacd` 先把 `.guac` 图形录像写入共享临时目录。会话结束后，Jianmen：

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
    "connect_timeout_seconds": 15,
    "spool_dir": "data/rdp-spool",
    "guacd_recording_root": "data/rdp-spool",
    "local_drive_root": "data/rdp-drive",
    "guacd_drive_root": "data/rdp-drive",
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

`spool_dir` 与 `guacd_recording_root` 必须指向同一共享卷在 Go 容器和 guacd
容器中的对应路径；虚拟盘两项路径同理。目录只能由这两个服务账号读写。

## Docker Compose

仓库中的 `docker-compose.web-rdp.yml` 提供 Jianmen 与
`guacamole/guacd:1.6.0` 的最小组合，且不发布 4822 端口：

```bash
docker volume create jianmen-certs
# 先按 README 生成管理端和数据库网关证书
docker compose -f docker-compose.web-rdp.yml up -d --build
```

示例使用文件系统对象存储，适合单机验收。生产环境应复制
`config.docker.web-rdp.example.json`，切换为 S3 配置，通过只读挂载或密钥
管理系统提供凭据。文件系统模式的 `/app/data/objects` 位于 `jianmen-data`
持久卷中，不适合作为多实例共享存储；单机使用时也必须把该卷纳入备份。

Compose 中两个共享卷的容器路径如下，路径必须与
`config.docker.web-rdp.example.json` 保持一致：

| 用途 | Jianmen | guacd |
|---|---|---|
| 录像临时目录 | `/shared/recordings` | `/shared/recordings` |
| 虚拟盘目录 | `/shared/drive` | `/shared/drive` |

`guacd` 仅使用 Compose 的 `expose` 向内部网络声明 4822，不配置宿主机
`ports` 映射。若需要跨主机部署，应通过防火墙或安全组只允许 Jianmen
服务访问 guacd，并继续禁止公网入口。

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
