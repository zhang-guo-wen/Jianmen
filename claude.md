每次回答，请用中文回答我。注释使用中文，提交代码使用中文

开发新功能的时候，要使用gitworktree 不要直接改项目

## 多人协作合并准则

日期：2026-06-27

- 合并到 dev 之前，必须先将最新 dev 合并到自己的 worktree 分支。
- 即：`git merge dev`（或 `git rebase dev`）→ 解决冲突 → 验证编译测试 → 再合并回 dev。
- 禁止不拉取最新 dev 直接合并，否则会静默覆盖别人的改动。
- 验证步骤至少包括：
  - `npm run typecheck`（前端）
  - `npm run build`（前端）
  - `go build ./...`（后端）
  - `go test ./... -count=1`（后端）

本产品还没有发布，不需要考虑兼容性，直接该重构的就重构。不用考虑以前的数据，功能失效。
如果是在worktree目录下，不启动服务，要回到主目录，合并代码回去，启动

## 编译打包

- Windows: `.\build.ps1`
- Linux/macOS/Git Bash: `./build.sh`

脚本会先构建前端（`npm run build`），将产物复制到 `internal/frontend/dist/`，然后交叉编译 Windows 和 Linux 两个平台的二进制文件，输出到 `dist/` 目录。

# Jianmen 问题总结与后续准则

日期：2026-06-24

## 资源模型问题

- 主机不是权限和连接的最小资源，主机只是容器。
- 一个主机应对应多个账号，最小资源应是“主机的某个账号”，即 `host_account`。
- 数据库同理，数据库实例只是容器，最小资源应是“数据库实例的某个账号”，即 `database_account`。
- RBAC、审计、快速连接、删除、禁用、有效期等能力都应围绕账号资源展开，而不是围绕主机或数据库实例本身。

## 静态配置问题

- 不应从配置文件加载静态主机账号或静态数据库实例给页面使用。
- 页面管理系统里，资源应全部来自管理页面创建和维护。
- 不应出现“配置文件账号不能删除”“配置文件实例不能删除”这类状态。
- 删除、禁用、编辑都应对页面管理资源生效。
- `config.example.json` 不应预置 demo 主机账号或数据库代理，避免用户误以为这些是页面创建的数据。

## 主机管理页面问题

- 主机列表应只显示必要字段：主机名称、IP/端口、账号数量、备注、操作。
- 主机账号不应直接铺在主机列表里，应点击账号数量或账号入口后再懒加载对应账号。
- 主机列表需要分页，默认 20 条。
- 主机需要分组；账号也需要分组。
- 分组选择应支持下拉输入，不存在时允许输入后自动作为新分组使用。
- 主机名称默认应等于 `主机地址:端口`。
- 主机地址可能是 IP、域名或带端口的地址；如果地址里带端口，应解析并同步端口字段，冲突时要提示。
- 主机状态和账号状态应在列表操作区切换，不应放在编辑弹窗里。

## 主机账号表单问题

- 用户不应理解后端服务器文件系统，所以“私钥路径”不应出现在页面新增/编辑流程里。
- 产品上账号认证方式应简化为：
  - 密码
  - 私钥
- 私钥应支持两种输入：
  - 浏览器选择本地私钥文件，前端读取文件内容。
  - 手动粘贴私钥内容。
- 提交给后端的应是私钥内容 `private_key_pem`，不是用户本机路径，也不是后端服务器路径。
- 私钥解锁口令是 private key passphrase，不是目标主机登录密码。
- 登录账号应是核心字段，账号名称默认等于登录账号。
- 账号名称、账号分组、备注等非必填字段应放在“更多设置”里。
- 有效期需要支持快捷设置：8 小时、7 天、1 年、永久，同时支持手动选择时间。
- 编辑账号时如果密码/私钥留空，应保留原密钥或密码。

## 快速连接问题

