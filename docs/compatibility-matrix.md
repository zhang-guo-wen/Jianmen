# 兼容性矩阵（SSH / SFTP 客户端）

更新日期：2026-06-23

本文用于记录 Jianmen 与主流 SSH/SFTP 客户端的兼容性验证结果。它是二阶段 P0 的测试台账：只有在这里有明确环境、步骤、结果和证据的条目，才能声明对应客户端兼容通过。

## 端口与默认环境

| 服务 | 默认地址 |
| --- | --- |
| Admin API | `http://127.0.0.1:47100/` |
| Vue Web Admin | `http://127.0.0.1:47101/` |
| SSH/SFTP Gateway | `127.0.0.1:47102` |
| 示例堡垒机用户 | `admin` / `admin` |
| 示例默认资产 | `target-local` |
| 指定资产用户名格式 | `admin+target-local` |

测试前建议先复制 `config.example.json` 为 `config.local.json`，并把 `targets[0]` 调整为当前可登录的测试 SSH 目标。

## 状态定义

| 状态 | 含义 |
| --- | --- |
| `PASS` | 已按本文测试项完成验证，未发现阻断问题 |
| `PARTIAL` | 核心链路可用，但存在非阻断兼容问题或缺少部分测试项 |
| `FAIL` | 发现阻断问题，客户端或关键场景不可用 |
| `BLOCKED` | 环境缺失或前置问题导致无法测试 |
| `NOT_TESTED` | 尚未执行正式验证 |
| `N/A` | 该客户端不适用该测试项 |

## 当前总览

> 当前结论：OpenSSH 命令行客户端已完成一轮自动化冒烟验证，SSH/SFTP 公钥认证主链路通过；OpenSSH SFTP 的基础连接、文件操作、中文/空格路径、100MB 大文件和 100 小文件批量传输通过；密码登录、keyboard-interactive、真实窗口 resize、Ctrl-C、TUI 和异常断线仍需补测。图形客户端尚未正式验证，因此不能宣称整体兼容通过。

| 客户端 | 类型 | 优先级 | 当前状态 | 最近验证日期 | 记录人 | 备注 |
| --- | --- | --- | --- | --- | --- | --- |
| OpenSSH `ssh` | SSH Shell | P0 | `PARTIAL` | 2026-06-23 | Claude | OpenSSH_for_Windows_8.6p1；公钥认证、默认/指定资产、exec、shell、stdout/stderr、exit-status、客户端长命令中断后服务可用、审计产物通过；密码、keyboard-interactive、真实 resize/Ctrl-C/TUI 未测 |
| OpenSSH `sftp` | SFTP | P0 | `PARTIAL` | 2026-06-23 | Claude | OpenSSH_for_Windows_8.6p1；基础连接、指定资产、上传下载、目录、重命名删除、stat/chmod、中文/空格路径、100MB 大文件、100 小文件批量、客户端中断、审计产物通过；chown 待 Linux 目标补测 |
| PuTTY | SSH Shell | P0 | `NOT_TESTED` | - | - | Windows 常用 SSH 客户端 |
| Xshell | SSH Shell | P0 | `NOT_TESTED` | - | - | 企业 Windows 常用 SSH 客户端 |
| SecureCRT | SSH Shell | P1 | `NOT_TESTED` | - | - | 企业 SSH 客户端，授权环境可测 |
| Xftp | SFTP | P0 | `NOT_TESTED` | - | - | 企业 Windows 常用 SFTP 客户端 |
| WinSCP | SFTP | P0 | `NOT_TESTED` | - | - | Windows 常用 SFTP 客户端 |
| FileZilla | SFTP | P0 | `NOT_TESTED` | - | - | 跨平台常用 SFTP 客户端 |
| VS Code Remote SSH | IDE Remote | P1 | `NOT_TESTED` | - | - | 先验证可连接性和已知限制 |
| JetBrains Remote Development | IDE Remote | P2 | `NOT_TESTED` | - | - | 先验证可连接性和已知限制 |

## 通用前置检查

### 1. 启动后端

```powershell
& "C:\Program Files\Go\bin\go.exe" run .\cmd\bastion-core -config config.local.json
```

或使用已编译二进制：

```powershell
.\bin\bastion-core.exe -config config.local.json
```

### 2. 验证 Admin API

```powershell
.\bin\bastionctl.exe -token dev-admin-token health
.\bin\bastionctl.exe -token dev-admin-token targets
```

