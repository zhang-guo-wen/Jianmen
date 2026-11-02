import assert from 'node:assert/strict';
import test from 'node:test';

import {
  AUDIT_USAGE_SAMPLE_SIZE,
  countUsage,
  fetchRecentSample,
} from './auditUsageSample';

test('用途取样只请求第一页，不逐页拉取全量审计历史', async () => {
  const requests: Array<{ page: number; pageSize: number }> = [];
  const items = await fetchRecentSample(async (page, pageSize) => {
    requests.push({ page, pageSize });
    // 明明还有更多页，取样也必须只取第一页就停止
    return { items: [{ id: 'a' }, { id: 'b' }], total: 10000 };
  });

  assert.deepEqual(requests, [{ page: 1, pageSize: AUDIT_USAGE_SAMPLE_SIZE }]);
  assert.deepEqual(items, [{ id: 'a' }, { id: 'b' }]);
  assert.equal(AUDIT_USAGE_SAMPLE_SIZE, 200);
});

test('用途取样在接口缺省 items 时返回空数组', async () => {
  const items = await fetchRecentSample(async () => ({ total: 0 }));

  assert.deepEqual(items, []);
});

test('用途计数按业务键累计并跳过空键', () => {
  const records = [
    { target: 'mysql-a', account: 'root' },
    { target: 'mysql-a', account: 'root' },
    { target: 'mysql-b', account: 'read' },
    { target: '', account: 'orphan' },
  ];

  const counts = countUsage(records, record => {
    const target = record.target.trim().toLowerCase();
    const account = record.account.trim().toLowerCase();
    return target && account ? `${target}\u0000${account}` : '';
  });

  assert.deepEqual(counts, {
    'mysql-a\u0000root': 2,
    'mysql-b\u0000read': 1,
  });
});
