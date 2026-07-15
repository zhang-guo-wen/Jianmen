import { defineStore } from 'pinia';
import { computed, ref } from 'vue';

import { apiClient, type UserPreferences, type UserPreferencesUpdate } from '@/api/client';

const APPEARANCE_CACHE_KEY = 'jianmen_user_appearance';

const defaults: UserPreferences = {
  theme: 'system',
  ssh_client: '',
  ssh_client_path: '',
  terminal_font_family: 'Cascadia Mono, Consolas, monospace',
  terminal_font_size: 14,
};

function cachedAppearance(): Partial<UserPreferences> {
  try {
    const cached = JSON.parse(localStorage.getItem(APPEARANCE_CACHE_KEY) || '{}') as Partial<UserPreferences>;
    const theme = cached.theme;
    const fontSize = Number(cached.terminal_font_size);
    return {
      ...(theme === 'system' || theme === 'light' || theme === 'dark' ? { theme } : {}),
      ...(typeof cached.terminal_font_family === 'string' && cached.terminal_font_family.trim()
        ? { terminal_font_family: cached.terminal_font_family }
        : {}),
      ...(fontSize >= 10 && fontSize <= 30 ? { terminal_font_size: fontSize } : {}),
    };
  } catch {
    return {};
  }
}

export const usePreferencesStore = defineStore('preferences', () => {
  const value = ref<UserPreferences>({ ...defaults, ...cachedAppearance() });
  const loaded = ref(false);
  const loading = ref(false);
  const saving = ref(false);
  const error = ref('');
  let mediaQuery: MediaQueryList | null = null;

  const hasSSHClient = computed(() => Boolean(value.value.ssh_client));

  function resolveDark(theme = value.value.theme): boolean {
    return theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  }

  function persistAppearance() {
    localStorage.setItem(APPEARANCE_CACHE_KEY, JSON.stringify({
      theme: value.value.theme,
      terminal_font_family: value.value.terminal_font_family,
      terminal_font_size: value.value.terminal_font_size,
    }));
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
      persistAppearance();
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
      persistAppearance();
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
    localStorage.removeItem(APPEARANCE_CACHE_KEY);
    value.value = { ...defaults };
    loaded.value = false;
    loading.value = false;
    saving.value = false;
    error.value = '';
    apply();
  }

  return { value, loaded, loading, saving, error, hasSSHClient, fetch, update, apply, reset };
});
