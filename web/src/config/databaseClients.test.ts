import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { setTimeout as delay } from 'node:timers/promises';
import { createPinia, setActivePinia } from 'pinia';

import {
  DATABASE_CLIENT_CA_FILE_NAME,
  DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION,
  buildDatabaseProtocolRegistrationCommand,
  buildDatabaseProtocolURL,
  databaseClientCAFileExample,
  databaseClientExecutableExample,
  isCurrentDatabaseClientProtocolRegistration,
  isValidDatabaseClientCAFilePath,
  isValidDatabaseClientExecutablePath,
} from './databaseClients.ts';
import { useDatabaseClientStore } from '../stores/databaseClient.ts';
import { usePreferencesStore } from '../stores/preferences.ts';

test('database client executable paths are platform-aware', () => {
  assert.equal(isValidDatabaseClientExecutablePath('C:\\Program Files\\DBeaver\\dbeaverc.exe', 'windows'), true);
  assert.equal(isValidDatabaseClientExecutablePath('/Applications/DBeaver.app/Contents/MacOS/dbeaver', 'macos'), true);
  assert.equal(isValidDatabaseClientExecutablePath('/usr/bin/dbeaver-ce', 'linux'), true);
  assert.equal(isValidDatabaseClientExecutablePath('dbeaver.exe', 'windows'), false);
  assert.equal(isValidDatabaseClientExecutablePath('\\\\server\\share\\dbeaverc.exe', 'windows'), false);
  assert.equal(isValidDatabaseClientExecutablePath('C:\\Tools\\other.exe', 'windows'), false);
  assert.match(databaseClientExecutableExample('windows'), /dbeaverc\.exe$/i);
  assert.match(databaseClientCAFileExample('windows'), new RegExp(`${DATABASE_CLIENT_CA_FILE_NAME.replace('.', '\\.')}$`, 'i'));
});

test('database client CA paths are absolute certificate files without connection separators', () => {
  assert.equal(isValidDatabaseClientCAFilePath('C:\\Users\\Alice\\Downloads\\gateway-ca.pem', 'windows'), true);
  assert.equal(isValidDatabaseClientCAFilePath('/home/alice/gateway-ca.crt', 'linux'), true);
  assert.equal(isValidDatabaseClientCAFilePath('/Users/alice/gateway-ca.cer', 'macos'), true);
  assert.equal(isValidDatabaseClientCAFilePath('gateway-ca.pem', 'windows'), false);
  assert.equal(isValidDatabaseClientCAFilePath('C:\\Temp\\gateway-ca.txt', 'windows'), false);
  assert.equal(isValidDatabaseClientCAFilePath('C:\\Temp\\gateway-ca.pem|password=secret', 'windows'), false);
  assert.equal(isValidDatabaseClientCAFilePath('C:\\Temp\\gateway-ca.pem\r\nconnect=true', 'windows'), false);
});

test('outdated database protocol registrations are invalidated after broker changes', () => {
  assert.equal(
    isCurrentDatabaseClientProtocolRegistration(
      true,
      DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION,
    ),
    true,
  );
  assert.equal(
    isCurrentDatabaseClientProtocolRegistration(
      true,
      DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION - 1,
    ),
    false,
  );
  assert.equal(isCurrentDatabaseClientProtocolRegistration(true, undefined), false);
  assert.equal(
    isCurrentDatabaseClientProtocolRegistration(
      false,
      DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION,
    ),
    false,
  );
});

