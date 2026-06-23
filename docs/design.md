# Jianmen 设计文档

## 1. 目标

本项目用于把当前 Teleport 的核心代理服务重写为 Go 版本。

第一版重点支持两个协议面：

- SSH Shell。
- SFTP over SSH，也就是 Xftp、WinSCP、FileZilla 这类文件客户端通常使用的协议。

必须具备的审计能力：

- 记录用户执行了什么命令。
- 记录命令对应的终端响应，支持回放和命令响应关联。
- 记录用户通过 SFTP 动了什么文件。
- 保留原始录像数据，作为结构化解析不准确时的最终证据。

第一版不引入 Linux agent、eBPF、auditd。它们适合做更高准确度的主机侧审计，但部署复杂度和内核兼容成本较高，应该作为后续增强能力。

## 2. 产品边界

### 第一版范围

- 启动 SSH Server，对外接受客户端连接。
- 支持用户、令牌或会话 ID 鉴权。
- 解析目标主机、目标账号、凭据和策略。
- 连接目标 SSH 主机。
- 代理 SSH Shell Channel。
- 代理 SFTP Subsystem Channel。
- 记录终端录像。
- 从交互式 SSH Shell 中解析命令事件。
- 解析 SFTP 请求和响应，记录文件操作事件。
- 提供审计查询和录像回放 API。

### 第一版不做

- RDP。
- Telnet。
- X11 转发。
- SSH TCP 隧道。
- 完整 Linux 主机 agent。
- eBPF/auditd 系统调用审计。
- 透明交互式 Shell 下的精确退出码。

## 3. 总体架构

```text
SSH / Xftp Client
        |
        v
Jianmen Gateway
        |
        +-- 访问控制层
        |     +-- 用户身份
        |     +-- MFA / Token 校验
        |     +-- 资产和账号解析
        |     +-- 策略引擎
        |
        +-- 协议代理层
        |     +-- SSH Server
        |     +-- SSH Target Client
        |     +-- Shell Channel Proxy
        |     +-- SFTP Channel Proxy
        |
        +-- 审计层
        |     +-- 终端录像
        |     +-- 命令解析
        |     +-- 命令响应关联
        |     +-- SFTP 文件操作解析
        |     +-- 审计事件管道
        |
        +-- 存储层
              +-- 元数据数据库
              +-- 录像文件 / 对象存储
              +-- Hash Chain / 校验摘要
```

最重要的原则：

```text
原始录像是证据，结构化事件是索引和摘要。
```

Shell 命令解析在某些边界场景下可能不准确。SFTP 通过语义层 request handler 更容易拿到明确的文件操作、路径和结果。无论结构化事件是否完整，原始终端录像必须保留。

## 4. Go 项目结构建议

```text
jianmen/
  cmd/
    bastion-core/
      main.go
  internal/
    config/
    server/
      sshserver/
    proxy/
      sshproxy/
      sftpproxy/
    access/
      auth/
      policy/
      resolver/
    audit/
      eventbus/
      terminal/
      command/
      sftp/
    recording/
      writer/
      reader/
      codec/
    storage/
      db/
      objectstore/
    model/
    api/
  docs/
    design.md
```

推荐基础库：

- `golang.org/x/crypto/ssh`：SSH Server 和 SSH Client 基础能力。
- `github.com/pkg/sftp`：可作为 SFTP 协议行为参考或部分辅助库。
- SQL 数据库：存储会话、命令、文件事件等元数据和索引。
- 文件系统、MinIO 或 S3：存储终端录像和大体积响应数据。

## 5. 连接流程

```text
1. 客户端连接 Jianmen 的 SSH 端口。
2. Bastion 校验用户、Token 或 Session ID。
3. Bastion 解析：
   - 堡垒机用户
   - 目标主机
   - 目标账号
   - 目标凭据
   - 协议权限
   - 录像策略
4. Bastion 在数据库中创建会话记录。
5. Bastion 连接目标 SSH 主机。
6. 客户端创建 Channel：
   - shell
   - subsystem: sftp
7. Bastion 创建 Channel 记录。
8. Bastion 双向代理数据。
9. 审计模块消费镜像数据流。
10. 会话关闭时，刷新录像文件并更新会话状态。
```

## 6. SSH Shell 代理

Shell Channel 本质是字节流。除非策略明确要求拦截或注入，否则代理层不要修改数据。

```text
client channel data  -> target channel
target channel data  -> client channel
target channel data  -> terminal recorder
client channel data  -> command parser
target channel data  -> command parser / response correlator
```

### 终端录像

第一版终端录像采用 asciinema v2 兼容格式，方便直接使用 `asciinema-player` 回放，也方便调试。记录目标服务端返回给客户端的输出帧，并带上相对时间偏移。

