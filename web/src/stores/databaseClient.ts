import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import {
  DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION,
  isCurrentDatabaseClientProtocolRegistration,
  type DatabaseClientPlatform,
} from '@/config/databaseClients';
import { usePreferencesStore } from '@/stores/preferences';

/** 协议注册状态只存浏览器本地（机器相关，不存入后端） */
const REG_STORAGE_KEY = 'jianmen_db_protocol_registered';

function readProtocolRegistered(): boolean {
  try {
    const stored = JSON.parse(localStorage.getItem(REG_STORAGE_KEY) || '{}') as Record<string, unknown>;
    return isCurrentDatabaseClientProtocolRegistration(
      stored.registered,
      stored.version,
    );
  } catch {
    return false;
  }
}

function writeProtocolRegistered(registered: boolean) {
  localStorage.setItem(REG_STORAGE_KEY, JSON.stringify({
    registered,
    version: registered ? DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION : 0,
  }));
}

export const useDatabaseClientStore = defineStore('database-client', () => {
  const protocolRegistered = ref(readProtocolRegistered());
  const preferences = usePreferencesStore();

  const configured = computed(() => preferences.hasDBClient);

  const directLaunchReady = computed(() =>
    configured.value
    && preferences.value.db_client_platform === 'windows'
    && protocolRegistered.value,
  );

  const value = computed(() => ({
    client: preferences.value.db_client as '' | 'dbeaver',
    platform: preferences.value.db_client_platform as DatabaseClientPlatform,
    executablePath: preferences.value.db_client_path,
    caFilePath: preferences.value.db_client_ca_file_path,
    protocolRegistered: protocolRegistered.value,
  }));

  function markRegistered() {
    protocolRegistered.value = true;
    writeProtocolRegistered(true);
  }

  function markUnregistered() {
    protocolRegistered.value = false;
    writeProtocolRegistered(false);
  }

  return {
    value,
    configured,
    directLaunchReady,
    protocolRegistered,
    markRegistered,
    markUnregistered,
  };
});
