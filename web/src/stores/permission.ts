import { defineStore } from 'pinia';
import { computed, ref } from 'vue';

import { apiClient, type AccessPage } from '@/api/client';

export const usePermissionStore = defineStore('permission', () => {
  const pages = ref<AccessPage[]>([]);
  const actions = ref<string[]>([]);
  const loaded = ref(false);
  const loading = ref(false);
  const error = ref('');
  let inFlight: Promise<boolean> | null = null;

  const menus = computed(() => pages.value.map(page => page.key));

  async function fetch(options: { force?: boolean } = {}): Promise<boolean> {
    if (loaded.value && !options.force && !error.value) return true;
    if (inFlight) return inFlight;

    loading.value = true;
    error.value = '';

    inFlight = apiClient.getMyAccessContext()
      .then((access) => {
        pages.value = [...(access.pages ?? [])].sort((left, right) => left.order - right.order);
        actions.value = access.actions ?? [];
        loaded.value = true;
        return true;
      })
      .catch((err: unknown) => {
        pages.value = [];
        actions.value = [];
        loaded.value = false;
        error.value = err instanceof Error ? err.message : '权限信息加载失败，请检查网络后重试';
        return false;
      })
      .finally(() => {
        loading.value = false;
        inFlight = null;
      });

    return inFlight;
  }

  function canAccessMenu(menuKey: string): boolean {
    return pages.value.some(page => page.key === menuKey);
  }

  function canDo(action: string): boolean {
    return actions.value.includes('*') || actions.value.includes(action);
  }

  function firstAccessiblePath(): string {
    return pages.value[0]?.path ?? '';
  }

  function reset() {
    pages.value = [];
    actions.value = [];
    loaded.value = false;
    loading.value = false;
    error.value = '';
    inFlight = null;
  }

  return {
    pages,
    menus,
    actions,
    loaded,
    loading,
    error,
    fetch,
    canAccessMenu,
    canDo,
    firstAccessiblePath,
    reset,
  };
});
