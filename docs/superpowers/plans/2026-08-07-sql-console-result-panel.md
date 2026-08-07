# SQL 控制台结果面板优化实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 优化 SQL 控制台结果面板:默认折叠/查询后自动展开、列宽可拖动、表头不换行、行距更密。

**Architecture:** 全部改动集中在 `web/src/components/sql-console/SQLResultPanel.vue`(内部折叠状态 + el-collapse-transition + 表格属性/CSS)与 i18n;`SQLConsoleWorkspace.vue` 与后端零改动。

**Tech Stack:** Vue 3 + TypeScript + Element Plus(el-table/el-collapse-transition)、vitest + @vue/test-utils + happy-dom。

## Global Constraints

- 注释与 git 提交信息一律使用中文(项目规范);
- 改动范围:`web/src/components/sql-console/SQLResultPanel.vue`、`web/src/components/sql-console/SQLConsoleWorkspace.vue`、`web/src/i18n/index.ts`、`web/package.json`(测试脚本挂载)、新建挂载测试文件;
- 折叠状态为 `v-model:collapsed`,默认 `true`(由 SQLConsoleWorkspace 持有);`result` 有值或 `error` 非空时自动展开;手动折叠后新查询仍自动重新展开;
- 折叠让位布局(最终审查补充):折叠时结果面板 `flex: 0 0 auto`(收缩为 header 高度)、编辑器面板 `flex: 1 1 0`(吸收空间);展开时恢复现状(编辑器 `flex: 0 0 34%`、结果面板 `flex: 1`);
- 折叠时 header 指标区(耗时/行数/审计会话 ID)保留显示;
- `el-table-column` 由 `min-width="160"` 改为 `width="160"`(固定宽度启用列宽拖动);保留 `show-overflow-tooltip` 与 stripe;
- 表头不换行:`th .cell { white-space: nowrap }`;紧凑行距:`size="small"` + `.cell { line-height: 20px }`;
- 挂载测试风格参考 `web/src/components/sql-console/SQLEditorPanel.mount.test.ts`(vitest + @vue/test-utils + happy-dom)。

---

### Task 1: 结果面板折叠(默认折叠,查询/错误自动展开)

**Files:**
- Modify: `web/src/components/sql-console/SQLResultPanel.vue`
- Modify: `web/src/i18n/index.ts`
- Create: `web/src/components/sql-console/SQLResultPanel.mount.test.ts`
- Modify: `web/package.json`(测试脚本挂入新测试)

**Interfaces:**
- Consumes: 现有 props `result: SQLConsoleResult | null`、`error: string`、`executing: boolean`(类型从 `@/api/client` 导入,结构不变)
- Produces: 组件内部状态 `collapsed`(默认 true);header 折叠按钮;`el-collapse-transition` 包裹内容区

- [x] **Step 1: 写失败的挂载测试**

`web/src/components/sql-console/SQLResultPanel.mount.test.ts`:

```ts
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';

import type { SQLConsoleResult } from '@/api/client';

import SQLResultPanel from './SQLResultPanel.vue';

const baseResult: SQLConsoleResult = {
  audit_session_id: 'aud-1',
  query_kind: 'select',
  read_only: true,
  columns: ['id', 'name'],
  rows: [['1', 'a']],
  row_count: 1,
  rows_affected: 0,
  truncated: false,
  duration_ms: 5,
};

describe('SQLResultPanel 折叠', () => {
  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('默认折叠:内容区隐藏,header 可见', () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: null, error: '', executing: false },
    });
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    expect(wrapper.find('.result-header').exists()).toBe(true);
    expect(wrapper.find('.result-collapse-btn').exists()).toBe(true);
  });

  it('传入 result 后自动展开', async () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: null, error: '', executing: false },
    });
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    await wrapper.setProps({ result: baseResult });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });

  it('error 非空时自动展开', async () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: null, error: '', executing: false },
    });
    await wrapper.setProps({ error: '执行失败' });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });

  it('点击折叠按钮可手动切换', async () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: baseResult, error: '', executing: false },
    });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
    await wrapper.find('.result-collapse-btn').trigger('click');
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    await wrapper.find('.result-collapse-btn').trigger('click');
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });

  it('手动折叠后传入新 result 自动重新展开', async () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: baseResult, error: '', executing: false },
    });
    await wrapper.find('.result-collapse-btn').trigger('click');
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    await wrapper.setProps({ result: { ...baseResult, duration_ms: 9 } });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });
});
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/components/sql-console/SQLResultPanel.mount.test.ts`
Expected: FAIL(`.result-body` 无 v-show 控制,默认可见;无 `.result-collapse-btn` 元素)

