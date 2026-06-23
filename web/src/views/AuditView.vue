<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-segmented v-model="eventType" :options="eventTypes" />
      <el-button type="primary">{{ t('common.export') }}</el-button>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <el-table :data="auditRows" height="300">
        <el-table-column :label="t('audit.column.time')" width="180">
          <template #default="{ row }">
            {{ t(row.timeKey) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('audit.column.actor')" width="180">
          <template #default="{ row }">
            {{ t(row.actorKey) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('audit.column.event')">
          <template #default="{ row }">
            {{ t(row.eventKey) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('audit.column.result')" width="140">
          <template #default="{ row }">
            <el-tag :type="row.result === 'allowed' ? 'success' : 'warning'">
              {{ t(row.resultKey) }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import { useI18n, type TranslationKey } from '@/i18n';

type EventType = 'all' | 'login' | 'session' | 'policy';
type AuditResult = 'allowed' | 'review';
type AuditRow = {
  timeKey: TranslationKey;
  actorKey: TranslationKey;
  eventKey: TranslationKey;
  result: AuditResult;
  resultKey: TranslationKey;
};

const { t } = useI18n();
const eventType = ref<EventType>('all');
const eventTypes = computed(() => [
  { label: t('audit.filter.all'), value: 'all' },
  { label: t('audit.filter.login'), value: 'login' },
  { label: t('audit.filter.session'), value: 'session' },
  { label: t('audit.filter.policy'), value: 'policy' }
]);
const auditRows: AuditRow[] = [
  {
    timeKey: 'audit.pendingApi',
    actorKey: 'audit.actor.system',
    eventKey: 'audit.event.auditPlaceholder',
    result: 'allowed',
    resultKey: 'audit.result.allowed'
  },
  {
    timeKey: 'audit.pendingApi',
    actorKey: 'audit.actor.operator',
    eventKey: 'audit.event.policyChangePlaceholder',
    result: 'review',
    resultKey: 'audit.result.review'
  }
];
</script>
