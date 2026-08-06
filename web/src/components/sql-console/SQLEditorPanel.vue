<script setup lang="ts">
import { EditorView, basicSetup } from 'codemirror';
import { MySQL, PostgreSQL, sql as sqlLanguage } from '@codemirror/lang-sql';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { Compartment, type Extension } from '@codemirror/state';
import { keymap } from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { Close, VideoPlay } from '@element-plus/icons-vue';
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { useI18n } from '@/i18n';
import type { SQLTableMetadata } from '@/utils/sqlSchema';
import { sqlSchemaFromMetadata } from '@/utils/sqlSchema';
import { parseSQLStatements, statementAtPosition } from '@/utils/sqlStatements';

import { executionGutter, setExecutingStatement } from './codemirror/executionGutter';
import { statementDecorator } from './codemirror/statementDecorator';

const props = withDefaults(defineProps<{
  executing: boolean;
  disabled: boolean;
  metadata?: readonly SQLTableMetadata[];
  dialect?: 'mysql' | 'postgres';
}>(), {
  metadata: () => [],
  dialect: 'mysql',
});

const emit = defineEmits<{
  execute: [statementText: string];
  cancel: [];
}>();

const sql = defineModel<string>({ required: true });
const { t } = useI18n();

let view: EditorView | null = null;
const container = ref<HTMLElement | null>(null);

/* 主题(浅/深):跟随 documentElement[data-theme],复用全局 CSS 变量 */
const themeCompartment = new Compartment();
const themeExtensions = (dark: boolean): Extension => [  EditorView.theme(
    {
      '&': { fontSize: '13px', backgroundColor: 'var(--color-surface)', color: 'var(--color-text)' },
      '.cm-gutters': { backgroundColor: 'var(--color-surface-muted)', color: 'var(--color-text-secondary)', borderRight: '1px solid var(--color-border)' },
      '.cm-activeLine': { backgroundColor: 'var(--color-primary-bg)' },
      '.cm-activeLineGutter': { backgroundColor: 'var(--color-surface-muted)', color: 'var(--color-primary)' },
      '.cm-cursor': { borderLeftColor: 'var(--color-primary)' },
      '.cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--color-primary) 20%, transparent)' },
      '&.cm-focused .cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--color-primary) 30%, transparent)' },
      '.cm-tooltip': { backgroundColor: 'var(--color-card)', border: '1px solid var(--color-border)', color: 'var(--color-text)' },
      '.cm-tooltip-autocomplete ul li[aria-selected]': { backgroundColor: 'var(--color-primary)', color: '#fff' },
      '.cm-tooltip-autocomplete ul li': { color: 'var(--color-text)' },
      '.cm-panels': { backgroundColor: 'var(--color-surface-muted)', color: 'var(--color-text)' },
    },
    { dark },
  ),
  syntaxHighlighting(
    HighlightStyle.define([
      { tag: tags.keyword, color: dark ? '#c586c0' : '#cf222e' },
      { tag: tags.string, color: dark ? '#ce9178' : '#0a3069' },
      { tag: tags.number, color: dark ? '#b5cea8' : '#0550ae' },
      { tag: tags.comment, color: dark ? '#6a9955' : '#6e7781', fontStyle: 'italic' },
      { tag: tags.typeName, color: dark ? '#4ec9b0' : '#0550ae' },
      { tag: tags.function(tags.variableName), color: dark ? '#dcdcaa' : '#8250df' },
      { tag: tags.operator, color: dark ? '#d4d4d4' : '#24292f' },
      { tag: tags.bool, color: dark ? '#569cd6' : '#cf222e' },
      { tag: tags.null, color: dark ? '#569cd6' : '#cf222e' },
    ]),
  ),
];

/* 可编辑性控制:执行期间禁用编辑(与基线 textarea :disabled="executing" 对齐),
   避免写确认弹窗期间用户编辑文本与 composable 的 sql.value 不一致,重发语句与弹窗展示相悖 */
const editableCompartment = new Compartment();

/* 补全 schema 动态切换 */
const schemaCompartment = new Compartment();
const schemaExtension = (metadata: readonly SQLTableMetadata[], dialect: 'mysql' | 'postgres'): Extension =>
  sqlLanguage({
    dialect: dialect === 'postgres' ? PostgreSQL : MySQL,
    schema: sqlSchemaFromMetadata(metadata),
  });

/* 程序性回写抑制:父级 handleExecute 会把 sql 临时写为单条语句(composable 需读 sql.value),
   须抑制该回写,避免整个编辑器缓冲被替换成被执行的单条语句。 */
let suppressProgrammaticSync = false;
let lastEmittedStatement = '';

/* 最近一次执行语句的 from 偏移(执行状态 → gutter loading 定位)。
   与 emitStatement 同步记录,避免点击非光标所在语句的 ▶ 时,
   loading 错误地显示在光标所在的语句上;执行结束(executing=false)时清除。 */
let lastExecutedFrom: number | null = null;

/* 记录并发出执行事件(父级收到后同步写回 sql,watch 依据标志识别并忽略该回写) */
function emitStatement(text: string, from: number | null): void {
  lastEmittedStatement = text;
  lastExecutedFrom = from;
  suppressProgrammaticSync = true;
  emit('execute', text);
}

/* 执行当前语句(Ctrl+Enter / 工具栏执行按钮) */
function executeCurrentStatement(): void {
  if (!view) return;
  const statements = parseSQLStatements(view.state.doc.toString());
  const current = statementAtPosition(statements, view.state.selection.main.head);
  if (!current) return;
  emitStatement(current.text, current.from);
}

