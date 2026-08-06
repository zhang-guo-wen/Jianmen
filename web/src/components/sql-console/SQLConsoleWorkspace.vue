<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus';
import { computed, onMounted } from 'vue';
import { useRoute } from 'vue-router';

import { ApiError } from '@/api/client';
import { useSQLConsole } from '@/composables/useSQLConsole';
import { useI18n } from '@/i18n';

import SQLEditorPanel from './SQLEditorPanel.vue';
import SQLConsoleToolbar from './SQLConsoleToolbar.vue';
import SQLResultPanel from './SQLResultPanel.vue';

const route = useRoute();
const requestedAccountId = computed(() => {
  const value = route.query.database_account_id;
  return Array.isArray(value) ? String(value[0] ?? '') : String(value ?? '');
});

const {
  accounts,
  databases,
  accountId,
  database,
  sql,
  loadingAccounts,
  connecting,
  connected,
  executing,
  error,
  result,
  metadata,
  selectedAccount,
  loadAccounts,
  connect,
  execute,
  cancel,
} = useSQLConsole({ requestedAccountId });
const { t } = useI18n();

const executionDisabled = computed(
  () => executing.value || connecting.value || !connected.value || !database.value,
);

/** 编辑器 SQL 方言:与 useSQLConsole 的账号协议过滤保持一致,postgresql 兼容别名按 postgres 处理。 */
const editorDialect = computed<'mysql' | 'postgres'>(() => {
  const protocol = selectedAccount.value?.instance_protocol?.toLowerCase();
  return protocol === 'postgres' || protocol === 'postgresql' ? 'postgres' : 'mysql';
});

onMounted(() => {
  void loadAccounts();
});

/** 执行 SQL:语句文本来自编辑器(工具栏/快捷键/Ctrl+Enter 的光标当前语句或行号按钮的单条语句)。 */
function handleExecute(statementText?: string): void {
  if (!accountId.value) {
    ElMessage.warning(t('sqlConsole.error.missingAccount'));
    return;
  }
  if (!database.value) {
    ElMessage.warning(t('sqlConsole.error.missingDatabase'));
    return;
  }
  const sqlText = statementText?.trim() ?? sql.value.trim();
  if (!sqlText) {
    ElMessage.warning(t('sqlConsole.error.missingSQL'));
    return;
  }
  // execute(composable)仍读 sql.value,先落盘再执行,语义不变
  sql.value = sqlText;
  void executeSQL(sqlText, false);
}

async function executeSQL(sqlText: string, confirmWrite: boolean): Promise<void> {
  try {
    await execute(confirmWrite);
    ElMessage.success(t('sqlConsole.executionSucceeded'));
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === 'PRECONDITION_FAILED') {
      await confirmAndExecuteWrite(sqlText);
      return;
    }
    if (cause instanceof DOMException && cause.name === 'AbortError') {
      ElMessage.info(t('sqlConsole.executionCancelled'));
      return;
    }
    ElMessage.error(cause instanceof Error ? cause.message : t('sqlConsole.executionFailed'));
  }
}

async function confirmAndExecuteWrite(sqlText: string): Promise<void> {
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
    // 用户确认后重发被拒的那条语句(写确认);取消/关闭时静默返回
    await executeSQL(sqlText, true);
  } catch (cause) {
    if (cause === 'cancel' || cause === 'close') return;
    if (cause instanceof DOMException && cause.name === 'AbortError') return;
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
      :databases="databases"
      :loading="loadingAccounts"
      :connecting="connecting"
      :connected="connected"
      :executing="executing"
      @account-change="connect"
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
      :metadata="metadata"
      :dialect="editorDialect"
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
