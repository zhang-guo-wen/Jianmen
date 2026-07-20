import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import {
  buildDatabaseProtocolRegistrationCommand,
  buildDatabaseProtocolURL,
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

test('database client deep link is a disconnected TLS-verified draft without secrets', () => {
  const url = buildDatabaseProtocolURL({
    protocol: 'postgres',
    host: 'gateway.db.example',
    port: 33060,
    username: 'D000100001',
    databaseName: 'postgres',
    connectionName: '生产库 / reporting',
  });
  assert.match(url, /^jianmen-db:\/\/connect\/[A-Za-z0-9_-]+$/);

  const payload = decodeLaunchPayload(url);
  assert.deepEqual(payload, {
    v: 1,
    driver: 'postgresql',
    host: 'gateway.db.example',
    port: 33060,
    database: 'postgres',
    user: 'D000100001',
    name: '生产库 / reporting',
    tls: 'verify-full',
  });
  const serialized = JSON.stringify(payload);
  assert.doesNotMatch(serialized, /password/i);
  assert.doesNotMatch(serialized, /savePassword=true|connect=true/i);
});

test('database client deep link rejects connection-field injection and cleans display names', () => {
  const base = {
    protocol: 'mysql',
    host: 'gateway.db.example',
    port: 33060,
    username: 'D000100001',
  };
  assert.equal(buildDatabaseProtocolURL({ ...base, host: 'gateway|password=secret' }), '');
  assert.equal(buildDatabaseProtocolURL({ ...base, username: 'ops;Start-Process' }), '');
  assert.equal(buildDatabaseProtocolURL({ ...base, protocol: 'mysql|connect=true' }), '');

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
  assert.match(script, /'savePassword=false'/);
  assert.match(script, /'create=true'/);
  assert.match(script, /'save=false'/);
  assert.match(script, /'connect=false'/);
  assert.match(script, /prop\.sslMode=VERIFY_IDENTITY/);
  assert.match(script, /prop\.sslmode=verify-full/);
  assert.doesNotMatch(script, /savePassword=true|connect=true|(?:^|[|'" ])password=/im);
  assert.match(
    script,
    /Start-Process -FilePath 'C:\\Tools\\Bob''s; Start-Process calc\\dbeaverc\.exe' -ArgumentList/,
  );
});

test('Windows protocol broker receives the URI as a real script argument', {
  skip: process.platform !== 'win32',
}, () => {
  const command = buildDatabaseProtocolRegistrationCommand({
    client: 'dbeaver',
    platform: 'windows',
    executablePath: 'C:\\Tools\\DBeaver\\dbeaverc.exe',
    protocolRegistered: false,
  });
  const systemRoot = process.env.SystemRoot || 'C:\\Windows';
  const whereExecutable = join(systemRoot, 'System32', 'where.exe').replace(/'/g, "''");
  const directory = mkdtempSync(join(tmpdir(), 'jianmen-db-broker-'));
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
  writeFileSync(
    scriptPath,
    `\uFEFF${installedScript.replace(
      "'C:\\Tools\\DBeaver\\dbeaverc.exe'",
      `'${whereExecutable}'`,
    )}`,
    'utf8',
  );
  const url = buildDatabaseProtocolURL({
    protocol: 'postgres',
    host: 'gateway.db.example',
    port: 54320,
    username: 'D000100001',
    databaseName: 'postgres',
    connectionName: '进程测试',
  });

  try {
    const result = spawnSync(
      'powershell.exe',
      ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', scriptPath, url],
      { encoding: 'utf8', timeout: 10_000 },
    );
    assert.equal(result.status, 0, result.stderr || result.stdout);

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
