# Contributing to Jianmen

感谢你对 Jianmen 的关注！本项目处于早期开发阶段，欢迎 Issue 和 PR。

## 开发环境

### 后端
- Go 1.23+
- 推荐 IDE：VS Code + Go 插件

### 前端
- Node.js 18+
- npm 9+
- 推荐 IDE：VS Code + Volar

## 快速开始

```bash
# 克隆项目
git clone <repo-url> && cd Jianmen

# 后端
go build -o bin/bastion-core.exe ./cmd/bastion-core
go build -o bin/bastionctl.exe ./cmd/bastionctl
go test ./... -count=1

# 前端
cd web
npm install
npm run dev        # 开发模式，默认 http://127.0.0.1:47101
npm run typecheck  # 类型检查
npm run build      # 生产构建
```

详细开发文档见 [docs/development.md](docs/development.md)。

## 分支策略

- `main` — 稳定分支
- `dev` — 开发集成分支
- 功能分支使用 git worktree 隔离开发

## 合并准则

合并到 `dev` 之前，必须：
1. 将最新 `dev` 合并到自己的分支
2. 解决冲突
3. 验证通过：
   - `npm run typecheck`（前端）
   - `npm run build`（前端）
   - `go build ./...`（后端）
   - `go test ./... -count=1`（后端）

## 代码风格

- Go：遵循标准 Go 风格（gofmt/goimports）
- Vue：Composition API + `<script setup>` + TypeScript
- 提交信息使用中文，格式：`type: 描述`
  - `feat:` 新功能
  - `fix:` 修复
  - `refactor:` 重构
  - `test:` 测试
  - `docs:` 文档
  - `style:` 样式
  - `chore:` 杂项

## 提 Issue

- Bug 报告：描述复现步骤、期望行为、实际行为、环境信息
- Feature Request：描述使用场景、期望功能和验收标准

## 提 PR

1. 确保你的分支已合并最新 `dev`
2. 确保编译和测试通过
3. PR 标题用中文，描述清楚变更内容和原因
4. 关联相关 Issue（如有）

## 项目约定

- 当前处于未发布阶段，不考虑向后兼容，直接重构即可
- 配置文件不应预置 demo 数据
- API 不能泄露密码、私钥等敏感信息
- 资源模型以账号（host_account / database_account）为最小单位
