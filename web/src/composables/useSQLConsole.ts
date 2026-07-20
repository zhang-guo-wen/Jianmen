import { computed, readonly, ref, shallowReadonly, shallowRef } from 'vue';

import {
  ApiError,
  apiClient,
  type DBAccountRecord,
  type SQLConsoleResult,
} from '@/api/client';

export function useSQLConsole() {
  const accounts = ref<DBAccountRecord[]>([]);
  const accountId = shallowRef('');
  const database = shallowRef('');
  const sql = shallowRef('SELECT 1 AS ready;');
  const loadingAccounts = shallowRef(false);
  const executing = shallowRef(false);
  const error = shallowRef('');
  const result = shallowRef<SQLConsoleResult | null>(null);
  const activeController = shallowRef<AbortController | null>(null);

  const selectedAccount = computed(
    () => accounts.value.find(account => account.id === accountId.value) ?? null,
  );

  async function loadAccounts() {
    loadingAccounts.value = true;
    error.value = '';
    try {
      const response = await apiClient.getAllDBAccounts({
        page: 1,
        page_size: 200,
        connectable: true,
      });
      accounts.value = (response.items ?? []).filter((account) => {
        const protocol = account.instance_protocol?.toLowerCase();
        return protocol === 'mysql' || protocol === 'postgres' || protocol === 'postgresql';
      });
      if (!accounts.value.some(account => account.id === accountId.value)) {
        accountId.value = String(accounts.value[0]?.id ?? '');
      }
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '加载数据库账号失败';
    } finally {
      loadingAccounts.value = false;
    }
  }

  async function execute(confirmWrite = false) {
    error.value = '';
    const controller = new AbortController();
    activeController.value = controller;
    executing.value = true;
    try {
      const response = await apiClient.executeSQL({
        account_id: accountId.value,
        database: database.value.trim(),
        sql: sql.value,
        confirm_write: confirmWrite,
      }, controller.signal);
      result.value = response;
      return response;
    } catch (cause) {
      if (
        !controller.signal.aborted &&
        (!(cause instanceof ApiError) || cause.code !== 'PRECONDITION_FAILED')
      ) {
        error.value = cause instanceof Error ? cause.message : 'SQL 执行失败';
      }
      throw cause;
    } finally {
      if (activeController.value === controller) {
        activeController.value = null;
        executing.value = false;
      }
    }
  }

  function cancel() {
    activeController.value?.abort();
  }

  return {
    accounts: readonly(accounts),
    accountId,
    database,
    sql,
    loadingAccounts: readonly(loadingAccounts),
    executing: readonly(executing),
    error: readonly(error),
    result: shallowReadonly(result),
    selectedAccount,
    loadAccounts,
    execute,
    cancel,
  };
}
