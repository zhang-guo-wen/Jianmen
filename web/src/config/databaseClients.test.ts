import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildDBeaverConfigurationCommand,
  databaseClientExecutableExample,
  isValidDatabaseClientExecutablePath,
} from './databaseClients.ts';

test('database client executable paths are platform-aware', () => {
  assert.equal(isValidDatabaseClientExecutablePath('C:\\Program Files\\DBeaver\\dbeaverc.exe', 'windows'), true);
  assert.equal(isValidDatabaseClientExecutablePath('/Applications/DBeaver.app/Contents/MacOS/dbeaver', 'macos'), true);
  assert.equal(isValidDatabaseClientExecutablePath('/usr/bin/dbeaver-ce', 'linux'), true);
  assert.equal(isValidDatabaseClientExecutablePath('dbeaver.exe', 'windows'), false);
  assert.equal(isValidDatabaseClientExecutablePath('\\\\server\\share\\dbeaverc.exe', 'windows'), false);
  assert.equal(isValidDatabaseClientExecutablePath('C:\\Tools\\other.exe', 'windows'), false);
  assert.match(databaseClientExecutableExample('windows'), /dbeaverc\.exe$/i);
});

test('DBeaver configuration command never carries a password or custom browser URI', () => {
  const command = buildDBeaverConfigurationCommand({
    platform: 'windows',
    executablePath: "C:\\Program Files\\DBeaver\\dbeaverc.exe",
    protocol: 'postgres',
    host: 'gateway.db.example',
    port: 33060,
    username: 'ops+account@jianmen',
    databaseName: 'postgres',
    connectionName: "生产库's",
  });

  assert.match(command, /driver=postgresql/);
  assert.match(command, /connect=false/);
  assert.match(command, /savePassword=false/);
  assert.doesNotMatch(command, /(?:^|\|)password=/i);
  assert.doesNotMatch(command, /jianmen-db:\/\//i);
  assert.match(command, /生产库''s/);
});

test('DBeaver command rejects invalid paths and ports', () => {
  const base = {
    platform: 'linux' as const,
    executablePath: '/usr/bin/dbeaver-ce',
    protocol: 'mysql',
    host: 'gateway.db.example',
    port: 33060,
    username: 'ops',
  };
  assert.equal(buildDBeaverConfigurationCommand({ ...base, executablePath: 'dbeaver' }), '');
  assert.equal(buildDBeaverConfigurationCommand({ ...base, port: 70000 }), '');
  assert.equal(buildDBeaverConfigurationCommand({ ...base, protocol: 'mysql|password=secret' }), '');
});
