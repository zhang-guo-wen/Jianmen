<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus';
import { computed, onMounted, ref } from 'vue';
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

/** 结果面板折叠状态:折叠时让位编辑器,吸收全部剩余空间 */
const resultCollapsed = ref(true);

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
      class="sql-editor-panel"
      :class="{ 'sql-editor-panel--expanded': resultCollapsed }"
      :executing="executing"
      :disabled="executionDisabled"
      :metadata="metadata"
      :dialect="editorDialect"
      @execute="handleExecute"
      @cancel="cancel"
    />

    <SQLResultPanel
      v-model:collapsed="resultCollapsed"
      class="sql-result-panel"
      :class="{ 'sql-result-panel--collapsed': resultCollapsed }"
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

/* 折叠让位布局:默认展开态编辑器占 34%,结果面板吸收剩余;
   折叠时结果面板收缩为 header 高度,编辑器吸收全部空间。
   双类写法提高特异性,确保覆盖子组件内部 .editor-panel/.result-panel 的 flex 规则
   (同特异性下样式加载顺序不可控,避免依赖后加载胜出)。 */
.sql-editor-panel {
  flex: 0 0 34%;
}

.sql-editor-panel.sql-editor-panel--expanded {
  flex: 1 1 0;
}

.sql-result-panel {
  flex: 1;
  min-height: 190px;
}

.sql-result-panel.sql-result-panel--collapsed {
  flex: 0 0 auto;
  min-height: 0;
}
</style>
