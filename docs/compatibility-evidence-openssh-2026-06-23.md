# OpenSSH 兼容性测试证据（2026-06-23）

本文记录 2026-06-23 对 OpenSSH 命令行客户端执行的自动化冒烟测试证据。结论已回填到 [compatibility-matrix.md](compatibility-matrix.md)。

## 环境

| 项 | 值 |
| --- | --- |
| 操作系统 | Windows 11 |
| OpenSSH | `OpenSSH_for_Windows_8.6p1, LibreSSL 3.4.3` |
| Jianmen commit | `15a7247` |
| Jianmen SSH/SFTP Gateway | `127.0.0.1:47102` |
| 临时目标 SSH/SFTP Server | `127.0.0.1:47222` |
| Jianmen 用户 | `admin` |
| 测试目标资产 | `target-local` |
| 认证方式 | OpenSSH 公钥认证 |

备注：

- Windows OpenSSH `sftp` 不支持 `sftp -V`，因此 SFTP 客户端版本沿用 `ssh -V`。
- 本机用户目录下的 `C:\Users\Windows11\.ssh\config` 存在权限警告，会导致 OpenSSH 拒绝启动。测试时使用 `-F data\compat-test\empty_ssh_config` 绕过用户级 SSH config，避免修改用户配置。
- 本轮为非交互自动化测试，因此未覆盖密码登录、keyboard-interactive、真实窗口 resize、Ctrl-C、TUI 程序和异常断线。

## 测试拓扑

```text
OpenSSH ssh/sftp
  -> Jianmen SSH/SFTP Gateway 127.0.0.1:47102
  -> 临时目标 SSH/SFTP Server 127.0.0.1:47222
  -> data/compat-test/target-root
```

临时目标服务只用于本轮兼容性冒烟测试：

- 支持 SSH `session` channel。
- 支持 `shell`、`exec`、`subsystem=sftp`。
- `exec` 命令返回固定测试输出。
- SFTP 使用 `github.com/pkg/sftp` server，并以测试目录作为 root。

## SSH 测试命令与结果

### 默认目标 exec

```powershell
ssh -F data\compat-test\empty_ssh_config `
  -i data\compat-test\client_key `
  -o BatchMode=yes `
  -o StrictHostKeyChecking=no `
  -o UserKnownHostsFile=data\compat-test\known_hosts `
  -p 47102 admin@127.0.0.1 whoami
```

结果：

```text
targetuser
EXIT=0
```

### 指定资产 exec

```powershell
ssh -F data\compat-test\empty_ssh_config `
  -i data\compat-test\client_key `
  -o BatchMode=yes `
  -o StrictHostKeyChecking=no `
  -o UserKnownHostsFile=data\compat-test\known_hosts `
  -p 47102 admin+target-local@127.0.0.1 hostname
```

结果：

```text
compat-target
EXIT=0
```

### exit-status

```powershell
ssh ... -p 47102 admin@127.0.0.1 false
```

结果：

```text
FALSE_EXIT=1
```

### stdout / stderr

```powershell
ssh ... -p 47102 admin@127.0.0.1 stderr
```

结果：

```text
stderr
stdout
STDERR_EXIT=0
```

### Shell 会话

使用输入文件：

```text
whoami
pwd
hostname
exit
```

命令：

```powershell
cmd /c "ssh -F data\compat-test\empty_ssh_config -i data\compat-test\client_key -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=data\compat-test\known_hosts -tt -p 47102 admin@127.0.0.1 < data\compat-test\ssh_shell_input.txt"
```

结果：

```text
Jianmen compat target
$ targetuser
$ data\compat-test\target-root
$ compat-target
$ logout
Connection to 127.0.0.1 closed.
SHELL_EXIT=0
```

### SSH 审计产物

代表性会话目录：

```text
data/compat-test/replay/ssh/a010bc208f3e56af6685fe7f4a9dbdab
```

生成文件：

- `meta.json`
- `terminal.cast`
- `terminal-events.jsonl`
- `commands.jsonl`
- `files.jsonl`

`meta.json` 关键字段：

```json
{
  "client_ip": "127.0.0.1",
  "session_id": "a010bc208f3e56af6685fe7f4a9dbdab",
  "target": "compat target",
  "user": "admin"
}
```

`commands.jsonl` 记录到 `whoami`、`pwd`、`hostname`、`exit`，置信度为当前实现的 `partial`。

## SFTP 测试命令与结果

### 基础文件操作

Batch 文件：

```sftp
pwd
ls
mkdir jianmen-compat
put config.example.json jianmen-compat/config.example.json
get jianmen-compat/config.example.json data/compat-test/downloaded-config.example.json
rename jianmen-compat/config.example.json jianmen-compat/config-renamed.json
rm jianmen-compat/config-renamed.json
rmdir jianmen-compat
bye
```

