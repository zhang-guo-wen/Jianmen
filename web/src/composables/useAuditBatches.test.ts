import { describe, expect, it } from 'vitest';
import { useAuditBatches, useAuditPageRequest } from './useAuditBatches';

describe('审计批次', () => {
  it('restores batch list filters and page after drill down', () => {
    const audit = useAuditBatches();
    expect(audit.view.value).toEqual({ mode: 'session', batchId: '' });
    audit.selectMode('batch');
    audit.batchPage.value = 3;
    audit.batchKeyword.value = 'orders';
    audit.showSessions('batch-1');
    expect(audit.view.value).toEqual({ mode: 'session', batchId: 'batch-1' });
    audit.selectMode('batch');
    expect(audit.view.value).toEqual({ mode: 'batch', batchId: '' });
    expect(audit.batchPage.value).toBe(3);
    expect(audit.batchKeyword.value).toBe('orders');
  });

  it('ignores stale batch log responses', async () => {
    const page = useAuditPageRequest<string>();
    let completeOld!: (result: { items: string[]; total: number }) => void;
    const old = page.load(() => new Promise(resolve => { completeOld = resolve; }), '错误');
    await page.load(async () => ({ items: ['当前结果'], total: 1 }), '错误');
    completeOld({ items: ['过期结果'], total: 100 });
    await old;
    expect(page.rows.value).toEqual(['当前结果']);
    expect(page.total.value).toBe(1);
    page.cancel(true);
    expect(page.rows.value).toEqual([]);
  });
});