- 需要独立快速连接页面或入口。
- 列表操作里应提供“连接”按钮。
- 点击连接后弹窗展示连接配置，并支持一键复制。
- SSH/SFTP 命令中的认证是堡垒机用户认证，不是目标主机账号密码。
- 目标账号通过 `用户名+账号ID@堡垒机` 这种方式指定。
- 连接弹窗必须明确复制目标：SSH 命令、SFTP 命令、Web Terminal 路由、资源标识。

## 数据库管理问题

- 数据库管理不应放在数据库审计页面里。
- 需要独立数据库管理页面。
- 数据库实例需要完整 CRUD。
- 数据库账号需要完整 CRUD。
- 数据库账号是数据库权限、审计、连接控制的最小资源。
- 数据库实例新增/编辑/删除后，代理监听应动态生效，不能要求用户重启服务。
- 数据库账号变更也需要刷新代理配置，因为账号影响登录识别和 RBAC 资源。

## RBAC 问题

- 权限管理页面不能是空数据或概念页面，需要有可操作的角色、权限、资源和绑定关系。
- RBAC 的资源类型至少应覆盖：
  - `host_account`
  - `database_account`
- 需要能把用户分配到角色。
- 需要能把角色绑定到资源和操作。
- 需要能查询用户的有效权限。
- 默认数据应足够让用户理解系统如何授权，而不是空白。
- 权限判断应接入 SSH 连接、数据库连接等实际路径。

## 布局与弹窗问题

- 新增主机和新增账号弹窗宽度、高度应统一，避免视觉跳动。
- 有输入项很多的弹窗，应使用“更多设置”折叠，不应无限拉长。
- 弹窗体内部可滚动，但页面主体不应被弹窗或表格撑出滚动条。
- 主机管理页表格不应固定高度撑出整页滚动条，应该使用可用视口高度，数据多时只在表格内部滚动。
- 表格列宽要控制，状态、认证方式等短字段不能占太宽。
- 操作按钮应靠右、间距一致，删除和禁用按钮不能挤在一起。
- 表格中的操作按钮文本要短，避免撑出横向滚动条。

## 安全与数据暴露问题

- API 返回主机账号详情时不能泄露密码、私钥内容、私钥解锁口令。
- 私钥路径只作为历史兼容字段，不应在页面产品入口继续暴露。
- 前端上传私钥内容后，应避免在列表或详情接口回显。
- 删除和禁用操作要针对账号资源，避免误删主机容器导致理解混乱。
- 账号有效期、禁用状态、主机禁用状态都要在连接路径实际生效。

## Go 后端开发规范

日期：2026-06-30

### 包组织

| 包 | 职责 |
|---|------|
| `cmd/` | 入口点，只做依赖组装和 `main()`，不放业务逻辑 |
| `internal/model/` | 纯 GORM 实体定义，每个实体一个文件 |
| `internal/store/` | 数据访问接口 + 实现，接口定义和错误哨兵放 `store.go` |
| `internal/server/<name>/` | 服务器启动、路由注册、中间件 |
| `internal/handler/<name>/` | HTTP handler 函数，按资源拆分文件 |
| `internal/service/` | 纯业务逻辑，不依赖 HTTP/SSH 框架，供 handler 和 server 共享 |
| `internal/proxy/` | 代理会话协议处理 |
| `internal/config/` | 配置结构体和加载函数，不包含业务类型 |
| `internal/util/` | 无状态工具函数 |

### 分层原则

```
cmd/                  ← 组装依赖、启动
  ↓
internal/server/      ← 协议层（端口监听、路由、中间件）
  ↓
internal/handler/     ← 请求解析、响应序列化、调用 service
  ↓
internal/service/     ← 纯业务逻辑（不依赖 http.Request / ssh.Session）
  ↓
internal/store/       ← 数据访问
internal/model/       ← 实体定义
```

**规则：**
- handler 不做业务判断，只做参数校验和响应格式化
- service 不做 SQL 操作，通过 store 接口访问数据
- server 不做请求处理，只做路由和中间件
- model 不 import 其他内部包（加密字段类型除外）
- store 接口使用 `context.Context` 作为第一个参数

### handler 拆分规则

