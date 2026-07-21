import assert from 'node:assert/strict';
import test from 'node:test';
import { createPinia, setActivePinia } from 'pinia';

import { usePreferencesStore } from './preferences.ts';

test('preferences store defaults ssh client to xshell when nothing is saved', () => {
  const storage = createStorage();
  const localStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');

  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });

  try {
    setActivePinia(createPinia());
    const store = usePreferencesStore();
    assert.equal(store.value.ssh_client, 'xshell');
  } finally {
    restoreGlobalProperty('localStorage', localStorageDescriptor);
  }
});

test('persistPartialToBrowser merges only the requested fields', () => {
  const storage = createStorage({
    jianmen_client_config: JSON.stringify({
      theme: 'dark',
      terminal_font_family: 'Cascadia Mono',
      ssh_client: 'xshell',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
    }),
  });
  const localStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');

  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });

  try {
    setActivePinia(createPinia());
    const store = usePreferencesStore();

    store.persistPartialToBrowser({
      db_client: 'dbeaver',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
    });

    const cached = JSON.parse(storage.getItem('jianmen_client_config') || '{}');
    assert.equal(cached.theme, 'dark');
    assert.equal(cached.ssh_client, 'xshell');
    assert.equal(cached.db_client, 'dbeaver');
    assert.equal(cached.db_client_path, 'C:\\Program Files\\DBeaver\\dbeaverc.exe');
  } finally {
    restoreGlobalProperty('localStorage', localStorageDescriptor);
  }
});

function createStorage(seed: Record<string, string> = {}): Storage {
  const values = new Map(Object.entries(seed));
  return {
    get length() { return values.size; },
    clear() { values.clear(); },
    getItem(key) { return values.get(key) ?? null; },
    key(index) { return [...values.keys()][index] ?? null; },
    removeItem(key) { values.delete(key); },
    setItem(key, value) { values.set(key, value); },
  };
}

function restoreGlobalProperty(name: 'localStorage', descriptor?: PropertyDescriptor) {
  if (descriptor) Object.defineProperty(globalThis, name, descriptor);
  else Reflect.deleteProperty(globalThis, name);
}
