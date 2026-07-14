import { defineStore } from 'pinia';
import { computed, ref } from 'vue';

import { apiClient, type UserPreferences, type UserPreferencesUpdate } from '@/api/client';

const defaults: UserPreferences = {
  theme: 'system',
  ssh_client: '',
  ssh_client_path: '',
  terminal_font_family: 'Cascadia Mono, Consolas, monospace',
  terminal_font_size: 14,
};

export const usePreferencesStore = defineStore('preferences', () => {
  const value = ref<UserPreferences>({ ...defaults });
  const loaded = ref(false);
  const loading = ref(false);
  const saving = ref(false);
  const error = ref('');
  let mediaQuery: MediaQueryList | null = null;

  const hasSSHClient = computed(() => Boolean(value.value.ssh_client));

  function resolveDark(theme = value.value.theme): boolean {
    return theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  }

  function apply() {
    const dark = resolveDark();
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    document.documentElement.classList.toggle('dark', dark);
    document.documentElement.style.setProperty('--terminal-font-family', value.value.terminal_font_family);
    document.documentElement.style.setProperty('--terminal-font-size', `${value.value.terminal_font_size}px`);
    if (!mediaQuery) {
      mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
      mediaQuery.addEventListener('change', () => {
        if (value.value.theme === 'system') apply();
      });
    }
  }

  async function fetch(options: { force?: boolean } = {}) {
    if (loaded.value && !options.force) return value.value;
    loading.value = true;
    error.value = '';
    try {
      value.value = { ...defaults, ...(await apiClient.getMyPreferences()) };
      loaded.value = true;
      apply();
      return value.value;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '用户配置加载失败';
      apply();
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function update(patch: UserPreferencesUpdate) {
    saving.value = true;
    error.value = '';
    try {
      value.value = { ...defaults, ...(await apiClient.updateMyPreferences(patch)) };
      loaded.value = true;
      apply();
      return value.value;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '用户配置保存失败';
      throw err;
    } finally {
      saving.value = false;
    }
  }

  function reset() {
    value.value = { ...defaults };
    loaded.value = false;
    loading.value = false;
    saving.value = false;
    error.value = '';
    apply();
  }

  return { value, loaded, loading, saving, error, hasSSHClient, fetch, update, apply, reset };
});