handler 包按资源拆分文件，每个文件包含该资源全套 handler 方法。

### model 拆分规则

- 每个 GORM 实体一个文件，文件名为实体名的 snake_case
- 关联紧密的实体（如 User 和 UserPublicKey）可放同一文件

### View 类型

- View/DTO 类型属于表现层，定义在 `internal/handler/<name>/` 或独立的 `internal/dto/` 包
- 不在 `store.go` 里堆 View 类型
- 不在 model 包里定义 JSON 标签

### server 包规则

- `server.go` 只放 Server 结构体和 `New()` 构造函数
- 路由注册放在 `routes.go`
- 中间件放在 `middleware.go`
- server 通过 handler 实例调用 handler 方法

### 接口设计

- 接口在使用方定义，不在实现方导出（Go 核心惯例）
- 接口方法按资源分组，一个接口对应一个资源实体
- 哨兵错误（ErrXxxNotFound）和接口定义同包
- 外部类型不进接口签名，接口定义自己的参数/返回类型
- 接口方法第一个参数必须是 `context.Context`

### 构造函数

- 构造函数命名 `New`，带依赖的用 `NewXxx`
- 通过构造函数注入依赖，函数签名体现所有外部依赖
- 不接受 `nil` 依赖，`nil` 意味着"不需要"而非"忘记传"

### 错误处理

- 使用 `fmt.Errorf("...: %w", err)` 包装错误，保留调用链
- 错误字符串小写开头，不以句号结尾
- 用 `errors.Is` / `errors.As` 做错误判断，不用 `==`
- 返回 error 时其他值应为零值，调用方不应依赖 error 路径的返回值

### 命名

- handler 方法以 `handle` 开头
- 文件名和包名一致
- 避免 stutter：用 `rbac.Checker` 而不是 `rbac.RBACChecker`
- 测试文件命名 `<file>_test.go`，与源文件同目录

### 单文件行数硬上限

| 层级 | 上限 |
|------|------|
| 入口 `cmd/` | 150 |
| model 文件 | 200 |
| handler 文件 | 500 |
| service 文件 | 500 |
| store 实现文件 | 500 |
| server 路由文件 | 300 |
| 代理会话文件 | 600 |
| 通用工具 | 200 |

超过上限时，必须先拆分再继续添加新功能。

### 测试规范

- 单元测试和源文件同目录，白盒测试
- 集成测试也和源文件同目录（Go 惯例），用 build tag `//go:build integration` 区分
- 不需要额外基础设施的集成测试（如 SQLite 内存库）不加 build tag，和单元测试一起跑
- 需要 Docker/外部服务的集成测试加 `//go:build integration`，CI 中通过 `-tags=integration` 执行
- handler 测试 mock store 接口，不连真实数据库
- 测试辅助函数放 `test_helper.go`

## 实现和测试问题

- 每次资源模型变化后，后端 API、前端页面、RBAC、审计、快速连接必须一起检查。
- 静态配置迁移为页面管理后，要清理测试里“静态资源不能删除”的旧假设。
- 动态数据库代理需要测试：新增启用后端口监听，删除后端口关闭。
- 前端改布局后必须跑：
  - `npm run typecheck`
  - `npm run build`
- 后端改资源模型或代理逻辑后必须跑：
  - `go test ./... -count=1`

## 新增页面/功能的检查清单

日期：2026-07-13

新增一个带导航菜单的功能页面时，必须同时修改以下位置，缺一不可：

### 前端必改文件

| 文件 | 改什么 | 漏改后果 |
|------|--------|----------|
| `web/src/App.vue` | `ALL_NAV_ITEMS` 数组中添加菜单项（path、icon、labelKey、menuKey） | 侧边栏看不到菜单入口 |
| `web/src/App.vue` | 顶部 `import` 中添加图标（如 `Key`、`Folder` 等） | 编译报错 |
| `web/src/router/index.ts` | `routeMenuMap` 中添加路径→菜单键映射 | 路由守卫误判无权限，跳转到 quickConnect |
| `web/src/router/index.ts` | 添加路由定义（path、name、component、meta） | 访问 404 |
| `web/src/i18n/index.ts` | zhCN 和 enUS 中都添加 `nav.xxx` 和 `route.xxx.title/description` 翻译 | typecheck 报错或菜单显示原始 key |

