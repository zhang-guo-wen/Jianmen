# 堡垒机项目实现调研

调研对象：

- JumpServer / KoKo
- Teleport
- NextTerminal
- Warpgate

本文重点关注 SSH/SFTP 代理、终端录像、命令审计、文件审计和存储设计。

## 1. 总体结论

这几个项目的实现路线并不完全相同，但主流方向很清楚：

```text
协议代理层负责接入和转发。
录像层保留原始终端/流量证据。
审计层提取结构化事件。
存储层把大文件和可查询元数据分开。
```

对 Jianmen 最有价值的参考点：

- 终端录像优先采用 asciinema v2 / JSONL 兼容格式，而不是自定义二进制格式。
- SFTP 文件审计优先做语义层代理，也就是用 SFTP request handler 转发到远端文件系统，而不是第一版就手写原始 SFTP 包解析。
- 命令解析不要承诺完全准确，要保留 `confidence` 或类似字段。
- 录像写入要异步化，并支持本地落盘、后台上传、失败重试。
- 大型系统一般都把会话录像和结构化审计事件分开存储。

## 2. JumpServer / KoKo

### 项目定位

JumpServer 是完整 PAM 平台。KoKo 是 JumpServer 的字符协议连接器，支持 SSH、Telnet、Kubernetes、SFTP、数据库协议、Web Terminal 和 Web 文件管理。

源码和文档显示，JumpServer 是多组件架构：

```text
Core        Python / Django，管理后台、权限、审计中心
KoKo        Go + Vue，字符协议连接器
Lion        Go，RDP/VNC 图形协议连接器
Lina/Luna   Web UI / Web Terminal
Chen        Java，Web DB 连接器
```

### 录像实现

KoKo 使用 asciinema v2 风格的录像文件。

源码位置：

- `pkg/asciinema/asciinema.go`
- `pkg/proxy/recorder.go`

关键特点：

- 录像头写入 `version`、`width`、`height`、`timestamp`。
- 每条终端输出写成一行 JSON。
- 输出行格式类似：`[time, "o", data]`。
- 录像文件先写本地 `.cast`，结束后 gzip 压缩。
- 支持上传到 server、S3、OSS、Azure、OBS 等存储。
- 上传失败有重试和降级逻辑。

这说明 asciinema 兼容格式已经是堡垒机终端录像的现实选择之一。它能直接接入成熟播放器，也方便调试和文本检索。

### 命令审计

KoKo 有独立的 `CommandRecorder`：

- 命令事件先进队列。
- 批量保存。
- 高风险命令触发通知。
- 保存失败时可以降级推送到 Core 服务。

命令解析在 `pkg/proxy/parser.go`：

- 同时处理用户输入和服务端输出。
- 使用终端解析器维护屏幕状态。
- 识别 Enter、粘贴、多行命令。
- 识别 vim/screen 等全屏模式，在这些状态下减少命令解析。
- 支持命令过滤、拒绝、审批、告警。
- 识别 zmodem 上传/下载状态。

对我们的启发：

- 命令解析应该是独立模块，不应该散落在 SSH 转发逻辑里。
- 要显式识别全屏程序、zmodem、粘贴、多行输入等特殊状态。
- 命令阻断可以做，但必须建立在高置信度解析结果上。

### 文件审计

KoKo 有 `FTPFileRecorder`：

- 文件日志和文件内容记录分开。
- 文件内容先落本地，再上传到对象存储。
- 有最大文件大小限制。
- 支持分块记录。

它更偏“Web 文件管理 / SFTP 文件传输内容留存”的实现，而不仅是记录路径和动作。

对我们的启发：

- 第一版可以只做 `file_events` 元数据。
- 后续可以加“文件内容留存”，但要有大小限制、敏感策略和存储成本控制。

## 3. Teleport

### 项目定位

Teleport 是 Go 实现的身份化访问平台，支持 SSH、Kubernetes、数据库、应用、Windows Desktop 等。

它的 SSH 审计设计比普通堡垒机更完整，值得重点参考。

### 录像模式

Teleport 官方文档明确说明：

