# 单容器托管 guacd

Jianmen 的容器镜像同时包含 Go 服务和 Apache Guacamole `guacd`。Go
进程负责启动、健康检查、日志采集和停止 `guacd`，不需要独立 sidecar
容器，也不需要 systemd。

## 构建方式

Go 程序在 Windows 中交叉编译，WSL 只负责装配和运行 Linux 容器：

```text
Windows build.ps1
    -> dist/bastion-core-linux-amd64
    -> 固定版 guacd Linux 运行层
    -> jianmen:guacd-1.6.0
```

先在 Windows PowerShell 中执行：

```powershell
.\build.ps1
```

Dockerfile 只复制上述 Linux 二进制，不会在 WSL 或 Docker 构建阶段重新下载
Go/npm 依赖，也不会重复编译前端和后端。

当前成品明确为 `linux/amd64`。`dist` 不进入版本库，因此每次从干净工作区
构建镜像前都必须先执行 `build.ps1`；缺少 Linux 产物时 Docker 构建会直接失败，
避免误用旧二进制。

## 固定版本

运行镜像基于不可变的官方 Guacamole 镜像：

```text
guacamole/guacd:1.6.0
sha256:8974eaa9ba32f713daf311e7cc8cd7e4cdfba1edea39eed75524e78ef4b08f4f
```

Dockerfile 使用摘要而不是可变标签作为最终运行时基础。Jianmen 和
`guacd` 因此使用同一用户、网络命名空间和文件系统。

## 配置

容器内的托管配置如下：

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
  }
}
```

`guacd` 的启动参数不开放配置。Jianmen 会从 `guacd_address` 强制生成前台模式、
回环绑定、监听端口和日志级别参数，避免进程后台化或意外暴露 4822。

两个进程直接共享 `/app/data`，不需要额外录像共享卷。4822 只监听容器
回环地址，Compose 不会将其发布到宿主机。

## 生命周期

启用 `managed_guacd` 后：

1. Jianmen 启动前确认 4822 未被其他进程占用。
2. 启动前台 `guacd` 并等待 TCP 健康检查成功。
3. 将 `guacd` 标准输出和错误写入 Jianmen 日志。
4. `guacd` 意外退出时，将其作为运行时故障处理。
5. Jianmen 收到停止信号时，停止并回收 `guacd`。

## WSL 装配并启动

首次启动前，需要按项目 `README.md` 的容器证书步骤创建外部卷
`jianmen-certs`。证书文件归属必须为 `10001:10001`，`admin.key` 和
`database.key` 权限为 `0600`。完成证书准备和 `build.ps1` 后，在 Windows
PowerShell 中执行：

```powershell
wsl.exe -d Debian -u root --exec docker compose `
  -f /mnt/c/02-codespace/Jianmen/docker-compose.web-rdp.yml `
  up -d --build
```

检查状态：

```powershell
wsl.exe -d Debian -u root --exec docker compose `
  -f /mnt/c/02-codespace/Jianmen/docker-compose.web-rdp.yml `
  ps
```

固定的官方 guacd 1.6.0 镜像使用其上游发布时的 Alpine 运行层。正式发布前
应对最终镜像执行安全扫描；长期升级时同时更新 guacd 版本和镜像摘要。
