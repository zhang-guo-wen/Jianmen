<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { apiClient, type AuditBatchLogRecord, type AuditBatchScope } from '@/api/client';
import DataTableCard from '@/components/DataTableCard.vue';
import AuditResultTag from '@/components/audit/AuditResultTag.vue';
import { TABLE_COLUMNS } from '@/config/tableColumns';
import { useAuditPageRequest } from '@/composables/useAuditBatches';
import { useI18n } from '@/i18n';
import { auditFailureDetail, formatAuditTimestamp, queryResultPresentation } from '@/utils/auditDisplay';

const props = defineProps<{ modelValue: boolean; scope: AuditBatchScope; batchId: string }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; 'query-detail': [row: AuditBatchLogRecord] }>();
const { t } = useI18n();
const { rows, total, loading, error, load, cancel } = useAuditPageRequest<AuditBatchLogRecord>();
const page = ref(1);
const pageSize = ref(50);
const input = ref('');
const keyword = ref('');
const isDatabase = computed(() => props.scope === 'db');
const title = computed(() => `${t('audit.batch.column')} ${props.batchId} · ${t('audit.action.log')}`);

function request() {
  if (!props.modelValue || !props.batchId) return;
  const { scope, batchId } = props;
  const params = { page: page.value, page_size: pageSize.value, q: keyword.value || undefined };
  void load(signal => apiClient.getAuditBatchLogs(scope, batchId, params, signal), t('audit.error.loadArtifact'));
}
function search(text: string) {
  keyword.value = text.trim();
  page.value = 1;
  request();
}
function changePage(value: number) {
  if (page.value === value) return;
  page.value = value;
  request();
}
function changeSize(value: number) {
  if (pageSize.value === value) return;
  pageSize.value = value;
  page.value = 1;
  request();
}
function logResult(row: AuditBatchLogRecord) {
  const result = queryResultPresentation(row.status);
  return { label: t(`audit.result.${result.code}`), tag: result.tag };
}
function rowKey(row: AuditBatchLogRecord) { return `${row.kind}:${row.id}`; }

watch(() => [props.modelValue, props.scope, props.batchId], () => {
  cancel(true);
  page.value = 1;
  input.value = '';
  keyword.value = '';
  request();
}, { immediate: true });
onBeforeUnmount(() => cancel());
</script>

<template>
  <el-drawer :model-value="modelValue" :title="title" direction="rtl" size="65%" @update:model-value="emit('update:modelValue', $event)">
    <div class="batch-drawer-content">
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <DataTableCard
        :data="rows" :total="total" :loading="loading" :page="page" :page-size="pageSize" :row-key="rowKey"
        v-model:search="input"
        :search-placeholder="t(isDatabase ? 'audit.batch.searchDatabaseLog' : 'audit.batch.searchSSHLog')"
        :empty-text="error ? ' ' : t(keyword ? 'audit.batch.noMatch' : 'audit.batch.emptyLog')"
        @search="search" @update:page="changePage" @update:page-size="changeSize"
      >
        <el-table-column v-bind="TABLE_COLUMNS.time" :label="t('audit.column.time')">
          <template #default="{ row }">{{ formatAuditTimestamp(row.timestamp) }}</template>
        </el-table-column>
        <el-table-column v-bind="TABLE_COLUMNS.identifier" :label="t('audit.batch.source')">
          <template #default="{ row }"><span class="source-id" :title="row.audit_session_id">{{ row.audit_session_id.slice(0, 12) }}</span></template>
        </el-table-column>
        <el-table-column :label="t(isDatabase ? 'audit.column.sql' : 'audit.batch.content')" min-width="280">
          <template #default="{ row }">
            <el-tag v-if="row.kind === 'file'" size="small" effect="plain">{{ row.action || t('audit.batch.file') }}</el-tag>
            <code class="audit-log-text" :title="row.text">{{ row.text }}</code>
          </template>
        </el-table-column>
        <el-table-column v-if="isDatabase" v-bind="TABLE_COLUMNS.number" :label="t('audit.column.rowCount')">
          <template #default="{ row }">{{ row.rows_affected ?? row.rows ?? '-' }}</template>
        </el-table-column>
        <el-table-column v-if="isDatabase" v-bind="TABLE_COLUMNS.number" :label="t('audit.column.duration')">
          <template #default="{ row }">{{ (row.duration_ms ?? 0) + 'ms' }}</template>
        </el-table-column>
        <el-table-column v-bind="TABLE_COLUMNS.status" :label="t('audit.column.result')">
          <template #default="{ row }">
            <AuditResultTag v-if="row.status" :label="logResult(row).label" :type="logResult(row).tag" :detail="auditFailureDetail(row.error_code, row.error_message)" />
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column v-if="isDatabase" v-bind="TABLE_COLUMNS.actionsCompact" :label="t('common.actions')">
          <template #default="{ row }">
            <el-button link type="primary" :disabled="!row.has_full_log" @click="emit('query-detail', row)">{{ t('audit.action.viewDetail') }}</el-button>
          </template>
        </el-table-column>
      </DataTableCard>
    </div>
  </el-drawer>
</template>

<style scoped>
.batch-drawer-content { display: flex; flex-direction: column; flex: 1; min-width: 0; height: 100%; min-height: 0; gap: 8px; }
.source-id, .audit-log-text { font-family: var(--font-mono); }
.audit-log-text { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