预期：`health` 返回 `status=ok`，`targets` 中能看到测试目标。

### 3. 清理旧审计记录（可选）

为了方便核对新会话，可在测试前备份或清理 `data/replay/`。不要在需要保留证据的环境里直接删除。

## SSH Shell 测试项

| ID | 场景 | 命令/操作 | 预期结果 | 证据 |
| --- | --- | --- | --- | --- |
| SSH-01 | 默认目标密码登录 | `ssh -p 47102 admin@127.0.0.1` | 可登录目标主机 shell | 终端截图、`meta.json` |
| SSH-02 | 指定资产密码登录 | `ssh -p 47102 admin+target-local@127.0.0.1` | 登录指定资产 | 终端截图、`meta.json` 中 target 正确 |
| SSH-03 | 公钥登录 | 使用配置中的 public key / authorized_keys | 可免密或用私钥口令登录 | 终端截图、认证方式记录 |
| SSH-04 | keyboard-interactive | 客户端启用 keyboard-interactive | 可完成登录 | 客户端日志/截图 |
| SSH-05 | exec 命令 | `ssh -p 47102 admin@127.0.0.1 whoami` | stdout 正常，退出码合理 | 命令输出、`commands.jsonl` |
| SSH-06 | PTY 与窗口变化 | 登录后调整终端窗口，执行 `stty size` | 终端可用，resize 不断连 | `terminal-events.jsonl` |
| SSH-07 | Ctrl-C / signal | 执行长命令后 Ctrl-C | 目标进程中断，会话不异常退出 | 终端截图 |
| SSH-08 | stderr / exit-status | 执行会产生 stderr 和非 0 退出码的命令 | stderr 透传，客户端能结束 | 终端输出 |
| SSH-09 | 交互程序 | `top`/`vim`/`less` 简单进入退出 | 可显示、可退出；命令审计允许 partial | `terminal.cast` |
| SSH-10 | 审计产物 | 结束会话后查看 replay 目录 | 生成 meta/cast/events/commands | 文件列表截图 |

## SFTP 测试项

| ID | 场景 | 命令/操作 | 预期结果 | 证据 |
| --- | --- | --- | --- | --- |
| SFTP-01 | 连接和列目录 | `sftp -P 47102 admin@127.0.0.1` 后执行 `pwd`、`ls` | 可连接、可列目录 | 客户端输出、`files.jsonl` |
| SFTP-02 | 指定资产连接 | `sftp -P 47102 admin+target-local@127.0.0.1` | 进入指定资产 | 客户端输出 |
| SFTP-03 | 上传下载 | `put config.example.json`，再 `get` | 文件内容一致 | checksum、`files-summary.json` |
| SFTP-04 | 目录操作 | `mkdir`、`rmdir` | 成功并记录审计 | `files.jsonl` |
| SFTP-05 | 重命名删除 | `rename`、`rm` | 成功并记录审计 | `files.jsonl` |
| SFTP-06 | stat/chmod | 查看属性、修改权限（目标支持时） | 操作成功或明确失败 | 客户端日志、`files.jsonl` |
| SFTP-07 | 中文/空格路径 | 上传、下载、删除中文和空格文件名 | 路径正确，无乱码 | 文件列表、审计记录 |
| SFTP-08 | 大文件 | 上传/下载 ≥100MB 文件 | 不崩溃，字节统计合理 | checksum、summary |
| SFTP-09 | 批量文件 | 批量上传/下载 ≥100 个小文件 | 无明显丢失或死锁 | 文件计数、summary |
| SFTP-10 | 异常断线 | 传输中断开客户端 | 服务端清理会话，无 goroutine 明显泄漏 | 日志、会话状态 |

## 审计验收项

每个 SSH/SFTP 客户端测试结束后，都应检查：

| ID | 检查项 | 预期结果 |
| --- | --- | --- |
| AUDIT-01 | `meta.json` | user、target、client_ip、started_at 合理 |
| AUDIT-02 | `terminal.cast` | Shell 会话有可回放输出 |
| AUDIT-03 | `terminal-events.jsonl` | resize 等终端事件有记录 |
| AUDIT-04 | `commands.jsonl` | 常规命令有记录，复杂 TUI 可为 partial |
| AUDIT-05 | `files.jsonl` | SFTP 文件动作有记录 |
| AUDIT-06 | `files-summary.json` | 文件读写字节数和动作汇总合理 |
| AUDIT-07 | 敏感输入 | 密码不应以明文出现在普通录像/命令记录里 |

