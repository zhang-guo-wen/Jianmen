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

## 前端依赖变更注意事项

**npm 版本已锁定**:`web/package.json` 的 `packageManager` 字段声明了 npm 版本(当前 `npm@11.16.0`),CI 流水线也已固定同版本。

修改 `web/package.json` 依赖后,必须同步提交 `package-lock.json`,否则 CI 的 `npm ci` 会失败。规范如下:

1. 生成/更新 lock 时使用与 packageManager 一致的 npm 版本(本地 npm 版本不一致时,用 `npx npm@11.16.0 ...` 或 `corepack` 代替):
   ```bash
   cd web
   npx npm@11.16.0 install --package-lock-only
   ```
2. 旧版 npm 生成的 lock 可能缺 peer 依赖条目(典型报错 `Missing: @emnapi/core@x.x.x from lock file`,npm ci EUSAGE)。原因是 npm 版本差异导致 peer 校验行为不同,必须用锁定版本重新生成。
3. 提交前用干净环境验证(现有 node_modules 会干扰 lock 更新,先删除):
   ```bash
   cd web && rm -rf node_modules && npx npm@11.16.0 ci
   ```
4. 不要手工编辑 `package-lock.json`。
