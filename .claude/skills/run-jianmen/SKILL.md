---
name: run-jianmen
description: Use when asked to run, launch, start, test, or smoke-test the Jianmen bastion application (backend or frontend)
---

# Run Jianmen

## Overview

Jianmen is a bastion/jump server — Go backend (SSH/SFTP gateway, Admin API, DB proxy) + Vue 3 frontend (management UI). This skill covers the full build-and-launch pipeline for local development.

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| Go | 1.23+ | `go version` |
| Node.js | 18+ | `node --version` |
| npm | 9+ | `npm --version` |

The backend creates `data/` at runtime (database, host keys, replays). No manual setup needed.

## Quick Start

### One-Click (Recommended)

```powershell
.\start.ps1
```

Handles cleanup, config, build, npm install (first time), and starts both services in background jobs. Output shows URLs and access token.

### Manual Steps

#### 0. Clean Up Old Instances

```powershell
Get-Process -Name "bastion-core" -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process -Name "node" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1
```

#### 1. Backend

```powershell
# Copy config template (first time only)
if (-not (Test-Path config.local.json)) { Copy-Item config.example.json config.local.json }

# Build and run
go build -o bin\bastion-core.exe .\cmd\bastion-core
Start-Job -Name "bastion-core" { Set-Location $using:PWD; .\bin\bastion-core.exe -config config.local.json }
```

#### 2. Frontend

```powershell
cd web
if (-not (Test-Path node_modules)) { npm install }
Start-Job -Name "vite-dev" { Set-Location "$using:PWD\web"; npm run dev }
```

## Verification

| Check | Command |
|-------|---------|
| Admin API (curl) | `curl -s --noproxy '*' http://127.0.0.1:47100/api/health` |
| Admin API with auth | `curl -s --noproxy '*' -H "Authorization: Bearer dev-admin-token" http://127.0.0.1:47100/api/hosts` |
| Web UI (PowerShell) | `(Invoke-WebRequest 'http://127.0.0.1:47101/' -UseBasicParsing).StatusCode` |
| Frontend proxy to backend | `(Invoke-WebRequest 'http://127.0.0.1:47101/api/hosts' -UseBasicParsing -Headers @{'Authorization'='Bearer dev-admin-token'}).Content` |
| SSH gateway | `ssh -p 47102 admin@127.0.0.1` (password: `admin`) |

**Proxy note:** If a system-wide `http_proxy` is set (e.g. `127.0.0.1:7890`), curl routes localhost traffic through it and gets 502. Use `--noproxy '*'` or PowerShell `Invoke-WebRequest` as fallback.

## Port Reference

| Service | Bind Address | Config Key |
|---------|-------------|-------------|
| Admin API | `127.0.0.1:47100` (IPv4) | `admin.listen_addr` |
| Vue dev server | `127.0.0.1:47101` (IPv4) | Vite config `server.host` + `server.port` |
| SSH/SFTP gateway | `0.0.0.0:47102` | `listen_addr` |

The Vue dev server proxies `/api` requests to the Admin API (default `http://localhost:47100`), configured in `web/vite.config.ts`.

## Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `config.local.json` not found | First run, config not copied | Copy `config.example.json` → `config.local.json` |
| Port 47100/47101/47102 in use | Previous instance still running | Run cleanup step (section 0) to kill old processes |
| Backend starts but curl returns empty/502 | System `http_proxy` env var routing localhost through external proxy | Use `--noproxy '*'` with curl, or PowerShell `Invoke-WebRequest` |

| Frontend API calls fail | Backend not running | Start backend before frontend |
| `go build` fails | Missing Go dependencies | Run `go mod download` |
| `npm run dev` fails | `node_modules` missing or stale | `cd web` then run `npm install` |

## Driving the App

After launch, drive it to confirm it works:

```powershell
# Smoke test: Admin API with auth token
(Invoke-WebRequest 'http://127.0.0.1:47100/api/hosts' -UseBasicParsing -Headers @{Authorization='Bearer dev-admin-token'}).Content

# Verify frontend serves HTML
(Invoke-WebRequest 'http://127.0.0.1:47101/' -UseBasicParsing).StatusCode

# Verify frontend proxies /api to backend
(Invoke-WebRequest 'http://127.0.0.1:47101/api/hosts' -UseBasicParsing -Headers @{Authorization='Bearer dev-admin-token'}).Content
```
