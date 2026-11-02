// 审计用途统计的取样条数上限。
// “常用”排序只需要近期使用情况，因此固定只读取最近一页，避免逐页拉取全量审计历史。
export const AUDIT_USAGE_SAMPLE_SIZE = 200;

export interface UsageSamplePage<T> {
  items?: T[];
  total?: number;
}

/**
 * 读取用途统计样本：只请求第一页，拿到 items 即停止。
 * 与 fetchAllPages 的区别是本函数不会翻页，请求次数恒为 1。
 */
export async function fetchRecentSample<T>(
  fetchPage: (page: number, pageSize: number) => Promise<UsageSamplePage<T>>,
  limit: number = AUDIT_USAGE_SAMPLE_SIZE,
): Promise<T[]> {
  const response = await fetchPage(1, limit);
  return response.items ?? [];
}

/** 按业务键累计样本内的使用次数，空键不计入。 */
export function countUsage<T>(items: readonly T[], keyOf: (item: T) => string): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const item of items) {
    const key = keyOf(item);
    if (!key) continue;
    counts[key] = (counts[key] ?? 0) + 1;
  }
  return counts;
}
