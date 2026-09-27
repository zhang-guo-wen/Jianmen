import assert from 'node:assert/strict';
import { nextTick, shallowRef } from 'vue';
import { afterEach, describe, it, vi } from 'vitest';

import { apiClient, type DBAccountRecord } from '@/api/client';

import { resetSQLConsoleState, useSQLConsole } from './useSQLConsole';

const accounts: DBAccountRecord[] = [
  { id: 'account-1', username: 'reader', instance_protocol: 'postgres' },
  { id: 'account-2', username: 'writer', instance_protocol: 'mysql' },
  { id: 'redis-account', username: 'cache', instance_protocol: 'redis' },
];

afterEach(() => {
  vi.restoreAllMocks();
  // useSQLConsole 为模块级单例,清空连接状态避免测试间污染
  resetSQLConsoleState();
});

describe('useSQLConsole requested account', () => {
  it('selects the account passed from the database Web connection entry', async () => {
    vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({
      items: accounts,
      total: accounts.length,
      page: 1,
      page_size: 200,
    });
    vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({
      id: 'session-1',
      databases: ['app', 'reporting'],
      default_database: 'app',
    });
    vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
    vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockResolvedValue({ tables: [] });
    const requestedAccountId = shallowRef('account-2');
    const consoleState = useSQLConsole({ requestedAccountId });

    await consoleState.loadAccounts();

    assert.equal(consoleState.accountId.value, 'account-2');
    assert.equal(consoleState.selectedAccount.value?.username, 'writer');
    assert.equal(consoleState.connected.value, true);
    assert.deepEqual(consoleState.databases.value, ['app', 'reporting']);
    assert.equal(consoleState.database.value, 'app');

    requestedAccountId.value = 'account-1';
    await nextTick();
    assert.equal(consoleState.accountId.value, 'account-1');
  });

  it('does not silently fall back to a different account for an invalid link', async () => {
    vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({
      items: accounts,
      total: accounts.length,
      page: 1,
      page_size: 200,
    });
    const consoleState = useSQLConsole({ requestedAccountId: 'missing-account' });

    await consoleState.loadAccounts();

    assert.equal(consoleState.accountId.value, '');
    assert.match(consoleState.error.value, /指定的数据库账号不可用/);
  });

  it('reuses the established session for repeated executions', async () => {
    vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({
      items: accounts,
      total: accounts.length,
      page: 1,
      page_size: 200,
    });
    const createSession = vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({
      id: 'session-reused',
      databases: ['app'],
      default_database: 'app',
    });
    const executeSQL = vi.spyOn(apiClient, 'executeSQL').mockResolvedValue({
      audit_session_id: 'audit-1',
      query_kind: 'select',
      read_only: true,
      columns: ['ready'],
      rows: [[1]],
      row_count: 1,
      rows_affected: 0,
      truncated: false,
      duration_ms: 2,
    });
    vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
    vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockResolvedValue({ tables: [] });

    const consoleState = useSQLConsole();
    await consoleState.loadAccounts();
    await consoleState.execute();
    await consoleState.execute();

    assert.equal(createSession.mock.calls.length, 1);
    assert.equal(executeSQL.mock.calls.length, 2);
    assert.equal(executeSQL.mock.calls[0]?.[0], 'session-reused');
    assert.equal(executeSQL.mock.calls[1]?.[0], 'session-reused');
  });

  it('连接成功后自动加载元数据,失败静默降级为空', async () => {
    vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({ items: accounts.slice(0, 1), total: 1, page: 1, page_size: 200 });
    vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({ id: 'session-1', databases: ['app'], default_database: 'app' });
    vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
    vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockResolvedValue({ tables: [{ name: 'users', columns: [{ name: 'id', type: 'bigint' }] }] });
    const consoleState = useSQLConsole();
    await consoleState.loadAccounts();
    assert.equal(consoleState.metadata.value.length, 1);
    assert.equal(consoleState.metadata.value[0].name, 'users');
  });

  it('元数据加载失败时静默置空,不抛错', async () => {
    vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({ items: accounts.slice(0, 1), total: 1, page: 1, page_size: 200 });
    vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({ id: 'session-1', databases: ['app'], default_database: 'app' });
    vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
    vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockRejectedValue(new Error('boom'));
    const consoleState = useSQLConsole();
    await consoleState.loadAccounts();
    assert.deepEqual(consoleState.metadata.value, []);
  });

  it('重新挂载(再次 loadAccounts)复用已有连接,不再新建会话', async () => {
    vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({
      items: accounts,
      total: accounts.length,
      page: 1,
      page_size: 200,
    });
    const createSession = vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({
      id: 'session-reused',
      databases: ['app'],
      default_database: 'app',
    });
    const closeSession = vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
    vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockResolvedValue({ tables: [] });

    // 首次进入:建立连接
    const first = useSQLConsole();
    await first.loadAccounts();
    assert.equal(first.connected.value, true);

    // 模拟组件卸载后再挂载:状态仍为模块级单例,复用同一连接
    const second = useSQLConsole();
    await second.loadAccounts();

    assert.equal(createSession.mock.calls.length, 1);
    assert.equal(closeSession.mock.calls.length, 0);
    assert.equal(second.connected.value, true);
  });
});
