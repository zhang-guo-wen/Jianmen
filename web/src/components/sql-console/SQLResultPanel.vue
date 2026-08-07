<script setup lang="ts">
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue';
import { ElCollapseTransition } from 'element-plus';
import { computed, ref, watch } from 'vue';

import type { SQLConsoleResult } from '@/api/client';
import { useI18n } from '@/i18n';

const props = defineProps<{
  result: SQLConsoleResult | null;
  error: string;
  executing: boolean;
}>();

const { t } = useI18n();
const hasRows = computed(() => Boolean(props.result?.columns.length));
const emptyDescription = computed(() => {
  if (props.executing) return `${t('sqlConsole.execute')}...`;
  if (props.result) return t('sqlConsole.executionSucceeded');
  return t('sqlConsole.noResult');
});

/* 折叠状态:默认折叠,查询结果/错误时自动展开
   immediate:true —— 面板挂载时即携带 result/error(如会话切换后重建)也应展开 */
const collapsed = ref(true);

watch(() => props.result, (result) => {
  if (result) collapsed.value = false;
}, { immediate: true });
watch(() => props.error, (error) => {
  if (error) collapsed.value = false;
}, { immediate: true });

const collapseButtonIcon = computed(() => (collapsed.value ? ArrowUp : ArrowDown));
const collapseTitle = computed(() =>
  collapsed.value ? t('sqlConsole.expand') : t('sqlConsole.collapse'),
);

function toggleCollapsed(): void {
  collapsed.value = !collapsed.value;
}

function formatCell(value: unknown): string {
  if (value === null || value === undefined) return 'NULL';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}
</script>

<template>
  <section class="result-panel" aria-labelledby="sql-console-result-title">
    <header class="result-header" @click="toggleCollapsed">
      <div class="result-heading">
        <strong id="sql-console-result-title">{{ t('sqlConsole.resultTitle') }}</strong>
        <el-tag v-if="result" :type="result.read_only ? 'success' : 'warning'" effect="light" round>
          {{ result.read_only ? t('sqlConsole.readOnly') : t('sqlConsole.write') }}
        </el-tag>
      </div>
      <div v-if="result" class="result-metrics">
        <span>{{ t('sqlConsole.result.duration') }} <b>{{ result.duration_ms }} ms</b></span>
        <span v-if="result.read_only">{{ t('sqlConsole.result.rows') }} <b>{{ result.row_count }}</b></span>
        <span v-else>{{ t('sqlConsole.result.affected') }} <b>{{ result.rows_affected }}</b></span>
        <span class="audit-id">{{ t('sqlConsole.result.audit') }} <b>{{ result.audit_session_id }}</b></span>
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
        <el-alert
          v-if="error"
          class="result-alert"
          type="error"
          :title="error"
          :closable="false"
          show-icon
        />
        <el-alert
          v-else-if="result?.truncated"
          class="result-alert"
          type="warning"
          :title="t('sqlConsole.result.truncated')"
          :closable="false"
          show-icon
        />

        <div v-if="hasRows" class="result-table">
          <el-table :data="result?.rows ?? []" height="100%" stripe size="small">
            <el-table-column
              v-for="(column, index) in result?.columns ?? []"
              :key="`${index}-${column}`"
              :label="column"
              width="160"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                <span :class="{ 'null-value': row[index] == null }">{{ formatCell(row[index]) }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div v-else class="result-empty">
          <el-empty
            :description="emptyDescription"
            :image-size="72"
          />
        </div>
      </div>
    </el-collapse-transition>
  </section>
</template>

<style scoped>
.result-panel {
  display: flex;
  flex: 1;
  min-height: 190px;
  flex-direction: column;
  overflow: hidden;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.result-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: 0 0 auto;
  gap: 16px;
  min-height: 54px;
  padding: 10px 16px;
  background: var(--color-surface-muted);
  border-bottom: 1px solid var(--color-border);
}

.result-heading,
.result-metrics {
  display: flex;
  align-items: center;
  gap: 10px;
}

.result-heading strong {
  font-size: 14px;
}

.result-metrics {
  color: var(--color-text-secondary);
  font-family: var(--font-mono);
  font-size: 11px;
}

.result-metrics b {
  color: #334155;
  font-weight: 700;
}

.audit-id {
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.result-body {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.result-alert {
  flex: 0 0 auto;
  border-radius: 0;
}

.result-table,
.result-empty {
  flex: 1;
  min-height: 0;
}

/* 表头不换行 */
.result-table :deep(th .cell) {
  white-space: nowrap;
}

/* 紧凑行距:单元格行高收紧 */
.result-table :deep(.el-table .cell) {
  line-height: 20px;
}

.result-empty {
  display: grid;
  place-items: center;
}

.null-value {
  color: #94a3b8;
  font-family: var(--font-mono);
  font-style: italic;
}

@media (max-width: 900px) {
  .result-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .result-metrics {
    flex-wrap: wrap;
  }
}
</style>