```go
type TerminalFrame struct {
    SessionID string
    ChannelID string
    OffsetMs  uint32
    Stream    string // stdout, stderr, resize
    Payload   []byte
}
```

对应落盘示例：

```json
{"version":2,"width":120,"height":40,"timestamp":1781510400}
[0.012,"o","Last login: ..."]
[1.235,"o","$ ls\r\n"]
```

窗口大小变化也要记录：

```go
type ResizeFrame struct {
    Width  uint16
    Height uint16
}
```

录像回放必须基于原始终端帧，而不是基于解析出来的命令事件。

默认只录服务端输出，不录原始按键输入，避免密码或交互式密钥被原样写入录像。命令输入通过命令解析模块结构化记录；如客户有强审计要求，可以通过配置开启 input stream recording，并在 UI 明确提示。

### 命令审计

交互式 Shell 的命令审计来自输入、回显和 prompt 的推断。

```go
type CommandEvent struct {
    ID         int64
    SessionID  string
    ChannelID  string
    Seq        int64
    Command    string
    StartedAt  int64
    EndedAt    int64
    OffsetMs   uint32
    OutputRef  string
    Preview    string
    Confidence string // exact, inferred, partial
}
```

需要明确几个限制：

- 透明代理通常无法保证拿到准确退出码。
- `vim`、`top`、`less` 这类全屏程序很难拆成清晰的命令响应。
- 脚本内部行为对 SSH 代理不可见。
- `sudo su -` 会改变目标主机用户身份，但堡垒机用户仍然是原始登录人。

所以命令事件必须有 `Confidence` 字段。原始录像才是最终证据。

### 命令响应关联

命令响应关联器维护一个状态机：

```text
waiting_prompt
waiting_input
input_editing
command_submitted
collecting_output
fullscreen_or_uncertain
```

当解析器看到客户端提交 Enter 时，创建一个待完成命令。目标服务端输出会归入该命令，直到检测到下一个 prompt。若 prompt 判断失败，则该命令标记为 `partial`。

完整响应内容不要全部塞进数据库。数据库只放摘要和 `OutputRef`，完整输出保存在录像存储里。

## 7. SFTP / Xftp 代理

Xftp 通常使用 SFTP over SSH。代理层应该把它作为 SSH 的 `sftp` subsystem 处理。

第一版建议采用语义层 SFTP 代理，而不是手写裸 SFTP 包解析。也就是：

```text
client SFTP channel
    -> pkg/sftp RequestServer
    -> audit-aware handlers
    -> remote SFTP client / remote filesystem
    -> target host
```

这样可以在 handler 层直接拿到操作语义、路径、读写 flags 和错误结果，更容易做审计和策略控制。裸包解析可作为后续 fallback，用于处理不兼容扩展或特殊客户端。

推荐模块：

```text
internal/proxy/sftpproxy/
  server.go        // 接 client channel，启动 RequestServer
  handlers.go      // FileGet/FilePut/FileCmd/FileList
  remotefs.go      // 包装目标主机 SFTP client
  audit.go         // 生成 FileEvent 和 SummaryEvent
```

语义层代理内部仍需要维护 handle 级别统计：

```text
handle -> opened file path / mode / bytes_read / bytes_written
```

### 文件事件

```go
type FileEvent struct {
    ID        int64
    SessionID string
    ChannelID string
    Seq       int64
    Action    string // open, read, write, close, remove, rename, mkdir, rmdir, opendir, setstat
    Path      string
    Path2     string
    Handle    string
    Offset    int64
    Size      int64
    Result    string // success, failure, unknown
    ErrorCode uint32
    StartedAt int64
    EndedAt   int64
}
```

第一版至少支持这些 SFTP 语义操作：

```text
open
read
write
close
remove
rename
mkdir
rmdir
opendir
readdir
stat/lstat/fstat
setstat/fsetstat
realpath
```

上传/下载总结规则：

- `open` 确定路径和访问模式。
- `read` 成功表示下载/读取。
- `write` 成功表示上传/写入。
- `close` 用于汇总该 handle 的总读写量。
- 会话结束生成 `sftp_summary`，汇总文件路径、读字节数、写字节数和失败次数。

## 8. 审计事件管道

协议代理的热路径不要直接写数据库。建议使用缓冲事件管道：

```text
proxy goroutine
    -> audit event channel
    -> session event worker
    -> recorder writer
    -> database index writer
```

这样做的好处：

- 网络代理延迟更稳定。
- 方便批量写入。
- 录像文件和数据库索引可以分开重试。
- 后续可以接 Kafka、NATS、对象存储或实时告警。

事件统一设计为 append-only：

```go
type AuditEvent struct {
    Type      string
    SessionID string
    ChannelID string
    Seq       int64
    Time      int64
    Payload   any
}
```

## 9. 存储设计

推荐录像目录结构：