- SSH 默认录制整个 PTY output，目标是记录用户在终端里“看到的内容”。
- 默认录像不捕获输入，因此通常不会把用户输入的密码录进去。
- 默认 PTY 录像存在绕过风险，比如脚本、base64 混淆、关闭终端回显。
- 有 node/proxy 两种录制位置。
- 有同步/异步两种录制模式。
- 同步模式下记录失败会终止会话，适合强合规场景。
- 异步模式下先本地落盘，会话结束后上传，适合低延迟和弱网络环境。
- 录像存储和审计日志存储是两套后端。
- 录像格式是有序结构化事件序列，事件编码为 Protocol Buffer，整体 gzip 压缩，可选加密。

对我们的启发：

- 应该在配置里保留 `recording_mode = sync | async`。
- 第一版可以默认 async，本地落盘后上传。
- 后续强合规场景可以做 sync，记录失败即断开。
- 录像和审计事件应该分开存储。

### SFTP 文件审计

Teleport 的 SFTP 实现非常值得借鉴。

源码位置：

- `lib/srv/forward/sftp.go`

它不是简单转发裸 SFTP 字节流，而是：

```text
客户端 SFTP channel
    -> pkg/sftp RequestServer
    -> proxyHandlers
    -> remote filesystem
    -> 目标 SSH 主机
```

关键点：

- 使用 `github.com/pkg/sftp` 的 `RequestServer`。
- 实现 `FileGet`、`FilePut`、`FileCmd`、`FileList` handlers。
- handler 内部调用远端文件系统。
- 每个 handler 完成后生成 SFTP audit event。
- 会话结束时生成 `sftp_summary`，汇总每个文件的读写字节数。

这比手写 SFTP 包解析更稳：

- 不需要自己维护 request_id 解析细节。
- 能直接拿到 `req.Method`、`req.Filepath`、读写 flags。
- 更容易做权限控制和路径策略。
- 更容易得到成功/失败结果。

代价：

- 代理不再是纯字节透明隧道，而是语义层 SFTP server + remote FS client。
- 必须确保常见 SFTP 扩展和客户端行为兼容。

对我们的 Go 版建议：

```text
第一版 SFTP 采用 Teleport 这种语义层代理。
裸包解析作为补充或 fallback，而不是主路径。
```

### Enhanced Session Recording

Teleport 的增强录像使用 BPF，把非结构化终端会话转成结构化事件。

它解决的问题：

- 脚本内部命令看不到。
- base64/管道执行隐藏真实命令。
- 关闭终端 echo 后命令不出现在录像里。
- 终端流难以监控告警。

但它也有明显成本：

- 需要目标主机支持。
- 需要内核能力和权限。
- root 用户仍可能干扰。
- 和 proxy recording 模式存在边界。

这验证了我们之前的判断：

```text
第一版不做 eBPF。
等客户要求脚本/真实 exec/file syscall 审计时再加 agent。
```

## 4. NextTerminal

### 项目定位

NextTerminal 是轻量级运维审计系统，支持 RDP、SSH、VNC、Telnet、HTTP 等。

需要注意：项目 README 明确说明从 v2.0.0 起后端代码不再开源。因此它更适合作为产品形态参考，不适合作为后端源码架构参考。

### 从前端可见的审计模型

前端 API 暴露了这些对象：

- `sessions`
- `session-commands`
- `filesystem-logs`
- `ssh-gateways`

`Session` 中包含：

- protocol
- clientIp
- recording
- recordingSize
- commandCount
- auditStatus

`SessionCommand` 中包含：

- sessionId
- riskLevel
- command
- result
- createdAt

`FileSystemLog` 中包含：

- assetId
- sessionId
- userId
- action
- fileName
- createdAt

文件日志动作包括：

```text
upload
download
rename
remove
create-dir
create-file
```

### 回放实现

前端终端回放使用 `asciinema-player`。

源码位置：

- `src/pages/access/TerminalPlayback.tsx`

它从接口读取：

```text
/admin/sessions/{sessionId}/recording
```

然后用 asciinema-player 播放，并在侧边栏展示命令列表。点击命令后，根据命令时间和会话开始时间计算偏移并 seek 到对应时间点。

图形协议回放使用 Guacamole SessionRecording。

对我们的启发：

