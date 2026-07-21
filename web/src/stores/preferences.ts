import { defineStore } from 'pinia';
import { computed, ref } from 'vue';

import { apiClient, type UserPreferences, type UserPreferencesUpdate } from '@/api/client';
import { isAbsoluteExecutablePath } from '@/config/sshClients';
import { isValidDatabaseClientCAFilePath, isValidDatabaseClientExecutablePath } from '@/config/databaseClients';

/** 浏览器本地缓存 key，保存用户确认后加载的完整配置。 */
const CLIENT_CACHE_KEY = 'jianmen_client_config';
const SSH_PROTOCOL_REGISTRATION_KEY = 'jianmen_ssh_protocol_registered';

const defaults: UserPreferences = {
  theme: 'light',
  ssh_client: '',
  ssh_client_path: '',
  ssh_client_platform: 'windows',
  db_client: 'dbeaver',
  db_client_platform: 'windows',
  db_client_path: '',
  db_client_ca_file_path: '',
  terminal_font_family: 'Cascadia Mono, Consolas, monospace',
  terminal_font_size: 14,
};

function cachedAppearance(): Partial<UserPreferences> {
  try {
    const cached = JSON.parse(localStorage.getItem(CLIENT_CACHE_KEY) || '{}') as Partial<UserPreferences>;
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

function readSSHProtocolRegistered(): boolean {
  return localStorage.getItem(SSH_PROTOCOL_REGISTRATION_KEY) === 'true';
}

function cachedClientConfig(): Partial<UserPreferences> {
  try {
    const cached = JSON.parse(localStorage.getItem(CLIENT_CACHE_KEY) || '{}') as Partial<UserPreferences>;
    const validPlatforms = ['windows', 'macos', 'linux'];
    return {
      ...(typeof cached.ssh_client === 'string' ? { ssh_client: cached.ssh_client } : {}),
      ...(typeof cached.ssh_client_path === 'string' ? { ssh_client_path: cached.ssh_client_path } : {}),
      ...(typeof cached.ssh_client_platform === 'string' && validPlatforms.includes(cached.ssh_client_platform) ? { ssh_client_platform: cached.ssh_client_platform } : {}),
      ...(typeof cached.db_client === 'string' ? { db_client: cached.db_client } : {}),
      ...(typeof cached.db_client_platform === 'string' && validPlatforms.includes(cached.db_client_platform) ? { db_client_platform: cached.db_client_platform } : {}),
      ...(typeof cached.db_client_path === 'string' ? { db_client_path: cached.db_client_path } : {}),
      ...(typeof cached.db_client_ca_file_path === 'string' ? { db_client_ca_file_path: cached.db_client_ca_file_path } : {}),
    };
  } catch {
    return {};
  }
}

export const usePreferencesStore = defineStore('preferences', () => {
  const value = ref<UserPreferences>({ ...defaults, ...cachedAppearance(), ...cachedClientConfig() });
  const loaded = ref(false);
  const loading = ref(false);
  const saving = ref(false);
  const error = ref('');
  const sshProtocolRegistered = ref(readSSHProtocolRegistered());
  let mediaQuery: MediaQueryList | null = null;

  const hasSSHClient = computed(() => value.value.ssh_client === 'default' || Boolean(value.value.ssh_client && isAbsoluteExecutablePath(value.value.ssh_client_path)));

  /** 数据库客户端是否已完整配置（非 TLS 快速连接仅要求有效程序路径） */
  const hasDBClient = computed(() => {
    const v = value.value;
    return v.db_client === 'dbeaver'
      && v.db_client_platform === 'windows'
      && isValidDatabaseClientExecutablePath(v.db_client_path, 'windows')
      && (
        !v.db_client_ca_file_path.trim()
        || isValidDatabaseClientCAFilePath(v.db_client_ca_file_path, 'windows')
      );
  });

  function resolveDark(theme = value.value.theme): boolean {
    return theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  }

  /** 将数据库中的完整配置写入当前浏览器缓存。 */
  function persistClientConfig() {
    localStorage.setItem(CLIENT_CACHE_KEY, JSON.stringify(value.value));
  }

  function persistAppearance() {
    localStorage.setItem(CLIENT_CACHE_KEY, JSON.stringify({
      ...JSON.parse(localStorage.getItem(CLIENT_CACHE_KEY) || '{}'),
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
      const response = await apiClient.getMyPreferences();
      const server = { ...defaults, ...response, db_client: response.db_client || defaults.db_client };
      value.value = {
        ...server,
        ...cachedAppearance(),
      };
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
      const server = await apiClient.updateMyPreferences(patch);
      value.value = { ...defaults, ...server, db_client: server.db_client || defaults.db_client };
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

  /** 用户确认已执行协议注册命令后，才将数据库配置加载到浏览器。 */
  async function loadToBrowser() {
    loading.value = true;
    error.value = '';
    try {
      const response = await apiClient.getMyPreferences();
      value.value = {
        ...defaults,
        ...response,
        db_client: response.db_client || defaults.db_client,
        ...cachedAppearance(),
      };
      loaded.value = true;
      persistClientConfig();
      apply();
      return value.value;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '加载配置失败';
      throw err;
    } finally {
      loading.value = false;
    }
  }

  function markSSHProtocolRegistered(registered: boolean) {
    sshProtocolRegistered.value = registered;
    localStorage.setItem(SSH_PROTOCOL_REGISTRATION_KEY, String(registered));
  }

  function reset() {
    localStorage.removeItem(CLIENT_CACHE_KEY);
    localStorage.removeItem(SSH_PROTOCOL_REGISTRATION_KEY);
    value.value = { ...defaults };
    loaded.value = false;
    loading.value = false;
    saving.value = false;
    error.value = '';
    apply();
  }

  return { value, loaded, loading, saving, error, sshProtocolRegistered, hasSSHClient, hasDBClient, fetch, update, loadToBrowser, markSSHProtocolRegistered, apply, reset };
});
