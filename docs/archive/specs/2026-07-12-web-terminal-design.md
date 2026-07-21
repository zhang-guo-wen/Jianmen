# Web Terminal — 浏览器交互式 SSH 终端 设计文档

日期：2026-07-12

## 概述

在 Jianmen 管理界面中增加浏览器内交互式 SSH 终端。用户点击"连接"后不再只能复制 SSH 命令到本地终端，而是可以直接在浏览器中打开全屏终端，实时操作目标主机。

后端 `/api/web-terminal` WebSocket 端点已完整实现（SSH PTY、双向 I/O、resize、会话录制），本次仅需前端实现。

## 范围

- **本期：** 交互式 SSH 终端（全屏页面）
- **不做：** 文件管理（SFTP 面板）、只读终端升级
- **后续迭代可扩展：** 文件管理面板、多 Tab 终端、数据库 Web Terminal

---

## 架构

```
┌─ 浏览器 ────────────────────────────────────────────────────┐
│                                                              │
│  QuickConnectView / HostsView                                │
│    │  点击 [在浏览器中打开]                                      │
│    ▼                                                         │
│  WebTerminalView.vue    ─── WebSocket ───▶  /api/web-terminal │
│    │  useWebTerminal.ts                       (后端，已实现)    │
│    │                                                                  │
│    ├─ xterm Terminal (disableStdin: false)                             │
│    ├─ @xterm/addon-fit (自适应容器大小)                                   │
│    └─ @xterm/addon-web-links (链接可点击)                                │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

## 路由设计

- **路径：** `/web-terminal?target_id=xxx`
- **加载方式：** 懒加载 `() => import('@/views/WebTerminalView.vue')`
- **认证：** 需要通过路由守卫（已登录），非公开路由
- **不在侧边栏菜单中显示**（不加入 `ALL_NAV_ITEMS`）
- **目标信息获取：** 终端视图挂载后通过 `apiClient.getTarget(targetId)` 获取目标名称/地址/端口用于工具栏展示

## 组件设计

### WebTerminalView.vue

```
┌─ 顶部工具栏（固定 48px）─────────────────────────────────────┐
│  ← 返回   目标: {target_name} ({host}:{port})       ● 已连接  │
└──────────────────────────────────────────────────────────────┘
┌─ xterm 终端区域（flex: 1，占满剩余空间）─────────────────────┐
│  #terminal-container                                        │
│  (xterm 挂载点)                                              │
└──────────────────────────────────────────────────────────────┘
```

**状态管理：**
- `connecting` — WebSocket 连接中
- `connected` — 已连接，终端可交互
- `disconnected` — 连接断开，显示原因，禁用输入
- `error` — 连接失败，显示错误信息

### useWebTerminal.ts (composable)

封装 WebSocket 连接 + xterm 绑定逻辑，输入参数：
- `targetId: string` — 目标资源 ID
- `token: string` — 认证令牌
- `terminalOptions?: ITerminalOptions` — 可选的 xterm 配置覆盖

返回值：
- `terminal: Ref<Terminal>` — xterm 实例
- `status: Ref<'connecting'|'connected'|'disconnected'|'error'>`
- `error: Ref<string>`
- `connect(container: HTMLElement): Promise<void>` — 挂载并连接
- `disconnect(): void` — 断开并清理
- `targetInfo: Ref<{name?: string, host?: string, port?: number}>`

## 数据流

```
用户按键
  → term.onData(key)
    → ws.send(key)                           // TextMessage/BinaryMessage
      → 后端 → SSH stdin → 目标主机

目标主机输出
  → SSH stdout/stderr
    → ws.onmessage(event)
      → term.write(event.data)               // 直接写入终端

窗口大小变化
  → ResizeObserver → fitAddon.fit()
    → ws.send(JSON.stringify({
        type: "resize",
        cols: term.cols,
        rows: term.rows
      }))
      → 后端 → session.WindowChange(rows, cols)
```

## WebSocket 连接规格

- **URL：** `{wsProtocol}://{host}/api/web-terminal?target_id={id}&token={token}&cols={cols}&rows={rows}&term=xterm-256color`
- **认证：** Bearer token 通过 `token` query 参数传递（与后端 `authenticateWebTerminal` 一致）
- **消息方向：**
  - 客户端 → 服务端：TextMessage（普通输入 / resize JSON）/ BinaryMessage
  - 服务端 → 客户端：BinaryMessage（终端输出）

## 入口行为

入口按钮（QuickConnectView / HostsView 连接弹窗）：

- 点击 [在浏览器中打开] → 直接导航到 `/web-terminal?target_id={id}`
- **不需要**先调用 `createUserSession`，因为后端 Web Terminal 端点内部会创建自己的 session 记录（`newWebTerminalRecorder`）
- 如果调用方已有 `session_id`（如 QuickConnectView 已调用 `createUserSession` 获取 compact username），可选择性传递但不强制

## 修改文件清单

### 新建

| 文件 | 说明 |
|------|------|
| `web/src/views/WebTerminalView.vue` | 终端页面组件 |
| `web/src/composables/useWebTerminal.ts` | WebSocket + xterm Composable |

### 修改

| 文件 | 改动 |
|------|------|
| `web/src/router/index.ts` | 添加 `/web-terminal` 懒加载路由 |
| `web/src/views/QuickConnectView.vue` | 连接弹窗增加 [在浏览器中打开] 按钮 |
| `web/src/views/HostsView.vue` | 连接弹窗增加 [在浏览器中打开] 按钮 |
| `web/package.json` | 添加 `@xterm/addon-fit` `@xterm/addon-web-links` |

### 不改动

- 后端代码（零改动）
- `web/src/api/client.ts`（WebSocket 在 composable 中直接建立）
- 国际化文件（复用已有翻译键）

## 错误处理

| 场景 | 处理 |
|------|------|
| WebSocket 连接失败 | 终端区域显示错误提示 + 重试按钮 |
| 服务端关闭连接（SSH 会话结束） | 终端显示 "连接已关闭"，禁用输入 |
| 网络中断 | 终端显示 "连接断开"，自动重连（最多 3 次，间隔 2s） |
| 认证失败（401） | 跳转登录页 |
| 目标不存在（404） | 显示错误提示，提供返回按钮 |
| 目标已禁用（403） | 显示错误提示 |
| Resize 参数无效 | 静默忽略，使用上次有效尺寸 |

## 测试

- `npm run typecheck` — 类型检查
- `npm run build` — 构建验证
- 手动测试：完整流程（快速连接 → 浏览器终端 → 登录目标主机 → 执行命令 → 断开）

## 后续迭代方向

- SFTP 文件管理面板（需要后端 SFTP subsystem WebSocket 通道）
- 多 Tab 终端（同时连接多台主机）
- 数据库 Web Terminal（复用同样架构，后端换 MySQL/Postgres 协议）
- 终端主题切换
- 字体大小调节