test('database client store forces an old registered broker through registration again', () => {
  const values = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return values.size;
    },
    clear() {
      values.clear();
    },
    getItem(key) {
      return values.get(key) ?? null;
    },
    key(index) {
      return [...values.keys()][index] ?? null;
    },
    removeItem(key) {
      values.delete(key);
    },
    setItem(key, value) {
      values.set(key, value);
    },
  };
  const localStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
  const navigatorDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'navigator');
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });
  Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: { userAgent: 'Windows' },
  });
  // 写入 preferences store 的客户端配置缓存
  storage.setItem('jianmen_client_config', JSON.stringify({
    db_client: 'dbeaver',
    db_client_platform: 'windows',
    db_client_path: 'C:\\DBeaver\\dbeaverc.exe',
    db_client_ca_file_path: 'C:\\Users\\Alice\\Downloads\\jianmen-database-gateway-ca.pem',
  }));
  // 旧版本协议注册状态（版本号不匹配 → protocolRegistered=false）
  storage.setItem('jianmen_db_protocol_registered', JSON.stringify({
    registered: true,
    version: DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION - 1,
  }));

  try {
    setActivePinia(createPinia());
    const prefs = usePreferencesStore(); void prefs;
    const store = useDatabaseClientStore();
    assert.equal(store.configured, true);
    assert.equal(store.protocolRegistered, false);
    assert.equal(store.directLaunchReady, false);

    store.markRegistered();
    assert.equal(store.directLaunchReady, true);
    const persisted = JSON.parse(
      storage.getItem('jianmen_db_protocol_registered') || '{}',
    ) as Record<string, unknown>;
    assert.equal(
      persisted.version,
      DATABASE_CLIENT_PROTOCOL_REGISTRATION_VERSION,
    );
  } finally {
    restoreGlobalProperty('localStorage', localStorageDescriptor);
    restoreGlobalProperty('navigator', navigatorDescriptor);
  }
});

test('database client deep link carries only the generated temporary password for an immediate TLS connection', () => {
  const url = buildDatabaseProtocolURL({
    protocol: 'postgres',
    host: 'gateway.db.example',
    port: 33060,
    username: 'D000100001',
    password: 'temporary_password_1234567890',
    databaseName: 'postgres',
    connectionName: '生产库 / reporting',
  });
  assert.match(url, /^jianmen-db:\/\/connect\/[A-Za-z0-9_-]+$/);

  const payload = decodeLaunchPayload(url);
  assert.deepEqual(payload, {
    v: 2,
    driver: 'postgresql',
    host: 'gateway.db.example',
    port: 33060,
    database: 'postgres',
    user: 'D000100001',
    password: 'temporary_password_1234567890',
    name: '生产库 / reporting',
    tls: 'verify-full',
  });
});

test('database client deep link rejects connection-field injection and cleans display names', () => {
  const base = {
    protocol: 'mysql',
    host: 'gateway.db.example',
    port: 33060,
    username: 'D000100001',
    password: 'temporary_password_1234567890',
  };
  assert.equal(buildDatabaseProtocolURL({ ...base, host: 'gateway|password=secret' }), '');
  assert.equal(buildDatabaseProtocolURL({ ...base, username: 'ops;Start-Process' }), '');
  assert.equal(buildDatabaseProtocolURL({ ...base, protocol: 'mysql|connect=true' }), '');
  assert.equal(buildDatabaseProtocolURL({ ...base, password: 'secret|connect=true' }), '');

  const payload = decodeLaunchPayload(buildDatabaseProtocolURL({
    ...base,
    connectionName: '生产|password=secret\r\nconnect=true',
  }));
  assert.equal(payload.name, '生产 password secret connect true');
  assert.doesNotMatch(payload.name, /[|=\r\n]/);
});

