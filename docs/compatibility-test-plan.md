# 客户端兼容性手工测试步骤

更新日期：2026-06-23

本文提供可执行的手工测试步骤。测试结果统一记录到 [compatibility-matrix.md](compatibility-matrix.md)。

## 通用准备

### 1. 启动测试目标 SSH Server

本机 Windows OpenSSH Server 可作为最小测试目标：

```powershell
Get-Service sshd
```

若未运行，先启动：

```powershell
Start-Service sshd
```

### 2. 准备配置

```powershell
Copy-Item config.example.json config.local.json
notepad config.local.json
```

确认：

- `listen_addr` 是 `0.0.0.0:47102`。
- `admin.listen_addr` 是 `127.0.0.1:47100`。
- `targets[0]` 指向可登录的 SSH 目标。
- `targets[0].username/password` 是目标主机账号，不是 Jianmen 登录账号。
- `users[0].username/password` 是 Jianmen 登录账号，默认 `admin/admin`。

### 3. 启动 Jianmen

```powershell
& "C:\Program Files\Go\bin\go.exe" run .\cmd\bastion-core -config config.local.json
```

### 4. 验证 API 与目标资产

另开一个 PowerShell：

```powershell
& "C:\Program Files\Go\bin\go.exe" run .\cmd\bastionctl -token dev-admin-token health
& "C:\Program Files\Go\bin\go.exe" run .\cmd\bastionctl -token dev-admin-token targets
```

### 5. 记录 commit

```powershell
git rev-parse --short HEAD
```

把 commit、客户端版本、操作系统写入矩阵。

## OpenSSH `ssh`

### 查看版本

```powershell
ssh -V
```

### 默认资产登录

```powershell
ssh -p 47102 admin@127.0.0.1
```

输入 Jianmen 用户 `admin` 的密码。登录后执行：

```shell
whoami
pwd
hostname
exit
```

记录：

- 是否成功登录。
- `whoami` 是否是目标主机账号。
- `data/replay/ssh/<session>/meta.json` 是否生成。
- `commands.jsonl` 是否记录上述命令。

### 指定资产登录

```powershell
ssh -p 47102 admin+target-local@127.0.0.1
```

执行同样命令，确认 `meta.json` 中目标信息正确。

### exec 模式

```powershell
ssh -p 47102 admin@127.0.0.1 whoami
ssh -p 47102 admin@127.0.0.1 "echo stdout && echo stderr 1>&2"
```

记录 stdout/stderr 是否正常。

### resize / signal

登录后：

1. 调整终端窗口大小。
2. 执行 `stty size`。
3. 执行长命令，例如 `ping 127.0.0.1` 或目标系统等价命令。
4. 按 `Ctrl-C`。

记录 resize 是否生效、Ctrl-C 是否中断命令。

## OpenSSH `sftp`

### 查看版本

```powershell
sftp -V
```

某些版本没有 `-V`，可记录 `ssh -V`。

### 基础连接

```powershell
sftp -P 47102 admin@127.0.0.1
```

进入后执行：

```sftp
pwd
ls
bye
```

### 文件操作

```powershell
sftp -P 47102 admin+target-local@127.0.0.1
```

进入后执行：

```sftp
mkdir jianmen-compat
put config.example.json jianmen-compat/config.example.json
get jianmen-compat/config.example.json downloaded-config.example.json
rename jianmen-compat/config.example.json jianmen-compat/config-renamed.json
rm jianmen-compat/config-renamed.json
rmdir jianmen-compat
bye
```

检查：

```powershell
Get-ChildItem data\replay\ssh -Recurse -Filter files.jsonl
Get-ChildItem data\replay\ssh -Recurse -Filter files-summary.json
```

记录文件动作和字节统计是否合理。

### 中文和空格路径

准备本地文件：

```powershell
'hello' | Out-File -Encoding utf8 '中文 文件.txt'
```

SFTP 中执行：

```sftp
mkdir jianmen-compat
put "中文 文件.txt" "jianmen-compat/中文 文件.txt"
ls jianmen-compat
get "jianmen-compat/中文 文件.txt" "downloaded 中文 文件.txt"
rm "jianmen-compat/中文 文件.txt"
rmdir jianmen-compat
bye
```

记录路径是否乱码。

## PuTTY

### 准备

记录 PuTTY 版本：打开 PuTTY → About。

### 密码登录

1. Host Name: `127.0.0.1`
2. Port: `47102`
3. Connection type: `SSH`
4. Open。
5. login as: `admin`
6. password: `admin`

