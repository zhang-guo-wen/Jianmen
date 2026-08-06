# Web SQL 控制台 CodeMirror 6 改造设计

日期：2026-08-06
状态：已批准

## 背景与目标

在线 SQL 控制台的编辑器目前是 `el-input textarea`（`SQLEditorPanel.vue`），无语法高亮、无自动补全、无语句级交互；且执行时**把全文发给后端**，后端 `singleSQLStatement` 只允许单条语句（`ErrSQLConsoleMultipleStatements`），多语句文本（含注释）直接报错，用户无法执行第二条语句。

目标：

1. 编辑器替换为 CodeMirror 6：语法高亮、自动补全、行号、搜索等编辑器能力；
2. 语法树级**语句切分**：识别全文中每条 SQL 语句的精确位置；
3. **行号执行按钮**：每条语句起始行显示 ▶ 按钮，点击执行该条；
4. **当前语句虚线框**：光标所在语句用虚线框实时高亮；
5. **表/列自动补全**：后端新增元数据接口，连接会话后自动加载表结构；
6. 现有执行、写确认、审计、RBAC 流程**全部保持不动**。

非目标（YAGNI）：多语句连续执行、SQL 格式化按钮、结果表格增强（保持 `el-table`）、语句执行历史。

## 现状分析

- 前端：`SQLEditorPanel.vue`（textarea）↔ `SQLResultPanel.vue`（el-table）↔ `SQLConsoleToolbar.vue`，`useSQLConsole` composable 持有 `sql` 全文 ref，`execute()` 发送全文；
- 后端：`internal/service/sql_console.go` 的 `Execute` → `inspectSQLStatement` → `singleSQLStatement`（状态机处理引号/转义/行注释/块注释/分号），**单条执行，多余内容报 `ErrSQLConsoleMultipleStatements`**；`confirm_write` 写确认；`SQLConsoleResult` 字段：`audit_session_id / query_kind / read_only / columns / rows / row_count / rows_affected / truncated / duration_ms`；
- 主题：项目 `main.css` 有浅色默认 + `[data-theme="dark"]` 暗色两套变量；
- 测试：vitest（`node:assert` 风格）+ vue mount 测试 + Go 测试；i18n 单文件 `web/src/i18n/index.ts`。

## 设计

### 1. 后端：元数据接口

```
GET /api/sql-console/sessions/{sessionId}/metadata?database={db}
→ 200 { "tables": [ { "name": "users", "columns": [ { "name": "id", "type": "bigint" } ] } ] }
```

| 层 | 文件 | 改动 |
|---|---|---|
| handler | `internal/handler/sqlconsole/handler.go` | 新增 `HandleMetadata`:校验会话归属 + `database` 在 `webSession.databaseAllowed` 白名单内 |
| 权限 | `internal/service/sql_console.go` | 新增 `Metadata()`:RBAC `ActionDBQuery` 校验,不建审计会话 |
| executor | `internal/service/sql_console_executor.go` | 新增 `Metadata()` 方法:查 `information_schema.TABLES` / `COLUMNS`(排除系统库,按 `database()` 过滤),单次连接查询 |
| 路由 | `cmd/jianmen` 路由注册处 | 挂载 `metadata` 路由 |

防护：表数量上限 500 张(超出截断);查询失败返回 502,前端静默降级。

### 2. 前端：语句切分(核心纯函数)

新增 `web/src/utils/sqlStatements.ts`：

```
parseSQLStatements(doc: string): SQLStatementRange[]
SQLStatementRange = { from, to, startLine, endLine, text }
```

- 用 lang-sql 的 Lezer 语法树遍历收集 `Statement` 节点,字符串/注释内分号不误切;
- 编辑器文档变化由 `ViewPlugin.update` 增量触发重算;解析不完整时容错(取最后一个 Statement 到文档末尾);
- 测试覆盖：单条/多条、行注释与块注释、字符串/反引号内分号、未闭合引号、空文档;与后端 `singleSQLStatement` 语义对拍。

### 3. 前端：编辑器组件

新增依赖(web/package.json)：

```
codemirror  @codemirror/lang-sql  @codemirror/view
@codemirror/language  @codemirror/state  @codemirror/autocomplete
```

`SQLEditorPanel.vue` 重写为 CM6 编辑器：

