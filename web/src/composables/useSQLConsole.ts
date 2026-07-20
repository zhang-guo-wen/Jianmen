import {
  computed,
  readonly,
  ref,
  shallowReadonly,
  shallowRef,
  toValue,
  watch,
  type MaybeRefOrGetter,
} from 'vue';

import {
  ApiError,
  apiClient,
  type DBAccountRecord,
  type SQLConsoleResult,
} from '@/api/client';

export interface UseSQLConsoleOptions {
  requestedAccountId?: MaybeRefOrGetter<string>;
}

export function useSQLConsole(options: UseSQLConsoleOptions = {}) {
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
  const requestedAccountId = computed(() => (
    options.requestedAccountId ? String(toValue(options.requestedAccountId) ?? '').trim() : ''
  ));

  function applyRequestedAccount(): void {
    const requested = requestedAccountId.value;
    if (!requested) {
      if (!accounts.value.some(account => account.id === accountId.value)) {
        accountId.value = String(accounts.value[0]?.id ?? '');
      }
      return;
    }
    if (accounts.value.some(account => account.id === requested)) {
      accountId.value = requested;
      error.value = '';
      return;
    }
    accountId.value = '';
    error.value = '指定的数据库账号不可用或无连接权限';
  }

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
      applyRequestedAccount();
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

  watch(requestedAccountId, () => {
    if (accounts.value.length > 0) applyRequestedAccount();
  });

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
