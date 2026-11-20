import { ref, shallowRef } from 'vue';

export function useAuditBatches() {
  const view = shallowRef<{ mode: 'session' | 'batch'; batchId: string }>({ mode: 'session', batchId: '' });
  const batchPage = ref(1);
  const batchPageSize = ref(50);
  const batchKeyword = ref('');
  const memberPage = ref(1);
  const memberPageSize = ref(50);
  const memberKeyword = ref('');

  function selectMode(mode: 'session' | 'batch') {
    if (view.value.mode !== mode || view.value.batchId) view.value = { mode, batchId: '' };
  }

  function showSessions(batchId: string) {
    memberPage.value = 1;
    memberKeyword.value = '';
    view.value = { mode: 'session', batchId };
  }

  return { view, batchPage, batchPageSize, batchKeyword, memberPage, memberPageSize, memberKeyword, selectMode, showSessions };
}

export function useAuditPageRequest<T>() {
  const rows = shallowRef<T[]>([]);
  const total = ref(0);
  const loading = ref(false);
  const error = ref('');
  let active: AbortController | undefined;

  function cancel(clear = false) {
    active?.abort();
    active = undefined;
    loading.value = false;
    if (clear) {
      rows.value = [];
      total.value = 0;
      error.value = '';
    }
  }

  async function load(loader: (signal: AbortSignal) => Promise<{ items?: T[]; total?: number }>, fallback: string) {
    active?.abort();
    const request = new AbortController();
    active = request;
    error.value = '';
    loading.value = true;
    try {
      const result = await loader(request.signal);
      if (active !== request) return;
      rows.value = result.items ?? [];
      total.value = result.total ?? 0;
    } catch (err) {
      if (active !== request) return;
      rows.value = [];
      total.value = 0;
      error.value = err instanceof Error ? err.message : fallback;
    } finally {
      if (active === request) {
        active = undefined;
        loading.value = false;
      }
    }
  }

  return { rows, total, loading, error, load, cancel };
}
