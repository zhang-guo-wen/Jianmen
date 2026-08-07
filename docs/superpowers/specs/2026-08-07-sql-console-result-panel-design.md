# SQL 控制台结果面板优化设计

日期：2026-08-07
状态：已批准

## 背景与目标

在线 SQL 控制台的结果面板（`web/src/components/sql-console/SQLResultPanel.vue`）体验待优化：

1. 结果区域可折叠，默认折叠，查询后自动展示；
2. 表格列宽可拖动；
3. 表头不换行；
4. 行距更密。

## 现状分析

- `SQLResultPanel.vue`：`el-table` + `el-table-column min-width="160"`（无固定 width，Element Plus 列宽拖动需固定 `width` 才出现拖动手柄）；表头默认 `white-space: normal` 会换行；行距为默认档（cell padding 8px 0）。
- `SQLConsoleWorkspace.vue`：编辑器 + 结果面板上下布局，执行结果通过 `result` ref 传入面板。
- 结果面板当前无挂载测试。

## 设计

### 1. 折叠（默认折叠，查询后展示，折叠让位编辑器）

`SQLResultPanel.vue` 折叠状态改为 `v-model:collapsed`（默认 `true`，父组件可控）：

- header 右侧加折叠/展开按钮（箭头图标），标题区也可点击切换；
- 内容区（表格/错误 alert/空态）用 `el-collapse-transition` 包裹，折叠时仅显示 header；
- 自动展开：`watch(result)` 有值、`watch(error)` 非空时展开（错误也要可见）；初始未执行保持折叠；
- 折叠时 header 指标区（耗时/行数/审计会话 ID）保留显示；
- 手动折叠后执行新查询 → 自动重新展开。

**折叠让位布局**（最终审查补充）：折叠时结果面板收缩为 header 高度（`flex: 0 0 auto`），编辑器面板吸收剩余空间（`flex: 1 1 0`）；展开时恢复现状（编辑器 `flex: 0 0 34%`、结果面板 `flex: 1`）。折叠状态由 `SQLConsoleWorkspace.vue` 持有并驱动两个面板的 class 切换，实现"折叠到下面、编辑器变大"的空间收益。

### 2. 列宽可拖动

- `el-table-column` 由 `min-width="160"` 改为 `width="160"`，启用原生列宽拖动（resizable 默认开启）；
- 拖动宽度为组件内存态，新查询（列结构不同）后自然重置，不持久化（YAGNI）。

### 3. 表头不换行

- scoped CSS：`:deep(.el-table th .cell) { white-space: nowrap }`。

### 4. 行距更密

- `el-table` 加 `size="small"`（内置紧凑档：cell 纵向 padding 8px→4px、横向 12px→8px）；
- scoped CSS 微调 `.cell` line-height（23px→20px）。

## 影响面

- `web/src/components/sql-console/SQLResultPanel.vue`（折叠状态模型化 + 模板 + scoped CSS）；
- `web/src/components/sql-console/SQLConsoleWorkspace.vue`（持有折叠状态、面板 class 切换、布局 CSS）；
- `web/src/i18n/index.ts`、`web/package.json`（测试挂载）、新建挂载测试；
- 后端无需改动。

## 测试

新增 `web/src/components/sql-console/SQLResultPanel.mount.test.ts`：

| 用例 | 断言 |
|---|---|
| 默认折叠 | 内容区隐藏、header 可见、折叠按钮存在 |
| 有结果自动展开 | 传入 result 后内容区可见 |
| 错误自动展开 | 传入 error 后内容区可见 |
| 手动切换 | 点击折叠按钮后内容区隐藏/显示切换 |

列宽拖动、表头不换行、行距为视觉行为，手工验收。

## 验收标准

1. 打开控制台：结果区域折叠，只显示 header；
2. 执行 SQL：结果自动展开显示表格/错误；
3. header 点击或箭头可手动折叠/展开，折叠时指标（耗时/行数/审计 ID）可见；
4. 拖动列分隔线可调列宽；
5. 表头不换行；行距明显变密。
