<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus';
import { computed, onMounted } from 'vue';

import { ApiError } from '@/api/client';
import { useSQLConsole } from '@/composables/useSQLConsole';
import { useI18n } from '@/i18n';

import SQLEditorPanel from './SQLEditorPanel.vue';
import SQLConsoleToolbar from './SQLConsoleToolbar.vue';
import SQLResultPanel from './SQLResultPanel.vue';

const {
  accounts,
  accountId,
  database,
  sql,
  loadingAccounts,
  executing,
  error,
  result,
  loadAccounts,
  execute,
  cancel,
} = useSQLConsole();
const { t } = useI18n();

const executionDisabled = computed(
  () => executing.value || !accountId.value || !sql.value.trim(),
);

onMounted(() => {
  void loadAccounts();
});

async function handleExecute() {
  if (!accountId.value) {
    ElMessage.warning(t('sqlConsole.error.missingAccount'));
    return;
  }
  if (!sql.value.trim()) {
    ElMessage.warning(t('sqlConsole.error.missingSQL'));
    return;
  }
  try {
    await execute(false);
    ElMessage.success(t('sqlConsole.executionSucceeded'));
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === 'PRECONDITION_FAILED') {
      await confirmAndExecuteWrite();
      return;
    }
    if (cause instanceof DOMException && cause.name === 'AbortError') {
      ElMessage.info(t('sqlConsole.executionCancelled'));
      return;
    }
    ElMessage.error(cause instanceof Error ? cause.message : t('sqlConsole.executionFailed'));
  }
}

async function confirmAndExecuteWrite() {
  try {
    await ElMessageBox.confirm(
      t('sqlConsole.writeConfirmMessage'),
      t('sqlConsole.writeConfirmTitle'),
      {
        confirmButtonText: t('sqlConsole.execute'),
        cancelButtonText: t('common.cancel'),
        type: 'warning',
      },
    );
    await execute(true);
    ElMessage.success(t('sqlConsole.executionSucceeded'));
  } catch (cause) {
    if (cause === 'cancel' || cause === 'close') return;
    if (cause instanceof DOMException && cause.name === 'AbortError') {
      ElMessage.info(t('sqlConsole.executionCancelled'));
      return;
    }
    ElMessage.error(cause instanceof Error ? cause.message : t('sqlConsole.executionFailed'));
  }
}
</script>

<template>
  <div class="sql-console-workspace">
    <SQLConsoleToolbar
      v-model:account-id="accountId"
      v-model:database="database"
      :accounts="accounts"
      :loading="loadingAccounts"
      :executing="executing"
      @refresh="loadAccounts"
    />

    <el-alert
      v-if="!loadingAccounts && accounts.length === 0"
      type="info"
      :title="t('sqlConsole.emptyAccounts')"
      :closable="false"
      show-icon
    />

    <SQLEditorPanel
      v-model="sql"
      :executing="executing"
      :disabled="executionDisabled"
      @execute="handleExecute"
      @cancel="cancel"
    />

    <SQLResultPanel
      :result="result"
      :error="error"
      :executing="executing"
    />
  </div>
</template>

<style scoped>
.sql-console-workspace {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
  gap: 12px;
}
</style>
