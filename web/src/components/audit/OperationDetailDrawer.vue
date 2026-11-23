<script setup lang="ts">
import { computed } from 'vue';
import type { OperationAuditRecord } from '@/api/client';
import AuditResultTag from '@/components/audit/AuditResultTag.vue';
import { useI18n } from '@/i18n';
import { formatAuditTimestamp, operationResultPresentation, parseOperationAuditMetadata } from '@/utils/auditDisplay';
import { operationLabel, operationTargetLabel } from '@/utils/operationAudit';

const props = defineProps<{ modelValue: boolean; record: OperationAuditRecord | null }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();
const { t } = useI18n();

const metadata = computed(() => (
  props.record ? parseOperationAuditMetadata(props.record) : null
));
const result = computed(() => (
  props.record ? operationResultPresentation(props.record) : { code: 'unknown' as const, tag: 'info' as const }
));
const title = computed(() => (
  props.record ? operationLabel(props.record, t) : t('audit.title.detail')
));
const summary = computed(() => {
  if (!props.record) return '';
  switch (result.value.code) {
    case 'success':
      return t('audit.detail.summarySuccess');
    case 'failure':
      return metadata.value?.errorMessage
        ? t('audit.detail.summaryFailureReason')
        : t('audit.detail.summaryFailure');
    case 'pending':
      return t('audit.detail.summaryPending');
    default:
      return t('audit.detail.summaryUnknown');
  }
});
/** 容易被误读的操作补一句说明：签发凭据 / 建立会话都不等于已经登录。 */
const clarification = computed(() => {
  const operation = String(props.record?.operation ?? '').trim();
  if (operation === 'issue_credential') return t('audit.detail.credentialHint');
  if (operation === 'issue_session') return t('audit.detail.sessionHint');
  return '';
});
const basicRows = computed(() => {
  const record = props.record;
  if (!record) return [] as Array<[string, string]>;
  const rows: Array<[string, string]> = [
    [t('audit.column.operationTime'), formatAuditTimestamp(record.created_at, t('common.none'))],
    [t('audit.column.operator'), record.actor_display_name || record.actor_username || t('common.none')],
    [t('audit.column.actorChannel'), channelText(record.actor_channel)],
    [t('audit.column.operationTarget'), operationTargetLabel(record, t)],
    [t('audit.column.sourceIp'), record.client_ip || t('common.none')],
    [t('audit.column.result'), t(`audit.result.${result.value.code}`)],
  ];
  const reason = metadata.value?.errorMessage || '';
  if (reason) rows.push([t('audit.column.failureReason'), reason]);
  return rows;
});
const technicalRows = computed(() => {
  const record = props.record;
  const detail = metadata.value;
  if (!record || !detail) return [] as Array<[string, string]>;
  const rows: Array<[string, string]> = [
    [t('audit.column.requestId'), detail.requestId || record.request_id || t('common.none')],
    [t('audit.column.apiRequest'), [detail.method, detail.path].filter(Boolean).join(' ') || record.resource_name || t('common.none')],
    [t('audit.column.httpStatus'), String(detail.statusCode ?? record.status_code ?? t('common.none'))],
    [t('audit.column.resourceType'), record.resource_type || t('common.none')],
    [t('audit.column.resourceId'), record.resource_id || t('common.none')],
  ];
  return rows;
});

function channelText(channel?: string): string {
  switch (String(channel ?? '').trim()) {
    case 'ai_api':
      return t('audit.channel.aiApi');
    case 'admin_console':
      return t('audit.channel.adminConsole');
    default:
      return t('audit.channel.unknown');
  }
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    direction="rtl"
    size="520px"
    :title="t('audit.title.operationDetail')"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-if="record" class="operation-detail">
      <div class="operation-detail__heading">
        <h3>{{ title }}</h3>
        <AuditResultTag
          :label="t(`audit.result.${result.code}`)"
          :type="result.tag"
          :detail="metadata?.errorMessage || undefined"
        />
      </div>
      <el-alert
        v-if="!metadata?.errorMessage"
        :title="summary"
        type="info"
        :closable="false"
        show-icon
      />
      <el-alert
        v-if="clarification"
        :title="clarification"
        type="warning"
        :closable="false"
        show-icon
      />
      <el-descriptions :title="t('audit.column.eventInfo')" :column="1" size="small" border label-width="92">
        <el-descriptions-item v-for="[label, value] in basicRows" :key="label" :label="label">
          {{ value }}
        </el-descriptions-item>
      </el-descriptions>
      <el-descriptions :title="t('audit.column.technicalInfo')" :column="1" size="small" border label-width="92">
        <el-descriptions-item v-for="[label, value] in technicalRows" :key="label" :label="label">
          <code>{{ value }}</code>
        </el-descriptions-item>
      </el-descriptions>
    </div>
  </el-drawer>
</template>

<style scoped>
.operation-detail {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.operation-detail__heading {
  display: flex;
  align-items: center;
  gap: 10px;
}

.operation-detail__heading h3 {
  margin: 0;
  font-size: 16px;
}

.operation-detail code {
  font-family: var(--font-mono);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.operation-detail :deep(.el-descriptions__content) {
  overflow-wrap: anywhere;
}
</style>

