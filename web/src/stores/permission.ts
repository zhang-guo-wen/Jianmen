import { defineStore } from 'pinia';
import { ref } from 'vue';

import { apiClient } from '@/api/client';

export const usePermissionStore = defineStore('permission', () => {
  const menus = ref<string[]>([]);
  const actions = ref<string[]>([]);
  const loaded = ref(false);
  const loading = ref(false);
  const error = ref('');
  let inFlight: Promise<boolean> | null = null;

  async function fetch(options: { force?: boolean } = {}): Promise<boolean> {
    if (loaded.value && !options.force && !error.value) return true;
    if (inFlight) return inFlight;

    loading.value = true;
    error.value = '';
    const hadLoadedData = loaded.value;

    inFlight = Promise.all([
      apiClient.getMyMenus(),
      apiClient.getMyPermissions(),
    ])
      .then(([menuRes, permRes]) => {
        menus.value = menuRes?.menus ?? [];
        actions.value = permRes?.actions ?? [];
        loaded.value = true;
        return true;
      })
      .catch((err: unknown) => {
        error.value = err instanceof Error ? err.message : '权限信息加载失败，请检查网络后重试';
        if (!hadLoadedData) {
          menus.value = ['quickConnect'];
          actions.value = [];
          loaded.value = true;
        }
        return hadLoadedData;
      })
      .finally(() => {
        loading.value = false;
        inFlight = null;
      });

    return inFlight;
  }

  function canAccessMenu(menuKey: string): boolean {
    return menus.value.includes(menuKey);
  }

  function canDo(action: string): boolean {
    return actions.value.includes('*') || actions.value.includes(action);
  }

  function reset() {
    menus.value = [];
    actions.value = [];
    loaded.value = false;
    loading.value = false;
    error.value = '';
    inFlight = null;
  }

  return { menus, actions, loaded, loading, error, fetch, canAccessMenu, canDo, reset };
});