### 后端必改文件

| 文件 | 改什么 | 漏改后果 |
|------|--------|----------|
| `internal/server/admin/server.go` | `menuOrder` 中添加 `{key, action}` | `/api/me/menus` 不返回该菜单，前端看不到 |
| `internal/server/admin/server.go` | `ListenAndServe()` 中注册路由 `s.muxHandle(mux, "/api/xxx", ...)` | API 404 |
| `internal/rbac/resources.go` | 添加 `ActionXxxCreate/Update/Delete/View` 常量 | 权限检查失效 |

### 本项目的 i18n 注意事项

- `@/i18n` 导出的 `useI18n().t()` **只接受一个参数**（翻译 key），不支持 vue-i18n 的插值和 fallback。
- 需要插值时用 `t('key').replace('{name}', value)` 手动替换。
- `TranslationKey` 是 `zhCN` 的 keyof，**enUS 必须包含完全相同的 key**，否则 typecheck 报错。
- 路由 meta 中的 `titleKey` / `descriptionKey` 也必须是有效的 TranslationKey。

## Element Plus 组件按需注册

日期：2026-07-14

本项目 `main.ts` 中 Element Plus 采用按需注册（逐一 import + `app.use()`），不是全局全量注册。
**使用任何之前未用过的 Element Plus 组件时，必须先检查 `main.ts` 是否已注册该组件。**

### 排查方式

在 `main.ts` 的 import 列表和 `elementComponents` 数组中搜索组件名。如果找不到，说明该组件未注册，
模板中的 `<el-xxx>` 会被 Vue 当成普通 HTML 元素渲染，不会有任何交互行为。

### 本次教训

`el-dropdown` / `el-dropdown-menu` / `el-dropdown-item` 在页面模板中使用但不响应点击，
排查方向先后误判为：
1. `el-table` 的 `fixed="right"` 列克隆 DOM 导致事件丢失
2. `el-popover` 替代方案
3. `el-button` 组件边界导致下拉事件绑定失败
4. 手动实现 popup 菜单

最终发现根因仅仅是 `ElDropdown` / `ElDropdownMenu` / `ElDropdownItem` 未在 `main.ts` 注册。
加入注册后，`el-dropdown` + `el-button link` 在 `el-table` 列中完全正常工作。

### 正确做法

当需要新增 Element Plus 组件时：
1. 搜索 `main.ts` 确认组件是否已注册
2. 如未注册，在 import 和 `elementComponents` 数组中各添加一行
3. **不要**因为组件不工作就开始怀疑框架/布局/事件传播 — 先检查注册

### 已注册的组件（截至 2026-07-14）

```
ElAlert ElAside ElButton ElCard ElCheckbox ElCheckboxGroup
ElCollapse ElCollapseItem ElConfigProvider ElContainer
ElDatePicker ElDescriptions ElDescriptionsItem ElDialog
ElDivider ElDrawer ElDropdown ElDropdownItem ElDropdownMenu
ElEmpty ElForm ElFormItem ElHeader ElIcon ElInput
ElInputNumber ElLoading ElMain ElMenu ElMenuItem
ElOption ElOptionGroup ElPagination ElRadio ElRadioButton
ElRadioGroup ElSegmented ElSelect ElSlider ElSwitch
ElTable ElTableColumn ElTabPane ElTabs ElTag ElTooltip
```

## 系统化调试教训

日期：2026-07-14

本次"更多按钮点不了"的排查经历了 5 轮错误尝试后才找到根因。关键教训：

### 排查路径回顾