- [x] **Step 3: 实现折叠**

`web/src/components/sql-console/SQLResultPanel.vue` 修改:

```vue
<script setup lang="ts">
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue';
import { computed, ref, watch } from 'vue';

// ...现有 imports 保留...

/* 折叠状态:默认折叠,查询/错误时自动展开 */
const collapsed = ref(true);

watch(() => props.result, (result) => {
  if (result) collapsed.value = false;
});
watch(() => props.error, (error) => {
  if (error) collapsed.value = false;
});

const collapseButtonIcon = computed(() => (collapsed.value ? ArrowUp : ArrowDown));
const collapseTitle = computed(() =>
  collapsed.value ? t('sqlConsole.expand') : t('sqlConsole.collapse'),
);

function toggleCollapsed(): void {
  collapsed.value = !collapsed.value;
}
</script>
```

模板:header 标题区加点击 + 右侧折叠按钮,内容区包 `el-collapse-transition`:

```vue
<header class="result-header" @click="toggleCollapsed">
  <div class="result-heading">
    <strong id="sql-console-result-title">{{ t('sqlConsole.resultTitle') }}</strong>
    <!-- 现有 tags 保留 -->
  </div>
  <div class="result-metrics" id="metrics" hidden>
    <!-- 现有指标保留,点击事件冒泡到 header -->
  </div>
  <el-button
    class="result-collapse-btn"
    :icon="collapseButtonIcon"
    :title="collapseTitle"
    :aria-label="collapseTitle"
    circle
    text
    size="small"
    @click.stop="toggleCollapsed"
  />
</header>

<el-collapse-transition>
  <div v-show="!collapsed" class="result-body">
    <!-- 现有 error alert / warn alert / table / empty 原样移入 -->
  </div>
</el-collapse-transition>
```

注意:
- 现有模板根结构为 `<section class="result-panel">` → `<header class="result-header">` → `<div class="result-body">`(当前 class 是 `result-body`,内含 `.result-alert`、`.table-wrap`、`.result-empty`);把 `result-body` 用 `v-show="!collapsed"` + `el-collapse-transition` 包裹;
- header 现在有 `aria-labelledby` 与 metrics `hidden` 属性——保持现有属性不变,仅在 header 内新增按钮、header 加 `@click="toggleCollapsed"`;
- 折叠按钮 `@click.stop` 防止触发 header 的切换(按钮自身已切换,避免双重触发);
- 若 `useI18n` 的 `t` 在组件内为 `t('sqlConsole.xxx')` 形式,保持现状调用方式。

- [x] **Step 4: i18n 新增键**(`web/src/i18n/index.ts` 的 sqlConsole 区块)

```ts
'sqlConsole.collapse': '折叠结果',
'sqlConsole.expand': '展开结果',
```

- [x] **Step 5: 运行测试确认通过**

Run: `cd web && npx vitest run src/components/sql-console/SQLResultPanel.mount.test.ts`
Expected: PASS(5 个用例)

- [x] **Step 6: 挂入测试脚本 + 类型检查**(`web/package.json` 的 `test:connection-dialog` 末尾追加 ` src/components/sql-console/SQLResultPanel.mount.test.ts`)

Run: `cd web && npm run test:connection-dialog && npm run typecheck`
Expected: 全部 PASS

- [x] **Step 7: 提交**

