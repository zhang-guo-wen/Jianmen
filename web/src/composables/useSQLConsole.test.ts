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
    const requestedAccountId = shallowRef('account-2');
    const consoleState = useSQLConsole({ requestedAccountId });

    await consoleState.loadAccounts();

    assert.equal(consoleState.accountId.value, 'account-2');
    assert.equal(consoleState.selectedAccount.value?.username, 'writer');

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
});