```text
data/replay/
  ssh/
    000000123/
      meta.json
      terminal.cast
      terminal-events.jsonl
      commands.jsonl
      files.jsonl
      sftp-summary.json
      hash-chain.jsonl
```

### 元数据表

核心表：

```text
sessions
  id, sid, user_id, host_id, account_id,
  protocol, protocol_subtype,
  user_username, account_username,
  host_ip, conn_ip, conn_port, client_ip,
  started_at, ended_at, state

channels
  id, session_id, type, started_at, ended_at, state

command_events
  id, session_id, channel_id, seq,
  command, output_ref, preview, confidence,
  started_at, ended_at, offset_ms

file_events
  id, session_id, channel_id, seq,
  action, path, path2, handle,
  offset, size, result, error_code,
  started_at, ended_at

record_files
  id, session_id, kind, path, size, sha256, created_at
```

数据库存可检索元数据。大体积终端数据和完整命令输出放录像存储。

### 防篡改校验

每个录像帧或 JSONL 事件行都进入 hash chain：

```text
hash_n = sha256(hash_n-1 + event_bytes)
```

会话结束后，把最终 hash 写入数据库。这个机制不能防止文件被删除，但可以发现内容被修改。

## 10. 策略模型

第一版策略检查：

- 是否允许 SSH Shell。
- 是否允许 SFTP。
- 是否强制录像。
- 是否允许实时监控。
- 是否允许上传、下载、删除、重命名文件。
- 是否对危险命令进行告警或阻断。

交互式 Shell 下阻断命令比较难，因为输入编辑、别名、函数、回显都会影响判断。更稳妥的做法是：

- 在协议层阻断不支持的 Channel 类型。
- 对 SFTP 按操作类型和路径做精确阻断。
- 对 Shell 命令按高置信度模式告警或终止会话。

## 11. API 设计

给 Web 层使用的内部 API：

```text
GET /api/sessions
GET /api/sessions/{id}
GET /api/sessions/{id}/commands
GET /api/sessions/{id}/files
GET /api/sessions/{id}/replay/meta
GET /api/sessions/{id}/replay/frames?offset=...
POST /api/sessions/{id}/kill
```

运行时管理 API：

```text
POST /api/runtime/config
POST /api/runtime/reload-policy
GET  /api/runtime/health
GET  /api/runtime/metrics
```

## 12. 第一阶段里程碑

### Milestone 1：SSH 骨架

- 读取配置。
- 启动 SSH Listener。
- 支持测试用户或测试 Token 鉴权。
- 连接目标 SSH 主机。
- 打通 Shell Channel 代理。

### Milestone 2：会话元数据

- 创建 session 记录。
- 创建 channel 记录。
- 维护开始/结束状态。
- 暴露会话列表 API。

### Milestone 3：终端录像

- 记录 stdout/stderr 帧。
- 记录窗口大小变化。
- 写入 `terminal.cast`。
- 实现 replay reader。
- 提供基础录像回放 API。

### Milestone 4：命令审计

- 解析交互式输入。
- 关联命令响应区间。
- 写入 `commands.jsonl`。
- 把命令事件索引到数据库。

### Milestone 5：SFTP 代理和文件审计

- 支持 `sftp` subsystem。
- 基于 SFTP RequestServer 实现语义层代理。
- 实现远端 SFTP filesystem wrapper。
- 在 FileGet/FilePut/FileCmd/FileList handler 中生成文件审计事件。
- 维护 handle 读写统计。
- 写入 `files.jsonl`。
- 写入 `sftp-summary.json`。
- 把文件事件索引到数据库。

### Milestone 6：稳定性增强

- 背压和缓冲。
- 会话强制断开。
- 空闲超时。
- 最大会话时长。
- 录像 hash chain。
- 指标和结构化日志。

## 13. 后续增强

### Linux Agent

主机侧 agent 可以采集目标机器上的真实进程和文件事件。它适合客户要求高准确度审计的场景。

Agent 可采集：

```text
execve argv
进程树
cwd
退出码
open/write/unlink/rename/chmod
sudo 后的目标机用户变化
```

### eBPF / auditd

eBPF 或 auditd 可以采集内核级 exec 和文件事件，用于解决代理层看不见的问题：

- `sh script.sh` 隐藏脚本内部命令。
- `vim file` 难以从终端流判断是否保存了文件。
- `sudo su -` 改变目标主机身份。
- 后台进程可能在可见命令结束后继续执行。

这应该是第二阶段功能，因为它要求目标主机安装组件、授予权限、处理内核兼容和做事件关联。

## 14. 设计原则

Go 第一版应该围绕稳定的代理层证据来做：

```text
先做 SSH/SFTP 代理。
先做原始录像。
再做结构化审计。
后续再做 agent 增强。
```

这样第一版可交付，同时为后续企业级主机侧审计留下清晰扩展路径。