test('Windows registration command validates every payload field and fixes safe DBeaver flags', () => {
  const command = buildDatabaseProtocolRegistrationCommand({
    client: 'dbeaver',
    platform: 'windows',
    executablePath: "C:\\Tools\\Bob's; Start-Process calc\\dbeaverc.exe",
    caFilePath: "C:\\Users\\Bob's\\Downloads\\jianmen-database-gateway-ca.pem",
    protocolRegistered: false,
  });
  assert.match(command, /HKCU\\Software\\Classes\\jianmen-db/);
  assert.match(command, /URL Protocol/);
  assert.match(command, /-File \\"%LOCALAPPDATA%\\Jianmen\\database-protocol\.ps1\\" \\"%1\\"/);
  assert.doesNotMatch(command, /-EncodedCommand[\s\S]*%1/);

  const script = decodeBrokerPowerShell(command);
  assert.match(script, /Parameter\(Mandatory=\$true, Position=0\)/);
  assert.match(script, /\$args\.Count -ne 0/);
  assert.match(script, /\$uri\.Scheme -cne 'jianmen-db'/);
  assert.match(script, /\$properties\.Count -ne \$allowed\.Count/);
  assert.match(script, /\[string\]\$data\.host -notmatch/);
  assert.match(script, /\[string\]\$data\.user -notmatch/);
  assert.match(script, /\[string\]\$data\.password -notmatch/);
  assert.match(script, /\$caFile = 'C:\\Users\\Bob''s\\Downloads\\jianmen-database-gateway-ca\.pem'/);
  assert.match(script, /Test-Path -LiteralPath \$caFile -PathType Leaf/);
  assert.match(script, /配置的网关 CA 文件不存在/);
  assert.match(script, /无法打开 DBeaver，请检查本地客户端路径和网关 CA 配置/);
  assert.match(script, /WScript\.Shell/);
  assert.match(script, /'savePassword=true'/);
  assert.match(script, /'create=true'/);
  assert.match(script, /'save=true'/);
  assert.match(script, /'connect=true'/);
  assert.match(script, /"password=\$\(\$data\.password\)"/);
  assert.match(script, /prop\.sslMode=VERIFY_IDENTITY/);
  assert.match(script, /prop\.sslmode=verify-full/);
  assert.match(script, /netHandler\.ssl\.ca\.cert=\$caFile/);
  assert.match(script, /netHandler\.ssl\.sslMode=verify-full/);
  assert.match(script, /netHandler\.ssl\.verify\.server=true/);
  assert.doesNotMatch(script, /savePassword=false|'save=false'/);
  assert.match(
    script,
    /Start-Process -FilePath 'C:\\Tools\\Bob''s; Start-Process calc\\dbeaverc\.exe' -ArgumentList/,
  );
});