/* 执行指定语句(gutter 点击) */
function executeStatement(range: { from: number }): void {
  if (!view) return;
  const statements = parseSQLStatements(view.state.doc.toString());
  const target = statements.find((s) => s.from === range.from);
  if (target) emitStatement(target.text, target.from);
}

function isDarkTheme(): boolean {
  return document.documentElement.dataset.theme === 'dark';
}

/* 光标当前所在语句的 from 偏移(执行状态 → gutter loading 定位) */
function executingFrom(): number | null {
  if (!view) return null;
  const statements = parseSQLStatements(view.state.doc.toString());
  const current = statementAtPosition(statements, view.state.selection.main.head);
  return current?.from ?? null;
}

const stopHandles: Array<() => void> = [];

onMounted(() => {
  if (!container.value) return;
  const initialDark = isDarkTheme();
  view = new EditorView({
    parent: container.value,
    doc: sql.value,
    extensions: [
      basicSetup,
      keymap.of([
        { key: 'Ctrl-Enter', run: () => { executeCurrentStatement(); return true; } },
        { key: 'Mod-Enter', run: () => { executeCurrentStatement(); return true; } },
      ]),
      themeCompartment.of(themeExtensions(initialDark)),
      editableCompartment.of(EditorView.editable.of(!props.executing)),
      schemaCompartment.of(schemaExtension(props.metadata, props.dialect)),
      statementDecorator(),
      executionGutter({ onExecute: executeStatement }),
      /* 编辑器内修改 → 回写 v-model(与外部 watch 比较,防双向循环) */
      EditorView.updateListener.of((update) => {
        if (update.docChanged && update.state.doc.toString() !== sql.value) {
          sql.value = update.state.doc.toString();
        }
      }),
    ],
  });
  /* 外部修改 sql 时同步进编辑器(忽略执行语句引起的程序性回写,保留多语句全文) */
  stopHandles.push(watch(sql, (value) => {
    if (!view) return;
    if (suppressProgrammaticSync && value.trim() === lastEmittedStatement.trim()) {
      suppressProgrammaticSync = false;
      return;
    }
    suppressProgrammaticSync = false;
    const current = view.state.doc.toString();
    if (value !== current) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
    }
  }));
  /* 主题切换(浅/深)热更新 */
  const observer = new MutationObserver(() => {
    if (!view) return;
    view.dispatch({ effects: themeCompartment.reconfigure(themeExtensions(isDarkTheme())) });
  });
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  stopHandles.push(() => observer.disconnect());
  /* 执行状态 → 禁用编辑 + gutter loading。
     loading 优先定位最近执行的语句(lastExecutedFrom),回退到光标所在语句;
     执行结束清除 lastExecutedFrom,避免下次执行误用旧位置。 */
  stopHandles.push(watch(() => props.executing, (executing) => {
    view?.dispatch({
      effects: [
        editableCompartment.reconfigure(EditorView.editable.of(!executing)),
        setExecutingStatement.of(executing ? (lastExecutedFrom ?? executingFrom()) : null),
      ],
    });
    if (!executing) lastExecutedFrom = null;
  }));
  /* 元数据/方言更新 → 补全重建 */
  stopHandles.push(watch(() => [props.metadata, props.dialect], () => {
    view?.dispatch({ effects: schemaCompartment.reconfigure(schemaExtension(props.metadata, props.dialect)) });
  }));
});

onBeforeUnmount(() => {
  for (const stop of stopHandles.splice(0)) stop();
  view?.destroy();
  view = null;
});

defineExpose({ getView: () => view, executeCurrentStatement });
</script>

<template>
  <section class="editor-panel" aria-labelledby="sql-console-editor-title">
    <header class="editor-header">
      <div>
        <strong id="sql-console-editor-title">{{ t('sqlConsole.editorLabel') }}</strong>
        <span>{{ t('sqlConsole.keyboardHint') }}</span>
      </div>
      <div class="editor-actions">
        <el-button v-if="executing" :icon="Close" @click="emit('cancel')">
          {{ t('sqlConsole.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :icon="VideoPlay"
          :loading="executing"
          :disabled="disabled || executing"
          @click="executeCurrentStatement"
        >
          {{ t('sqlConsole.execute') }}
        </el-button>
      </div>
    </header>
    <div ref="container" class="sql-editor-container" />
  </section>
</template>

<style scoped>
.editor-panel {
  display: flex;
  flex: 0 0 34%;
  min-height: 220px;
  flex-direction: column;
  overflow: hidden;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: 0 0 auto;
  gap: 16px;
  padding: 12px 14px 12px 18px;
  color: var(--color-text);
  background: var(--color-surface-muted);
  border-bottom: 1px solid var(--color-border);
}

.editor-header strong,
.editor-header span {
  display: block;
}

.editor-header strong {
  font-size: 13px;
}

.editor-header span {
  margin-top: 2px;
  color: var(--color-text-secondary);
  font-family: var(--font-mono);
  font-size: 11px;
}

.editor-actions {
  display: flex;
  gap: 8px;
}

.editor-actions :deep(.el-button) {
  margin: 0;
}

.sql-editor-container {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.sql-editor-container :deep(.cm-editor) {
  height: 100%;
}

.sql-editor-container :deep(.cm-scroller) {
  font-family: var(--font-mono);
  line-height: 1.7;
}
</style>
