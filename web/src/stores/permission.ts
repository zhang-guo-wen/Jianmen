import { defineStore } from 'pinia';
import { ref } from 'vue';

import { apiClient } from '@/api/client';

export const usePermissionStore = defineStore('permission', () => {
  const menus = ref<string[]>([]);
  const actions = ref<string[]>([]);
  const loaded = ref(false);

  async function fetch() {
    try {
      const [menuRes, permRes] = await Promise.all([
        apiClient.getMyMenus(),
        apiClient.getMyPermissions(),
      ]);
      menus.value = menuRes?.menus ?? [];
      actions.value = permRes?.actions ?? [];
    } catch {
      // On error, default to empty — user will see no menus
      menus.value = [];
      actions.value = [];
    } finally {
      loaded.value = true;
    }
  }

  function canAccessMenu(menuKey: string): boolean {
    return menus.value.includes(menuKey);
  }

  function canDo(action: string): boolean {
    return actions.value.includes(action);
  }

  function reset() {
    menus.value = [];
    actions.value = [];
    loaded.value = false;
  }

  return { menus, actions, loaded, fetch, canAccessMenu, canDo, reset };
});