- **对外接口不变**：props `executing` / `disabled`,v-model `sql`,emit `execute` / `cancel`;
- v-model 双向同步：updateListener 写回 `sql` ref;外部改 `sql` 时 dispatch 进编辑器(防循环);
- `execute` 事件携带语句文本参数;`SQLConsoleWorkspace.handleExecute(statementText)` 置 `sql.value = statementText` 后走现有执行流程(写确认 PRECONDITION_FAILED → 确认弹窗 → 重发、404 重连、审计,全部复用);
- 三种触发方式统一为"执行单条语句":gutter ▶ 执行指定语句,Ctrl+Enter 与工具栏「执行」按钮执行光标所在语句——均由编辑器内部完成语句定位,事件一律携带语句文本;
- 主题：浅/深两套 CM6 主题(沿用项目配色),跟随 `data-theme` 用 Compartment 动态切换;
- onMounted 创建、onBeforeUnmount destroy、AbortController 生命周期管理。

### 4. 前端：gutter 执行按钮插件

新增 `web/src/components/sql-console/codemirror/executionGutter.ts`：

- 每条语句起始行渲染 ▶ 按钮,点击执行该条;
- 语句位置来自 `parseSQLStatements`;marker 复用 Map(对象引用稳定,避免 DOM 重建);
- `executing` 时按钮置灰并显示 loading 态,不可重复点击;
- 空文档/无语句 → 无按钮。

### 5. 前端：当前语句虚线框插件

新增 `web/src/components/sql-console/codemirror/statementDecorator.ts`：

- selection 变化 → 定位光标所在语句 → `Decoration.line` 给范围内每行加 class;
- CSS 虚线框：每行左边框虚线、首行上边框 + 尾行下边框、语句区域浅色背景(浅/深主题各一套);
- 光标在非语句区域(空白/纯注释)→ 无框。

### 6. 前端：自动补全

- 新增 `web/src/utils/sqlSchema.ts`：元数据 → lang-sql `{ self, children }` schema 格式转换;
- `sql({ dialect, schema })` 用 Compartment 动态 reconfigure(连接成功/切库后更新);
- 方言按账号 `instance_protocol` 选择 MySQL / PostgreSQL;元数据失败静默降级为关键字补全;
- 关键字/内置函数补全由 lang-sql 自带。

### 7. 数据流

```
连接会话成功/切换数据库
  → useSQLConsole.loadMetadata() → GET metadata(AbortController)
  → sqlSchema 转换 → Compartment reconfigure 编辑器补全
光标移动/文档编辑
  → 语法树 → 语句列表 → gutter 按钮 + 虚线框刷新
点击 ▶ / Ctrl+Enter
  → emit 语句文本 → SQLConsoleWorkspace: sql.value = 语句文本 → 现有 execute()
  → 写确认 / 404 重连 / 审计 —— 全部复用现有逻辑
```

### 8. 错误处理

| 场景 | 行为 |
|---|---|
| 元数据拉取失败 | 静默降级为关键字补全,不弹错误 |
| 语句切分为空(空文档) | gutter 无按钮;工具栏执行走现有 missingSQL 提示 |
| 执行写操作 | 现有 PRECONDITION_FAILED → 确认弹窗 → 重发,不变 |
| 连接过期 | 现有 404 自动重连提示,不变 |
| 组件卸载 | destroy CM6、abort 元数据请求 |

### 9. 测试

| 层 | 覆盖 |
|---|---|
| sqlStatements 单测 | 语句切分边界 + 后端语义对拍用例 |
| sqlSchema 单测 | 名称/类型转换、上限截断 |
| 挂载测试(happy-dom) | gutter ▶ 数量=语句数、点击触发 execute、虚线框 class 随 selection 变化、v-model 双向同步 |
| useSQLConsole 测试 | 现有用例更新(执行发送语句文本)、loadMetadata 触发时机 |
| 后端 Go 测试 | Metadata 权限拒绝/会话归属/白名单/信息查询 SQL 构造 |

### 10. 验收标准

1. 多语句文本:任意一条可通过 ▶ 按钮或光标 + Ctrl+Enter 执行;
2. 虚线框实时跟随光标,首尾行有虚线边框;
3. 写确认弹窗、审计 ID、截断提示行为与现状一致;
4. 浅/深主题下编辑器外观正常;
5. 补全:连接后输入 `FROM` 出现表名(带中文说明),列名带类型;断网时降级关键字补全。
