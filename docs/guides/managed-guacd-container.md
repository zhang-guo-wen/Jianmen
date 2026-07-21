# 单容器托管 guacd

Jianmen 的容器镜像同时包含 Go 服务和 Apache Guacamole `guacd`。Go
进程负责启动、健康检查、日志采集和停止 `guacd`，不需要独立 sidecar
容器，也不需要 systemd。

## 构建方式

Go 程序在 Windows 中交叉编译，WSL 只负责装配和运行 Linux 容器：

```text
Windows `scripts/build/build.ps1`
    -> dist/jianmen-linux-amd64-lite
    -> 固定版 guacd Linux 运行层
    -> jianmen:guacd-1.6.0
```

先在 Windows PowerShell 中执行：

```powershell
.\scripts\build\build.ps1
```

Dockerfile 只复制上述 Linux 二进制，不会在 WSL 或 Docker 构建阶段重新下载
Go/npm 依赖，也不会重复编译前端和后端。

构建脚本同时生成可独立部署的 `jianmen-linux-amd64-rdp`。该版本把固定摘要
镜像中的 guacd 运行时嵌入 Go 二进制，首次启动自动释放，不要求目标主机安装
Docker。容器镜像继续使用 Lite 版，避免重复携带 guacd。

`dist` 不进入版本库，因此每次从干净工作区
构建镜像前都必须先执行 `scripts/build/build.ps1`；缺少 Linux 产物时 Docker 构建会直接失败，
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
回环地址，容器不会将其发布到宿主机。

## 生命周期

启用 `managed_guacd` 后：

1. Jianmen 启动前确认 4822 未被其他进程占用。
2. 启动前台 `guacd` 并等待 TCP 健康检查成功。
3. 将 `guacd` 标准输出和错误写入 Jianmen 日志。
4. `guacd` 意外退出时，将其作为运行时故障处理。
5. Jianmen 收到停止信号时，停止并回收 `guacd`。

## Windows 启动 WSL 容器

在仓库根目录执行：

```powershell
.\scripts\start.ps1 -Mode WSL
```

脚本只使用 WSL 发行版中的 Docker Engine，构建产物和镜像，将数据保存到
`jianmen-data` 命名卷，并等待 `jianmen` 容器进入 healthy 状态。命名卷不会与 Windows
本地模式的 `data/` 目录混用。复用已有镜像重启时执行：

```powershell
.\scripts\start.ps1 -Mode WSL -SkipBuild
```

不需要 Web RDP 时，执行 `.\scripts\start.ps1 -Mode Windows`，直接启动 Windows 本机
程序且不启动 `guacd`。

固定的官方 guacd 1.6.0 镜像使用其上游发布时的 Alpine 运行层。正式发布前
应对最终镜像执行安全扫描；长期升级时同时更新 guacd 版本和镜像摘要。