命令：

```powershell
sftp -F data\compat-test\empty_ssh_config `
  -i data\compat-test\client_key `
  -o BatchMode=yes `
  -o StrictHostKeyChecking=no `
  -o UserKnownHostsFile=data\compat-test\known_hosts `
  -P 47102 `
  -b data\compat-test\sftp_batch_basic.txt `
  admin+target-local@127.0.0.1
```

结果：

```text
SFTP_BASIC_EXIT=0
```

Hash 校验：

```text
config.example.json                             FF7499327880A30C8C2B2CBBB4657674FD0EF03C53E2E178E0AB5F0E31AD137D
downloaded-config.example.json                  FF7499327880A30C8C2B2CBBB4657674FD0EF03C53E2E178E0AB5F0E31AD137D
```

### 中文和空格路径

Batch 文件：

```sftp
mkdir jianmen-compat
put "data/compat-test/中文 文件.txt" "jianmen-compat/中文 文件.txt"
ls jianmen-compat
get "jianmen-compat/中文 文件.txt" "data/compat-test/downloaded 中文 文件.txt"
rm "jianmen-compat/中文 文件.txt"
rmdir jianmen-compat
bye
```

结果：

```text
SFTP_UNICODE_EXIT=0
```

Hash 校验：

```text
中文 文件.txt                                  5360FBA379CEBDBBD4391123BD285FE74BFC011AA0E66ED909B75F8CFB0E4BE0
downloaded 中文 文件.txt                       5360FBA379CEBDBBD4391123BD285FE74BFC011AA0E66ED909B75F8CFB0E4BE0
```

代表性审计目录：

```text
data/compat-test/replay/ssh/7ffc28b7c1ff400af799952e934dadad
```

`files.jsonl` 正确记录：

- `realpath`
- `mkdir`
- `stat` / `lstat`
- `open_write`
- `write`
- `open_read`
- `read`
- `remove`
- `rmdir`

`files-summary.json` 中中文路径记录：

```json
{
  "path": "/C:/02-codespace/Jianmen/data/compat-test/target-root/jianmen-compat/中文 文件.txt",
  "read_bytes": 19,
  "write_bytes": 19,
  "last_result": "success"
}
```

### 100MB 大文件

文件大小：`104857600` bytes。

Batch 文件：

```sftp
mkdir jianmen-large
put data/compat-test/large-100mb.bin jianmen-large/large-100mb.bin
get jianmen-large/large-100mb.bin data/compat-test/downloaded-large-100mb.bin
rm jianmen-large/large-100mb.bin
rmdir jianmen-large
bye
```

结果：

```text
SFTP_LARGE_EXIT=0
```

Hash 校验：

```text
large-100mb.bin                                  20492A4D0D84F8BEB1767F6616229F85D44C2827B64BDBFB260EE12FA1109E0E
downloaded-large-100mb.bin                       20492A4D0D84F8BEB1767F6616229F85D44C2827B64BDBFB260EE12FA1109E0E
```

代表性审计目录：

```text
data/compat-test/replay/ssh/864db2f7e22c877c4f3b1ca6746f97b7
```

`files-summary.json` 记录：

```json
{
  "path": "/C:/02-codespace/Jianmen/data/compat-test/target-root/jianmen-large/large-100mb.bin",
  "read_bytes": 104857600,
  "write_bytes": 104857600,
  "last_result": "success"
}
```

### 100 个小文件批量传输

Batch 文件：

```sftp
mkdir jianmen-batch
put data/compat-test/batch-src/*.txt jianmen-batch/
get jianmen-batch/*.txt data/compat-test/batch-downloaded/
rm jianmen-batch/*.txt
rmdir jianmen-batch
bye
```

结果：

```text
SFTP_BATCH_EXIT=0
SRC_COUNT=100
DST_COUNT=100
HASH_MATCH=True
```

代表性审计目录：

```text
data/compat-test/replay/ssh/3341f532f1353c2ca7a800c0ff3451f3
```

`files-summary.json` 统计：

```text
SUMMARY_FILES=100
ALL_READ_WRITE=True
```

## 本轮结论

OpenSSH 命令行客户端本轮结论为 `PARTIAL`：

- OpenSSH `ssh`：公钥认证、默认资产、指定资产、exec、shell、stdout/stderr、exit-status、基础审计产物通过。
- OpenSSH `sftp`：基础连接、指定资产、上传下载、目录操作、重命名删除、中文/空格路径、100MB 大文件、100 小文件批量和文件审计通过。

仍需补测：

- SSH 密码登录。
- keyboard-interactive。
- 真实窗口 resize。
- Ctrl-C / signal。
- TUI 程序。
- SFTP chmod/chown。
- SFTP 异常断线。