## 客户端详细记录

### OpenSSH `ssh`

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SSH-01 | `NOT_TESTED` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 密码登录未自动化；本轮用公钥认证覆盖默认目标 shell |
| SSH-02 | `NOT_TESTED` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 指定资产密码登录未自动化；本轮用公钥认证覆盖指定资产 exec |
| SSH-03 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `-i data\\compat-test\\client_key` 公钥认证通过；默认目标 shell 和指定资产 exec 均可用 |
| SSH-04 | `NOT_TESTED` | - | - | keyboard-interactive 需人工交互补测 |
| SSH-05 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `whoami` 返回 `targetuser`；指定资产 `hostname` 返回 `compat-target` |
| SSH-06 | `NOT_TESTED` | - | - | 本轮只验证 `-tt` shell 可进入；真实窗口 resize 未测 |
| SSH-07 | `PARTIAL` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 自动化验证：`sleep 10` 期间强制 kill `ssh.exe`，Jianmen 继续接受新连接；真实 Ctrl-C signal 仍需人工交互补测 |
| SSH-08 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `false` 返回 exit code 1；stdout/stderr 均可透传 |
| SSH-09 | `NOT_TESTED` | - | - | TUI 程序需人工交互补测 |
| SSH-10 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 生成 `meta.json`、`terminal.cast`、`terminal-events.jsonl`、`commands.jsonl`；证据目录 `data/compat-test/replay/ssh/a010bc208f3e56af6685fe7f4a9dbdab` |

### OpenSSH `sftp`

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SFTP-01 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `pwd`/`ls` 成功，远端列出 `hello.txt` |
| SFTP-02 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `admin+target-local@127.0.0.1` 指定资产连接成功 |
| SFTP-03 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `config.example.json` 上传后下载 SHA256 一致：`FF749932...AD137D` |
| SFTP-04 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `mkdir`/`rmdir` 成功并写入 `files.jsonl` |
| SFTP-05 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `rename`/`rm` 成功并写入 `files.jsonl` |
| SFTP-06 | `PARTIAL` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `ls -l`/stat 与 `chmod 600` 成功，审计记录 `setstat`；chown 在 Windows 临时目标上不适用，待 Linux 目标补测 |
| SFTP-07 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | `中文 文件.txt` 上传、列目录、下载、删除成功；下载 SHA256 一致：`5360FBA...0E4BE0` |
| SFTP-08 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 100MB 文件上传/下载成功，SHA256 一致：`20492A4D...1109E0E`；summary 记录 read/write 各 104857600 字节 |
| SFTP-09 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 100 个小文件批量上传/下载成功，文件数量 100/100，hash 全部一致；summary 中 100 个文件都有读写记录 |
| SFTP-10 | `PASS` | OpenSSH_for_Windows_8.6p1 / Windows 11 / commit 15a7247 | 2026-06-23 | 512MB 上传中强制 kill `sftp.exe`，Jianmen 继续接受新 SSH 连接；中断会话 summary 记录 `transfer_error: 1` 和部分 `write_bytes` |

### PuTTY

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SSH-01 | `NOT_TESTED` | - | - | - |
| SSH-02 | `NOT_TESTED` | - | - | - |
| SSH-03 | `NOT_TESTED` | - | - | - |
| SSH-04 | `NOT_TESTED` | - | - | - |
| SSH-06 | `NOT_TESTED` | - | - | - |
| SSH-07 | `NOT_TESTED` | - | - | - |
| SSH-09 | `NOT_TESTED` | - | - | - |
| SSH-10 | `NOT_TESTED` | - | - | - |

### Xshell

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SSH-01 | `NOT_TESTED` | - | - | - |
| SSH-02 | `NOT_TESTED` | - | - | - |
| SSH-03 | `NOT_TESTED` | - | - | - |
| SSH-04 | `NOT_TESTED` | - | - | - |
| SSH-06 | `NOT_TESTED` | - | - | - |
| SSH-07 | `NOT_TESTED` | - | - | - |
| SSH-09 | `NOT_TESTED` | - | - | - |
| SSH-10 | `NOT_TESTED` | - | - | - |