test('Windows protocol broker receives the URI as a real script argument', {
  skip: process.platform !== 'win32',
}, async () => {
  const directory = mkdtempSync(join(tmpdir(), 'jianmen-db-broker-'));
  const helperDirectory = join(directory, 'DBeaver Helper');
  const caDirectory = join(directory, "Bob's CA");
  mkdirSync(helperDirectory);
  mkdirSync(caDirectory);
  const helperSource = join(directory, 'capture-argv.go');
  const helperExecutable = join(helperDirectory, 'dbeaverc.exe');
  const capturedArgumentsPath = join(directory, 'captured-arguments.json');
  writeFileSync(helperSource, `package main

import (
  "encoding/json"
  "os"
)

func main() {
  encoded, err := json.Marshal(os.Args[1:])
  if err != nil {
    os.Exit(2)
  }
  if err := os.WriteFile(os.Getenv("JIANMEN_TEST_ARGV_FILE"), encoded, 0600); err != nil {
    os.Exit(3)
  }
}
`, 'utf8');
  const buildHelper = spawnSync(
    'go',
    ['build', '-o', helperExecutable, helperSource],
    { cwd: directory, encoding: 'utf8', timeout: 30_000 },
  );
  assert.equal(buildHelper.status, 0, buildHelper.stderr || buildHelper.stdout);

  const caFilePath = join(caDirectory, 'gateway-ca.pem');
  writeFileSync(caFilePath, 'test gateway certificate', 'utf8');
  const command = buildDatabaseProtocolRegistrationCommand({
    client: 'dbeaver',
    platform: 'windows',
    executablePath: helperExecutable,
    caFilePath,
    protocolRegistered: false,
  });
  const installer = command.slice(0, command.indexOf(' && reg.exe add'));
  const installResult = spawnSync(installer, {
    encoding: 'utf8',
    timeout: 10_000,
    shell: true,
    env: { ...process.env, LOCALAPPDATA: directory },
  });
  assert.equal(installResult.status, 0, installResult.stderr || installResult.stdout);

  const scriptPath = join(directory, 'Jianmen', 'database-protocol.ps1');
  assert.equal(existsSync(scriptPath), true);
  const installedScript = readFileSync(scriptPath, 'utf8').replace(/^\uFEFF/, '').trimEnd();
  assert.equal(installedScript, decodeBrokerPowerShell(command));
  const url = buildDatabaseProtocolURL({
    protocol: 'postgres',
    host: 'gateway.db.example',
    port: 54320,
    username: 'D000100001',
    password: 'temporary_password_1234567890',
    databaseName: 'postgres',
    connectionName: '进程测试',
  });

  try {
    const result = spawnSync(
      'powershell.exe',
      ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', scriptPath, url],
      {
        encoding: 'utf8',
        timeout: 10_000,
        env: { ...process.env, JIANMEN_TEST_ARGV_FILE: capturedArgumentsPath },
      },
    );
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const deadline = Date.now() + 5_000;
    while (!existsSync(capturedArgumentsPath) && Date.now() < deadline) {
      await delay(25);
    }
    assert.equal(existsSync(capturedArgumentsPath), true, 'DBeaver test helper did not receive arguments');
    const capturedArguments = JSON.parse(readFileSync(capturedArgumentsPath, 'utf8')) as string[];
    assert.equal(capturedArguments.length, 2);
    assert.equal(capturedArguments[0], '-con');
    const connectionFields = capturedArguments[1]?.split('|') ?? [];
    assert.ok(connectionFields.includes('driver=postgresql'));
    assert.ok(connectionFields.includes('password=temporary_password_1234567890'));
    assert.ok(connectionFields.includes(`netHandler.ssl.ca.cert=${caFilePath}`));
    assert.ok(connectionFields.includes(`prop.sslrootcert=${caFilePath}`));
    assert.ok(connectionFields.includes('connect=true'));
    assert.ok(connectionFields.includes('save=true'));
    assert.ok(connectionFields.includes('savePassword=true'));

    const extraArgument = spawnSync(
      'powershell.exe',
      ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', scriptPath, url, 'unexpected'],
      { encoding: 'utf8', timeout: 10_000 },
    );
    assert.equal(extraArgument.status, 1, extraArgument.stderr || extraArgument.stdout);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test('database protocol registration never pretends to support non-Windows platforms', () => {
  assert.equal(buildDatabaseProtocolRegistrationCommand({
    client: 'dbeaver',
    platform: 'macos',
    executablePath: '/Applications/DBeaver.app/Contents/MacOS/dbeaver',
    caFilePath: '/Users/alice/Downloads/jianmen-database-gateway-ca.pem',
    protocolRegistered: false,
  }), '');
});

function decodeLaunchPayload(url: string): Record<string, any> {
  const encoded = new URL(url).pathname.slice(1);
  return JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8')) as Record<string, any>;
}

function decodeBrokerPowerShell(command: string): string {
  const encoded = command.match(/FromBase64String\('([A-Za-z0-9+/=]+)'\)/)?.[1];
  assert.ok(encoded);
  return Buffer.from(encoded, 'base64').toString('utf8');
}

function restoreGlobalProperty(
  name: 'localStorage' | 'navigator',
  descriptor: PropertyDescriptor | undefined,
): void {
  if (descriptor) {
    Object.defineProperty(globalThis, name, descriptor);
    return;
  }
  delete (globalThis as typeof globalThis & {
    localStorage?: Storage;
    navigator?: Navigator;
  })[name];
}
