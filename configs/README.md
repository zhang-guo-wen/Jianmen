# 配置示例

- `config.example.json`：独立二进制部署及 Windows 本地模式使用的基础配置，默认关闭 Web RDP。
- `config.docker.json`：容器镜像内置配置，管理端 HTTP 供反向代理使用。
- `config.docker.local.json`：Windows 调用 WSL Docker 时使用的容器配置。
- `config.docker.web-rdp.example.json`：启用 Web RDP 的容器配置。
- `config.docker.proxy.example.json`：由反向代理终止 HTTPS 的容器配置。
- `config.wsl.rdp.example.json`：Linux RDP 独立包在 WSL 中运行的示例，使用内嵌 guacd。

实际密钥、密码和本地运行配置不要提交到仓库。Windows 本机启动请使用仓库的
`scripts/start.ps1`，通过 `-Mode Windows` 或 `-Mode WSL` 选择运行方式。