- 终端录像采用 asciinema 兼容格式，前端实现成本最低。
- 命令事件要带时间戳或 offset，方便点击命令跳转到录像位置。
- 文件系统日志应独立于会话列表，方便按资产、用户、动作检索。

## 5. Warpgate

### 项目定位

Warpgate 是 Rust 写的单二进制透明堡垒机，支持 SSH、HTTPS、Kubernetes、MySQL、PostgreSQL。它强调不需要客户端、不需要 SSH wrapper，直接暴露原生协议 listener。

### 核心架构

Warpgate 把协议、会话和录像拆得比较干净：

```text
warpgate-core
  recordings/
    terminal.rs
    traffic.rs
    writer.rs

warpgate-protocol-ssh
  server/
  client/

warpgate-db-entities
  Session
  Recording
```

### 录像模型

Warpgate 有两类 recording：

```text
terminal
traffic
```

数据库 `recordings` 表包括：

- id
- name
- started
- ended
- session_id
- kind

`TerminalRecorder` 记录：

- Input
- Output
- Error
- PtyResize

每条记录是 JSONL，数据部分用 base64 编码。它还能转换成 asciicast 风格。

`TrafficRecorder` 记录 TCP/socket 流量，并写成类似 pcap 的格式。这对未来做 SSH tunnel、数据库代理、HTTP 代理很有参考价值。

### 写入模型

`RecordingWriter` 使用 channel 异步写文件：

- 内部有 mpsc 队列。
- 写入时同时广播给 live subscriber。
- 定期 flush。
- writer 结束后更新数据库 recording 的 ended 时间。

对我们的启发：

- 录像写入要做异步 writer，不要在协议 goroutine 里直接同步写磁盘。
- 可以天然支持实时监控：writer 写文件的同时 broadcast 给订阅者。
- 可以为未来的 TCP/database 代理预留 `traffic` 类型录像。

### SSH 实现细节

Warpgate 的 SSH server handler 把所有 SSH 事件转成内部事件：

- auth
- channel open
- pty request
- shell request
- exec request
- subsystem request
- data
- extended data
- window change
- tcpip forward

会话主循环消费事件，再决定连接目标、转发数据、记录录像。

这比在 SSH callback 里直接做复杂逻辑更清晰。

对我们的启发：

```text
SSH callback 只负责协议适配。
真正的状态机、策略、审计在 session event loop 里做。
```

Warpgate 对 SFTP 没看到类似 Teleport 的文件级审计 handler，更多是把 subsystem 泛化转发。它有 SCP exec session 是否录制的开关，但不是文件级 SFTP 审计模型。

## 6. 对 Jianmen 设计的修正建议

### 6.1 终端录像格式改为 asciinema-compatible

原设计中的：

```text
terminal.trec
```

建议改成：

```text
terminal.cast
```

采用 asciinema v2 兼容格式：

```json
{"version":2,"width":120,"height":40,"timestamp":1781510400}
[0.012,"o","Last login: ..."]
[1.235,"o","$ ls\r\n"]
```

同时保留扩展事件文件：

```text
terminal-events.jsonl
commands.jsonl
files.jsonl
```

输入流策略：

- 默认不记录原始 keystroke，降低密码泄露风险。
- 只记录解析后的命令。
- 如果客户需要，可通过配置打开 input stream recording，并在 UI 明确提示。

### 6.2 SFTP 第一版采用语义层代理

原设计倾向于手写 SFTP request/response 包解析。调研 Teleport 后，建议第一版改为：

```text
client SFTP channel
    -> pkg/sftp RequestServer
    -> audit-aware handlers
    -> remote SFTP client / remote filesystem
    -> target host
```

Go 模块建议：

```text
internal/proxy/sftpproxy/
  server.go        // 接 client channel
  handlers.go      // FileGet/FilePut/FileCmd/FileList
  remotefs.go      // 包装目标机 SFTP client
  audit.go         // 生成 FileEvent 和 SummaryEvent
```

第一版重点实现：

- open/read/write/close
- mkdir/rmdir/remove/rename
- list/stat/realpath
- 按 handle 汇总 bytes_read / bytes_written
- 按请求生成成功/失败事件
- 会话结束生成 summary

