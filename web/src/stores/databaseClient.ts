import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import {
  DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION,
  detectDatabaseClientPlatform,
  isCurrentDatabaseClientProtocolRegistration,
  isValidDatabaseClientCAFilePath,
  isValidDatabaseClientExecutablePath,
  type DatabaseClientSettings,
} from '@/config/databaseClients';

const STORAGE_KEY = 'jianmen_local_database_client';

function defaults(): DatabaseClientSettings {
  return {
    client: '',
    platform: detectDatabaseClientPlatform(),
    executablePath: '',
    caFilePath: '',
    protocolRegistered: false,
  };
}

function readSettings(): DatabaseClientSettings {
  const fallback = defaults();
  try {
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}') as Partial<DatabaseClientSettings> & {
      protocolRegistrationVersion?: unknown;
    };
    const platform = stored.platform === 'windows' || stored.platform === 'macos' || stored.platform === 'linux'
      ? stored.platform
      : fallback.platform;
    return {
      client: stored.client === 'dbeaver' ? 'dbeaver' : '',
      platform,
      executablePath: typeof stored.executablePath === 'string' ? stored.executablePath : '',
      caFilePath: typeof stored.caFilePath === 'string' ? stored.caFilePath : '',
      protocolRegistered: isCurrentDatabaseClientProtocolRegistration(
        stored.protocolRegistered,
        stored.protocolRegistrationVersion,
      ),
    };
  } catch {
    return fallback;
  }
}

export const useDatabaseClientStore = defineStore('database-client', () => {
  const value = ref<DatabaseClientSettings>(readSettings());

  const configured = computed(() =>
    value.value.client === 'dbeaver'
    && isValidDatabaseClientExecutablePath(value.value.executablePath, value.value.platform)
    && isValidDatabaseClientCAFilePath(value.value.caFilePath, value.value.platform),
  );
  const directLaunchReady = computed(() =>
    configured.value
    && value.value.platform === 'windows'
    && value.value.protocolRegistered,
  );

  function update(settings: DatabaseClientSettings) {
    const nextValue = {
      ...settings,
      executablePath: settings.executablePath.trim(),
      caFilePath: settings.caFilePath.trim(),
      protocolRegistered: settings.client === 'dbeaver'
        && settings.platform === 'windows'
        && settings.protocolRegistered,
    };
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      ...nextValue,
      protocolRegistrationVersion: nextValue.protocolRegistered
        ? DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION
        : 0,
    }));
    value.value = nextValue;
  }

  function reset() {
    localStorage.removeItem(STORAGE_KEY);
    value.value = defaults();
  }

  return { value, configured, directLaunchReady, update, reset };
});
