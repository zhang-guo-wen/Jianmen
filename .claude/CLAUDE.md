每次回答，请用中文回答我。注释使用中文，提交代码使用中文

开发新功能的时候，要使用gitworktree 不要直接改项目


## 项目结构

```
jianmen/
├── cmd/
│   └── jianmen/         # 主服务入口
├── internal/
│   ├── server/          # SSH/Admin/DB 服务
│   ├── proxy/           # SSH/SFTP/DB 协议代理
│   ├── recording/       # 终端录像与命令记录
│   ├── rbac/            # 授权检查与资源定义
│   ├── store/           # 数据存储接口与实现
│   └── model/           # 数据模型
├── web/                 # Vue 3 前端
├── configs/             # 配置示例
├── deploy/docker/       # 容器部署文件
├── scripts/             # 构建、CI 与开发辅助脚本
└── docs/                # 指南、审查和历史资料
```

## 快速开始

### 环境要求

- Go 1.23+
- Node.js 18+（前端开发）
- 目标主机需运行 SSH Server（用于代理连接）

### 安装与运行

```powershell
# 克隆项目
git clone https://github.com/your-org/jianmen.git
cd jianmen

# Windows 本机统一通过 Docker 构建并启动
.\scripts\start.ps1
```

启动后：

| 服务 | 地址 |
|------|------|
| Admin API | `http://127.0.0.1:47100` |
| Vue Web Admin | `http://127.0.0.1:47101` |
| SSH/SFTP Gateway | `0.0.0.0:47102` |

### 验证代理

```bash
# SSH 连接堡垒机（默认资产）
ssh -p 47102 admin@127.0.0.1

# 指定资产 ID
ssh -p 47102 admin+web01@127.0.0.1

# SFTP 连接
sftp -P 47102 admin@127.0.0.1
```

### 前端开发

```bash
cd web
npm install
npm run dev        # 开发服务 http://127.0.0.1:47101
npm run typecheck  # TypeScript 类型检查
npm run build      # 生产构建
```