### 6.3 审计事件分两类

参考 Teleport 和 Warpgate，建议分成：

```text
session recording events  用于回放
audit index events        用于检索、告警、报表
```

不要把所有数据都塞进数据库。

### 6.4 增加 sync/async 录像模式

配置建议：

```yaml
recording:
  mode: async # async | sync
  local_dir: data/replay
  upload:
    enabled: true
    backend: local # local | s3 | minio
  fail_policy: continue # continue | terminate
```

默认：

- `async`
- 写本地
- 后台上传
- 失败不影响会话

强合规：

- `sync`
- 记录失败终止会话

### 6.5 录像 writer 独立异步化

参考 Warpgate：

```text
protocol goroutine
    -> recorder channel
    -> writer goroutine
    -> file/object storage
    -> live subscribers
```

这样能同时支持：

- 低延迟代理。
- 实时监控。
- 定期 flush。
- 失败状态上报。

### 6.6 命令解析保持谨慎

参考 JumpServer 和 Teleport：

- 命令解析模块独立。
- 支持全屏程序检测。
- 支持粘贴和多行命令。
- 支持风险命令策略。
- 每条命令记录 `confidence`。
- 完整响应放录像/对象存储，数据库只存摘要。

### 6.7 预留 traffic recording

参考 Warpgate，未来如果支持：

- SSH direct-tcpip
- 数据库代理
- HTTP 代理
- Kubernetes exec/port-forward

可以新增：

```text
traffic.pcap
traffic-events.jsonl
```

第一版 SSH/SFTP 不必实现，但数据模型中可以预留 `recording.kind`。

## 7. 建议更新后的第一版架构

```text
SSH / Xftp Client
        |
        v
SSH Server Adapter
        |
        v
Session Event Loop
        |
        +-- Access Resolver / Policy Engine
        |
        +-- Shell Proxy
        |     +-- stdout/stderr -> terminal.cast
        |     +-- user input    -> command parser
        |     +-- output        -> command response correlator
        |
        +-- SFTP Semantic Proxy
        |     +-- pkg/sftp RequestServer
        |     +-- remote FS wrapper
        |     +-- file_events
        |     +-- sftp_summary
        |
        +-- Recorder Pipeline
        |     +-- async writer
        |     +-- live broadcast
        |     +-- upload worker
        |
        +-- Metadata DB
              +-- sessions
              +-- channels
              +-- recordings
              +-- command_events
              +-- file_events
```

## 8. 最终取舍

最适合 Jianmen 第一版的组合：

```text
JumpServer:
  借鉴 asciinema 录像、命令解析、命令批量保存、对象存储上传。

Teleport:
  借鉴 SFTP 语义层代理、SFTP summary、sync/async 录像模式、增强录像边界。

NextTerminal:
  借鉴产品形态：会话列表、命令列表、文件系统日志、点击命令跳转录像。

Warpgate:
  借鉴 event loop、异步 recording writer、live broadcast、terminal/traffic recording 抽象。
```

不建议第一版照搬：

- Teleport 的完整证书体系和集群模型，复杂度太高。
- JumpServer 的全组件平台化拆分，第一版会拉长战线。
- Warpgate 的多协议单二进制目标，当前需求只需要 SSH/SFTP。
- eBPF/auditd agent，应该留到第二阶段。

## 9. 参考来源

- JumpServer KoKo: https://github.com/jumpserver/koko
- JumpServer: https://github.com/jumpserver/jumpserver
- JumpServer docs: https://www.jumpserver.com/docs
- Teleport Session Recording: https://goteleport.com/docs/reference/architecture/session-recording/
- Teleport Enhanced Session Recording with BPF: https://goteleport.com/docs/enroll-resources/server-access/guides/bpf-session-recording/
- Teleport SFTP proxy source: https://github.com/gravitational/teleport/blob/master/lib/srv/forward/sftp.go
- NextTerminal: https://github.com/dushixiang/next-terminal
- NextTerminal access docs: https://docs.next-terminal.typesafe.cn/usage/access.html
- Warpgate: https://github.com/warp-tech/warpgate
- Warpgate docs: https://warpgate.null.page/docs/

