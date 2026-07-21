import assert from 'node:assert/strict';
import { nextTick, shallowRef } from 'vue';
import { afterEach, describe, it, vi } from 'vitest';

import { apiClient, type DBAccountRecord } from '@/api/client';

import { useSQLConsole } from './useSQLConsole';

const accounts: DBAccountRecord[] = [
  { id: 'account-1', username: 'reader', instance_protocol: 'postgres' },
  { id: 'account-2', username: 'writer', instance_protocol: 'mysql' },
  { id: 'redis-account', username: 'cache', instance_protocol: 'redis' },
];

afterEach(() => {
  vi.restoreAllMocks();
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

    const consoleState = useSQLConsole();
    await consoleState.loadAccounts();
    await consoleState.execute();
    await consoleState.execute();

    assert.equal(createSession.mock.calls.length, 1);
    assert.equal(executeSQL.mock.calls.length, 2);
    assert.equal(executeSQL.mock.calls[0]?.[0], 'session-reused');
    assert.equal(executeSQL.mock.calls[1]?.[0], 'session-reused');
  });
});