| 轮次 | 假设 | 方案 | 结果 |
|------|------|------|------|
| 1 | `fixed="right"` 列克隆 DOM 阻断事件 | `el-dropdown` + `teleported` | ❌ |
| 2 | `el-dropdown` 本身在 fixed 列不兼容 | 换 `el-popover` | ❌（内容直接内联展开） |
| 3 | `fixed="right"` 是罪魁祸首 | 去掉 `fixed="right"`，恢复 `el-dropdown` | ❌ |
| 4 | `el-button` 作为触发器事件传播异常 | 换 `<span>` 做触发器 | ❌ |
| 5 | Element Plus 弹出组件全都不兼容 | 手写 popup（`Teleport` + 坐标计算） | ✅（但浪费） |
| **根因** | **`ElDropdown` 根本没注册** | `main.ts` 加注册 | ✅ |

### 核心教训

1. **组件不响应 → 先查注册，再查 DOM。** 按需注册的项目里，模板中未注册的组件会被当成普通 HTML 元素静默渲染，不报错、不警告、不工作。
2. **3 次修复失败后必须质疑根本假设。** 第 4 次失败时就应该问"这个组件真的存在吗？"而不是继续在 DOM/事件层面深挖。
3. **手写实现虽然是可靠的兜底方案，但在用它之前应该先排除配置/注册问题。**
4. **不要看到一个可能的原因就跳进去修。** `fixed="right"` 确实在某些场景下有问题，但它不是本次的根因，"看起来像"不代表"真的是"。

### 排查清单

遇到 Element Plus 组件不工作时，按此顺序排查：
1. 组件是否在 `main.ts` 中注册？（5 秒，搜索一下）
2. 浏览器控制台有没有 Vue 警告？（"Failed to resolve component"）
3. 浏览器 DevTools Elements 面板中组件是否渲染为 `<el-xxx>` 原始标签？
4. 然后再考虑 DOM 结构、事件传播、CSS 等问题

## 操作列按钮设计准则

日期：2026-07-14

### 主机管理和数据库管理的操作列

统一为三按钮布局：`连接` | `编辑` | `更多 ▼`

| 按钮 | 行为 |
|------|------|
| 连接 | 先查账号数：1 个 → 直接弹连接窗；0 个 → 提示无账号；多个 → 打开账号管理弹窗 |
| 编辑 | 打开编辑弹窗（主机/实例） |
| 更多 ▼ | 下拉菜单：审计日志 / 在线会话 / 权限管理 / 删除（红色） |

### 连接按钮智能行为实现要点

- 必须先 `setSelectedHost/Instance` + 加载账号列表，再判断 `accounts.value.length`
- 单账号直接调 `openConnectionDialog(accounts.value[0])`
- 多账号打开 `accountsDialogVisible = true` + `ElMessage.info('请从账号列表中选择要连接的账号')`
- 服务器端分页时 `accountPageSize` 可能不足以判断真实账号数，此时应信任 `account_count` 字段（需要后端返回），或者设 `pageSize=1` 快速试探

### 删除函数清理

移除"新增账号"下拉项后，`openCreateAccountForHost()` 和 `openCreateAccountForInstance()` 变为未使用函数，需要一并删除。移除后还要检查相关 import（如 `nextTick`）是否仍有其他引用。

## Worktree 开发注意事项

日期：2026-07-14

### 合并冲突

本次在合并 worktree 分支回 dev 时，`DatabaseView.vue` 的 CSS 区域产生冲突：
- dev 分支有人添加了 `.protocol-tag` 和 `.account-mgmt-btn` 样式
- worktree 分支添加了 `.table-actions` 和 `.danger-dropdown-item` 样式
- 解决方式：保留两边所有样式，按顺序排列

### worktree 中 npm 依赖

worktree 创建后不包含 `node_modules/`，需要先 `cd web && npm install`。
`npm run typecheck` 报 `vue-tsc not found` 时就是依赖没装。

### 提交路径

从 worktree 目录中提交时，路径相对于 worktree 根目录。退出 worktree 回到主目录后合并。
注意 `web/` 目录是自己的 git 工作区，在主目录中 `src/views/HostsView.vue` 的路径是相对于 `web/`。