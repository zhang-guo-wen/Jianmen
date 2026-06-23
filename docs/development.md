# 开发与调试

## 当前环境

- Go: `C:\Program Files\Go\bin\go.exe`
- Delve: `%USERPROFILE%\go\bin\dlv.exe`
- 本机 OpenSSH Server: `sshd` 已运行，可作为 `127.0.0.1:22` 调试目标

当前 PowerShell 会话如果还没有 Go PATH，先执行：

```powershell
$env:Path='C:\Program Files\Go\bin;'+$env:Path
```

## 编译

```powershell
go test ./...
go build -o bin\bastion-core.exe .\cmd\bastion-core
go build -o bin\bastionctl.exe .\cmd\bastionctl
```

## 本地运行

复制一份本地配置：

```powershell
Copy-Item config.example.json config.local.json
notepad config.local.json
```

配置里有两层账号：

- `users`: 登录堡垒机的账号，示例是 `admin/admin`。
- `targets`: 堡垒机转发到目标机器时使用的账号。调试本机 `sshd` 时，把 `username/password` 改成当前 Windows SSH 可登录账号。

启动：

```powershell
.\bin\bastion-core.exe -config config.local.json
```

或者直接运行源码：

```powershell
go run .\cmd\bastion-core -config config.local.json
```

管理入口：

```text
Admin API:      http://127.0.0.1:47100/
Vue Web Admin: http://127.0.0.1:47101/
```

后端 `/` 返回 api-only JSON；日常管理界面使用 Vue Web Admin。

默认 token：

```text
dev-admin-token
```

命令行客户端：

```powershell
.\bin\bastionctl.exe -token dev-admin-token health
.\bin\bastionctl.exe -token dev-admin-token users
.\bin\bastionctl.exe -token dev-admin-token targets
.\bin\bastionctl.exe -token dev-admin-token target-add web01 127.0.0.1 root change-me 22
.\bin\bastionctl.exe -token dev-admin-token sessions
```

新增服务器也可以在管理页面的 `Add Target` 区域完成。新增资产会写入：

```text
data/targets.json
```

## 验证代理是否生效

验证一定要让客户端走堡垒机端口，而不是直接连目标服务器。

默认资产：

```powershell
ssh -p 47102 admin@127.0.0.1
```

指定资产 ID：

```powershell
ssh -p 47102 admin+web01@127.0.0.1
```

这里的 `admin` 是堡垒机用户，`web01` 是资产 ID。目标服务器自己的 SSH 账号和密码配置在资产里。

SFTP / Xftp 也一样走堡垒机端口：

```powershell
sftp -P 47102 admin+web01@127.0.0.1
```

连接后执行命令或传文件，再回管理页面刷新 `SSH Sessions`，查看 `Commands`、`Files`、`Summary`。

## SSH Shell 调试

另开一个 PowerShell：

```powershell
ssh -p 47102 admin@127.0.0.1
```

密码使用堡垒机配置里的 `users[0].password`，示例为 `admin`。

登录后执行几条命令，例如：

```shell
whoami
pwd
dir
```

会话结束后查看：

```powershell
Get-ChildItem data\replay\ssh -Recurse
```

关键文件：

- `meta.json`: 会话元数据。
- `terminal.cast`: asciinema v2 终端录像。
- `terminal-events.jsonl`: 终端窗口变化等事件。
- `commands.jsonl`: 从交互输入中解析出的命令和响应预览。

## SFTP / Xftp 调试

命令行 SFTP：

```powershell
sftp -P 47102 admin@127.0.0.1
```

进入后执行：

```sftp
pwd
ls
mkdir bastion-debug
put config.example.json bastion-debug/config.example.json
get bastion-debug/config.example.json downloaded-config.example.json
rename bastion-debug/config.example.json bastion-debug/config-renamed.json
rm bastion-debug/config-renamed.json
rmdir bastion-debug
bye
```

图形客户端 Xftp、WinSCP、FileZilla 也可以连：

- Host: `127.0.0.1`
- Port: `47102`
- Protocol: `SFTP`
- Username: `admin`
- Password: `admin`

SFTP 审计文件：

- `files.jsonl`: 每个文件语义动作一行，包括 `open_read/open_write/read/write/list/stat/mkdir/remove/rename/setstat` 等。
- `files-summary.json`: 按路径聚合的动作次数和读写字节数。

## 数据库代理调试

在 `config.local.json` 打开对应数据库代理：

```json
{
  "enabled": true,
  "name": "mysql-local",
  "protocol": "mysql",
  "listen_addr": "127.0.0.1:33060",
  "upstream_addr": "127.0.0.1:3306"
}
```

MySQL 客户端连代理端口：

```powershell
mysql -h 127.0.0.1 -P 33060 -u root -p --ssl-mode=DISABLED
```

PostgreSQL 客户端连代理端口：

```powershell
psql "host=127.0.0.1 port=15432 user=postgres dbname=postgres sslmode=disable"
```

数据库审计文件：

- `data/replay/db/<connection-id>/meta.json`
- `data/replay/db/<connection-id>/queries.jsonl`

当前数据库代理只解析明文协议；客户端启用 TLS 后，代理仍会转发流量，但看不到 SQL 文本。

## Delve 单步调试

```powershell
& "$env:USERPROFILE\go\bin\dlv.exe" debug .\cmd\bastion-core -- -config config.local.json
```

常用断点：

```text
break jianmen/internal/server/sshserver.(*Server).handleConn
break jianmen/internal/proxy/sshproxy.(*Session).handleRequest
break jianmen/internal/proxy/sftpproxy.(*handler).Filecmd
break jianmen/internal/proxy/sftpproxy.(*trackedFile).WriteAt
continue
```

## 当前实现边界

- SSH Shell 已支持字节流代理、终端录像、简单命令解析和响应预览。
- SFTP 已支持语义代理和文件审计，覆盖常见文件客户端动作。
- 命令解析仍是基于终端输入的第一版，复杂交互、全屏 TUI、粘贴多行、shell alias 展开等场景后续要通过更强解析器或 Linux agent 增强。
