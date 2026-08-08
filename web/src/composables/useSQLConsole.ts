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
import type { SQLTableMetadata } from '@/utils/sqlSchema';

export interface UseSQLConsoleOptions {
  requestedAccountId?: MaybeRefOrGetter<string>;
}

/**
 * 模块级单例状态:SQL 控制台连接在组件卸载后保留,重新进入页面时复用,
 * 不再每次新建数据库连接会话。
 */
const accounts = ref<DBAccountRecord[]>([]);
const databases = ref<string[]>([]);
const accountId = shallowRef('');
const database = shallowRef('');
const sessionId = shallowRef('');
const sql = shallowRef('SELECT 1 AS ready;');
const loadingAccounts = shallowRef(false);
const connecting = shallowRef(false);
const executing = shallowRef(false);
const error = shallowRef('');
const result = shallowRef<SQLConsoleResult | null>(null);
const metadata = shallowRef<SQLTableMetadata[]>([]);
const activeController = shallowRef<AbortController | null>(null);
const connectionController = shallowRef<AbortController | null>(null);
const metadataController = shallowRef<AbortController | null>(null);
let connectionGeneration = 0;

/** 重置模块级状态(测试与调试用):清空连接与会话状态,不调用后端 API。 */
export function resetSQLConsoleState(): void {
  connectionGeneration++;
  connectionController.value?.abort();
  activeController.value?.abort();
  metadataController.value?.abort();
  accounts.value = [];
  databases.value = [];
  accountId.value = '';
  database.value = '';
  sessionId.value = '';
  sql.value = 'SELECT 1 AS ready;';
  loadingAccounts.value = false;
  connecting.value = false;
  executing.value = false;
  error.value = '';
  result.value = null;
  metadata.value = [];
}

export function useSQLConsole(options: UseSQLConsoleOptions = {}) {

  const selectedAccount = computed(
    () => accounts.value.find(account => account.id === accountId.value) ?? null,
  );
  const connected = computed(() => Boolean(sessionId.value));
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

  async function loadAccounts(): Promise<void> {
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
      // 已有活跃连接则复用(重新进入页面不新建会话);账号变化由 watch 触发 connect
      if (!sessionId.value) {
        await connect();
      }
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '加载数据库账号失败';
    } finally {
      loadingAccounts.value = false;
    }
  }

  async function connect(): Promise<void> {
    const generation = ++connectionGeneration;
    const targetAccountId = accountId.value;
    connectionController.value?.abort();
    activeController.value?.abort();
    const previousSessionId = sessionId.value;
    sessionId.value = '';
    databases.value = [];
    database.value = '';
    result.value = null;
    metadata.value = [];
    metadataController.value?.abort();
    if (previousSessionId) {
      void apiClient.closeSQLConsoleSession(previousSessionId).catch(() => undefined);
    }
    if (!targetAccountId) {
      connecting.value = false;
      return;
    }

    const controller = new AbortController();
    connectionController.value = controller;
    connecting.value = true;
    error.value = '';
    try {
      const session = await apiClient.createSQLConsoleSession(targetAccountId, controller.signal);
      if (controller.signal.aborted || generation !== connectionGeneration || accountId.value !== targetAccountId) {
        void apiClient.closeSQLConsoleSession(session.id).catch(() => undefined);
        return;
      }
      sessionId.value = session.id;
      databases.value = session.databases ?? [];
      database.value = session.default_database || databases.value[0] || '';
      void loadMetadata();
    } catch (cause) {
      if (!controller.signal.aborted && generation === connectionGeneration) {
        error.value = cause instanceof Error ? cause.message : '连接数据库失败';
      }
    } finally {
      if (connectionController.value === controller) {
        connectionController.value = null;
        connecting.value = false;
      }
    }
  }

  async function execute(confirmWrite = false): Promise<SQLConsoleResult> {
    error.value = '';
    const currentSessionId = sessionId.value;
    if (!currentSessionId || !database.value) {
      throw new Error('数据库连接尚未就绪');
    }
    const controller = new AbortController();
    activeController.value = controller;
    executing.value = true;
    try {
      const response = await apiClient.executeSQL(currentSessionId, {
        database: database.value,
        sql: sql.value,
        confirm_write: confirmWrite,
      }, controller.signal);
      result.value = response;
      return response;
    } catch (cause) {
      if (cause instanceof ApiError && cause.statusCode === 404) {
        sessionId.value = '';
        await connect();
        const sessionError = new Error('连接已过期，已自动重新连接，请再次执行');
        error.value = sessionError.message;
        throw sessionError;
      } else if (
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

  async function loadMetadata(): Promise<void> {
    const currentSessionId = sessionId.value;
    const currentDatabase = database.value;
    metadataController.value?.abort();
    if (!currentSessionId || !currentDatabase) {
      metadata.value = [];
      return;
    }
    const controller = new AbortController();
    metadataController.value = controller;
    try {
      const response = await apiClient.getSQLConsoleMetadata(currentSessionId, currentDatabase, controller.signal);
      if (controller.signal.aborted) return;
      metadata.value = response.tables ?? [];
    } catch {
      if (!controller.signal.aborted) metadata.value = [];
    } finally {
      if (metadataController.value === controller) metadataController.value = null;
    }
  }

  function cancel(): void {
    activeController.value?.abort();
  }

  async function disconnect(): Promise<void> {
    ++connectionGeneration;
    connectionController.value?.abort();
    activeController.value?.abort();
    metadata.value = [];
    metadataController.value?.abort();
    const currentSessionId = sessionId.value;
    sessionId.value = '';
    databases.value = [];
    database.value = '';
    if (currentSessionId) {
      await apiClient.closeSQLConsoleSession(currentSessionId).catch(() => undefined);
    }
  }

  watch(requestedAccountId, () => {
    if (accounts.value.length === 0) return;
    const previousAccountId = accountId.value;
    applyRequestedAccount();
    if (accountId.value !== previousAccountId) void connect();
  });

  watch(database, () => void loadMetadata());

  // 注意:组件卸载不断开连接——模块级状态保留会话,重新进入页面时复用。
  // 后端会话有 idleTTL 自动过期,执行遇 404 时 execute() 会自动重连。

  return {
    accounts: readonly(accounts),
    databases: readonly(databases),
    accountId,
    database,
    sql,
    loadingAccounts: readonly(loadingAccounts),
    connecting: readonly(connecting),
    connected,
    executing: readonly(executing),
    error: readonly(error),
    result: shallowReadonly(result),
    metadata: readonly(metadata),
    selectedAccount,
    loadAccounts,
    connect,
    loadMetadata,
    execute,
    cancel,
    disconnect,
  };
}