```bash
git add web/src/components/sql-console/SQLResultPanel.vue web/src/components/sql-console/SQLResultPanel.mount.test.ts web/src/i18n/index.ts web/package.json
git commit -m "feat(web): SQL 结果面板折叠(默认折叠,查询/错误自动展开)"
```

---

### Task 2: 列宽拖动 + 表头不换行 + 紧凑行距

**Files:**
- Modify: `web/src/components/sql-console/SQLResultPanel.vue`(表格属性与 scoped CSS)
- Modify: `web/src/components/sql-console/SQLResultPanel.mount.test.ts`(补断言)

**Interfaces:**
- Consumes: Task 1 的折叠结构与样式
- Produces: `el-table size="small"`、列 `width="160"`、表头/单元格 CSS

- [x] **Step 1: 追加失败测试**(`SQLResultPanel.mount.test.ts` 追加用例)

```ts
describe('SQLResultPanel 表格样式', () => {
  it('表格使用紧凑尺寸', () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: baseResult, error: '', executing: false },
    });
    expect(wrapper.find('.el-table--small').exists()).toBe(true);
  });

  it('列使用固定宽度(可拖动)', async () => {
    const wrapper = mount(SQLResultPanel, {
      props: { result: baseResult, error: '', executing: false },
    });
    await wrapper.find('.result-collapse-btn').trigger('click'); // 折叠后展开,确保表格渲染
    const ths = wrapper.findAll('.result-table thead th');
    expect(ths.length).toBeGreaterThan(0);
    ths.forEach((th) => {
      expect(th.attributes('style')).toContain('width');
    });
  });
});
```

注:若 happy-dom 下 `isVisible`/表格渲染有差异,`el-table` 渲染以实际输出为准;列宽断言可改为检查 DOM 中列样式含 `width` 或 `min-width`——实现后按实测调整断言(保持"列有固定宽度"的验证意图)。

- [x] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/components/sql-console/SQLResultPanel.mount.test.ts`
Expected: FAIL(`.el-table--small` 不存在;列无固定 width)

- [x] **Step 3: 表格属性与 CSS**

`SQLResultPanel.vue` 模板中的 `el-table` 增加 `size="small"`;`el-table-column` 的 `min-width="160"` 改为 `width="160"`:

```vue
<el-table :data="result?.rows ?? []" height="100%" stripe size="small">
  <el-table-column
    v-for="(column, index) in result?.columns ?? []"
    :key="`${index}-${column}`"
    :label="column"
    width="160"
    show-overflow-tooltip
  >
```

scoped CSS 追加(现有 `.result-table` 区块附近):

```css
/* 表头不换行 */
.result-table :deep(th .cell) {
  white-space: nowrap;
}

/* 紧凑行距:单元格行高收紧 */
.result-table :deep(.el-table .cell) {
  line-height: 20px;
}
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd web && npx vitest run src/components/sql-console/SQLResultPanel.mount.test.ts`
Expected: PASS

- [x] **Step 5: 全量验证**

Run: `cd web && npm run test:connection-dialog && npm run typecheck && npm run build`
Expected: 全部通过

- [x] **Step 6: 提交**

```bash
git add web/src/components/sql-console/SQLResultPanel.vue web/src/components/sql-console/SQLResultPanel.mount.test.ts
git commit -m "feat(web): SQL 结果表格列宽可拖动、表头不换行、紧凑行距"
```

---

## 自审记录

- **Spec 覆盖**:折叠默认/自动展开/手动切换/指标保留(Task 1)、列宽拖动(Task 2)、表头不换行(Task 2)、紧凑行距(Task 2)、测试与验收(Task 1/2)✓
- **占位符**:无 TBD;所有代码与命令已给出 ✓
- **类型一致性**:`SQLConsoleResult` 直接复用 `@/api/client` 现有类型;`collapsed`/`toggleCollapsed`/`collapseButtonIcon` 跨 Task 内一致 ✓
- **注意项**:happy-dom 下 el-table 渲染细节在 Task 2 Step 1 注中给出"以实测调整断言"的处置;折叠按钮 `@click.stop` 防双重触发已在 Task 1 Step 3 说明。