### SecureCRT

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SSH-01 | `NOT_TESTED` | - | - | - |
| SSH-02 | `NOT_TESTED` | - | - | - |
| SSH-03 | `NOT_TESTED` | - | - | - |
| SSH-04 | `NOT_TESTED` | - | - | - |
| SSH-06 | `NOT_TESTED` | - | - | - |
| SSH-07 | `NOT_TESTED` | - | - | - |
| SSH-09 | `NOT_TESTED` | - | - | - |
| SSH-10 | `NOT_TESTED` | - | - | - |

### Xftp

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SFTP-01 | `NOT_TESTED` | - | - | - |
| SFTP-02 | `NOT_TESTED` | - | - | - |
| SFTP-03 | `NOT_TESTED` | - | - | - |
| SFTP-04 | `NOT_TESTED` | - | - | - |
| SFTP-05 | `NOT_TESTED` | - | - | - |
| SFTP-06 | `NOT_TESTED` | - | - | - |
| SFTP-07 | `NOT_TESTED` | - | - | - |
| SFTP-08 | `NOT_TESTED` | - | - | - |
| SFTP-09 | `NOT_TESTED` | - | - | - |
| SFTP-10 | `NOT_TESTED` | - | - | - |

### WinSCP

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SFTP-01 | `NOT_TESTED` | - | - | - |
| SFTP-02 | `NOT_TESTED` | - | - | - |
| SFTP-03 | `NOT_TESTED` | - | - | - |
| SFTP-04 | `NOT_TESTED` | - | - | - |
| SFTP-05 | `NOT_TESTED` | - | - | - |
| SFTP-06 | `NOT_TESTED` | - | - | - |
| SFTP-07 | `NOT_TESTED` | - | - | - |
| SFTP-08 | `NOT_TESTED` | - | - | - |
| SFTP-09 | `NOT_TESTED` | - | - | - |
| SFTP-10 | `NOT_TESTED` | - | - | - |

### FileZilla

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| SFTP-01 | `NOT_TESTED` | - | - | - |
| SFTP-02 | `NOT_TESTED` | - | - | - |
| SFTP-03 | `NOT_TESTED` | - | - | - |
| SFTP-04 | `NOT_TESTED` | - | - | - |
| SFTP-05 | `NOT_TESTED` | - | - | - |
| SFTP-06 | `NOT_TESTED` | - | - | - |
| SFTP-07 | `NOT_TESTED` | - | - | - |
| SFTP-08 | `NOT_TESTED` | - | - | - |
| SFTP-09 | `NOT_TESTED` | - | - | - |
| SFTP-10 | `NOT_TESTED` | - | - | - |

### VS Code Remote SSH

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| 连接网关 | `NOT_TESTED` | - | - | 验证是否能通过 `admin+target-local` 形式进入目标 |
| 安装/启动远程服务 | `NOT_TESTED` | - | - | 可能依赖 port forwarding，当前可能不支持 |
| 文件浏览 | `NOT_TESTED` | - | - | 可能依赖 SFTP 或远程 server |
| 已知限制记录 | `NOT_TESTED` | - | - | 记录不支持项，不要求 P0 全通过 |

### JetBrains Remote Development

| 测试项 | 状态 | 版本/环境 | 日期 | 备注 |
| --- | --- | --- | --- | --- |
| 连接网关 | `NOT_TESTED` | - | - | 验证基础连接 |
| 启动远程后端 | `NOT_TESTED` | - | - | 可能依赖 port forwarding，当前可能不支持 |
| 文件同步 | `NOT_TESTED` | - | - | 记录行为和失败点 |
| 已知限制记录 | `NOT_TESTED` | - | - | 记录不支持项，不要求 P0 全通过 |

## 问题记录模板

发现兼容问题时，在本节追加记录。

```markdown
### ISSUE-YYYYMMDD-NN: <客户端> <场景> <简述>

- 客户端：
- 客户端版本：
- 操作系统：
- Jianmen commit：
- 测试项：
- 状态：FAIL / PARTIAL / BLOCKED
- 复现步骤：
  1.
  2.
  3.
- 实际结果：
- 预期结果：
- 服务端日志：
- 审计 artifact：
- 初步判断：
- 后续处理：
```

## 发布判断规则

- P0 客户端核心项没有 `FAIL`，才能进入可内测。
- 对 IDE Remote 类客户端，如果失败原因是当前明确未支持的 port forwarding，可记录为已知限制，不阻塞 SSH/SFTP 基础内测。
- 没有测试记录的客户端只能写 `NOT_TESTED`，不能在 README 或销售材料中声明兼容。