登录后执行：

```shell
whoami
pwd
exit
```

记录是否能正常登录、中文显示是否正常、会话结束后审计文件是否生成。

### 指定资产

login as 使用：

```text
admin+target-local
```

重复上面的命令。

### 公钥登录

1. 使用 PuTTYgen 准备 key。
2. 把 public key 配到 Jianmen 用户的 `public_keys` 或 `authorized_keys_path`。
3. PuTTY → Connection → SSH → Auth → Credentials → Private key file。
4. 登录 `admin`。

记录是否通过。

## Xshell

### 密码登录

1. 新建会话。
2. Host: `127.0.0.1`
3. Port: `47102`
4. Protocol: `SSH`
5. Authentication Method: Password。
6. User Name: `admin` 或 `admin+target-local`。
7. Password: `admin`。

登录后执行：

```shell
whoami
pwd
exit
```

### 交互场景

- 调整窗口大小。
- 执行长命令后 Ctrl-C。
- 打开 `top`/`vim`/`less`，再正常退出。

记录是否卡死、乱码、断连或审计异常。

## SecureCRT

### 密码登录

1. Quick Connect。
2. Protocol: `SSH2`。
3. Hostname: `127.0.0.1`。
4. Port: `47102`。
5. Username: `admin` 或 `admin+target-local`。
6. Authentication: Password。

执行：

```shell
whoami
pwd
exit
```

记录结果。

### 交互场景

与 Xshell 相同：resize、Ctrl-C、TUI 程序。

## Xftp

### 连接

1. 新建会话。
2. Protocol: `SFTP`。
3. Host: `127.0.0.1`。
4. Port: `47102`。
5. User Name: `admin` 或 `admin+target-local`。
6. Password: `admin`。

### 文件操作

在图形界面中执行：

- 新建目录 `jianmen-compat`。
- 上传 `config.example.json`。
- 下载回来并改名。
- 重命名远端文件。
- 删除文件。
- 删除目录。
- 刷新目录。

检查 `files.jsonl` 与 `files-summary.json`。

## WinSCP

### 连接

1. File protocol: `SFTP`。
2. Host name: `127.0.0.1`。
3. Port number: `47102`。
4. User name: `admin` 或 `admin+target-local`。
5. Password: `admin`。
6. Advanced 中可开启 Session log，便于定位问题。

### 文件操作

执行：

- 浏览目录。
- 上传/下载文件。
- 新建/删除目录。
- 重命名文件。
- 修改权限（目标系统支持时）。
- 批量上传多个小文件。

记录 WinSCP 日志和 Jianmen 审计结果。

## FileZilla

### 连接

1. Host: `sftp://127.0.0.1`
2. Username: `admin` 或 `admin+target-local`
3. Password: `admin`
4. Port: `47102`
5. Quickconnect。

### 文件操作

执行：

- 浏览目录。
- 上传/下载文件。
- 新建/删除目录。
- 重命名文件。
- 批量上传多个小文件。

记录 FileZilla message log 和 Jianmen 审计结果。

## VS Code Remote SSH

> 当前 Jianmen 尚未支持 SSH port forwarding。VS Code Remote SSH 可能需要端口转发或远程 server 链路，因此它是 P1 验证项，不应阻塞 SSH/SFTP 基础内测。

### 配置示例

编辑 `~/.ssh/config`：

```sshconfig
Host jianmen-target-local
  HostName 127.0.0.1
  Port 47102
  User admin+target-local
```

在 VS Code 中执行：Remote-SSH: Connect to Host。

记录：

- 是否能完成 SSH 登录。
- 是否卡在安装 VS Code Server。
- 是否需要 port forwarding。
- 失败日志。

## JetBrains Remote Development

> 当前作为 P2 验证项。若依赖 port forwarding，记录为已知限制。

连接参数：

- Host: `127.0.0.1`
- Port: `47102`
- User: `admin+target-local`
- Auth: password 或 key

记录：

- 是否能建立基础 SSH。
- 是否能启动远程后端。
- 是否有明确协议需求超出当前 Jianmen 支持范围。

## 结果回填

每完成一个客户端测试，更新 [compatibility-matrix.md](compatibility-matrix.md)：

- 总览表状态。
- 客户端详细表状态。
- 版本/环境/日期/记录人。
- 如失败，追加问题记录。

不要只写“已测”。必须包含环境、步骤和证据路径。
