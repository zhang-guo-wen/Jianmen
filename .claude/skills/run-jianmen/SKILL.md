---
name: run-jianmen
description: Use when asked to run, launch, start, test, or smoke-test the Jianmen bastion application (backend or frontend)
---

# Run Jianmen

## Overview

Jianmen is a bastion/jump server — Go backend (SSH/SFTP gateway, Admin API, DB proxy) + Vue 3 frontend (management UI). This skill covers the full build-and-launch pipeline for local development.

## Two Startup Modes

| 模式 | 前端 | 前端端口 | 热更新 | 适用场景 |
|------|------|---------|--------|---------|
| **分离模式（Windows 开发推荐）** | Vite 开发服务器独立运行 | 47101 | ✅ HMR | 前端开发调试 |
| 嵌入模式（`start.ps1`） | 前端 dist 嵌入 Go 二进制 | 47100（由后端提供） | ❌ 需重建 | 生产验证 / Docker 部署 |

**Windows 下开发调试默认使用分离模式**：后端用 `config.local.json`（`admin.dev: true`，不提供嵌入前端），前端用 Vite 开发服务器（支持热更新，`/api` 代理到后端 47100）。

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| Go | 1.23+ | `go version` |
| Node.js | 18+ | `node --version` |
| npm | 9+ | `npm --version` |

The backend creates `data/` at runtime (database, host keys, replays). No manual setup needed.

## Quick Start — 分离模式（Windows 开发推荐）

### Step 1: 停止旧进程

```bash
taskkill //F //IM jianmen.exe 2>/dev/null
taskkill //F //IM node.exe 2>/dev/null
sleep 1
```

### Step 2: 构建并启动后端

```bash
# 首次运行：从配置模板复制（如果还没有 config.local.json）
cp -n configs/config.example.json config.local.json 2>/dev/null || true

# 构建（嵌入的 dist 不会被使用，因为 dev: true）
mkdir -p logs bin
go build -o bin/jianmen.exe ./cmd/jianmen

# 启动后端（后台运行）
nohup ./bin/jianmen.exe -config config.local.json > logs/backend.log 2> logs/backend.err.log &
```

关键：`config.local.json` 中 `admin.dev` 必须为 `true`，这样后端不提供嵌入的前端，只提供 API。

### Step 3: 启动前端开发服务器

```bash
cd web
npm run dev -- --host 127.0.0.1 --strictPort > ../logs/frontend.log 2> ../logs/frontend.err.log &
```

Vite 开发服务器运行在 `127.0.0.1:47101`，`/api` 请求代理到 `http://localhost:47100`（后端 API）。

### Step 4: 验证

```bash
# 后端健康检查
curl -s --noproxy '*' http://127.0.0.1:47100/api/init/status

# 前端页面
curl -s --noproxy '*' -o /dev/null -w "%{http_code}" http://127.0.0.1:47101/

# 前端代理后端 API
curl -s --noproxy '*' http://127.0.0.1:47101/api/init/status
```

## 嵌入模式（start.ps1）

用于生产验证，把前端 dist 嵌入 Go 二进制，只启动后端一个进程。

```powershell
.\scripts\start.ps1
```

构建过程：`npm run build` → 复制到 `internal/frontend/dist/` → `go build` 嵌入 → 启动二进制。

## Port Reference

| 服务 | 地址 | 说明 |
|------|------|------|
| Admin API | `http://127.0.0.1:47100` | Go 后端 |
| 前端（分离模式） | `http://127.0.0.1:47101` | Vite 开发服务器，`/api` 代理到 47100 |
| 前端（嵌入模式） | `http://127.0.0.1:47100` | 由后端直接提供 |
| SSH/SFTP gateway | `0.0.0.0:47102` | SSH 代理端口 |

## Verification

| Check | Command |
|-------|---------|
| Admin API health | `curl -s --noproxy '*' http://127.0.0.1:47100/api/init/status` |
| Auth API | `curl -s --noproxy '*' -H "Authorization: Bearer dev-admin-token" http://127.0.0.1:47100/api/hosts` |
| Frontend page | `curl -s --noproxy '*' -o /dev/null -w "%{http_code}" http://127.0.0.1:47101/` |
| Frontend proxy | `curl -s --noproxy '*' http://127.0.0.1:47101/api/init/status` |

**Proxy note:** If a system-wide `http_proxy` is set (e.g. `127.0.0.1:7890`), curl routes localhost traffic through it and gets 502. Use `--noproxy '*'`.

## Key Config: admin.dev

`config.local.json` 中 `admin.dev: true` 控制后端是否提供嵌入的前端：

```json
{
  "admin": {
    "dev": true,
    "listen_addr": "127.0.0.1:47100"
  }
}
```

- `dev: true`：后端只提供 API，前端由 Vite 开发服务器提供（分离模式）
- `dev: false`：后端提供嵌入的前端 dist（嵌入模式 / 生产模式）

## Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `config.local.json` not found | First run | `cp configs/config.example.json config.local.json` |
| Port 47100/47101/47102 in use | Previous instance | `taskkill //F //IM jianmen.exe; taskkill //F //IM node.exe` |
| Frontend API calls fail | Backend not running | Start backend before frontend |
| `go build` fails | Missing Go deps | `go mod download` |
| `npm run dev` fails | `node_modules` missing | `cd web && npm install` |
| Embedded frontend shows stale content | Using old embedded build | Switch to 分离模式: set `admin.dev: true` |
