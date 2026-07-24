import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { buildConnectionCommands, type ConnectionCommandInput } from '../utils/connectionConfigCommands.ts';

test('connection commands include both SSH and SFTP when both are allowed', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: true,
    allowSftp: true,
    connectionInfo: {
      host: '10.0.0.1',
      port: 2222,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 2);
  assert.equal(commands[0]?.label.includes('SSH'), true);
  assert.equal(commands[1]?.label.includes('SFTP'), true);
  assert.equal(commands[0]?.value, 'ssh admin@10.0.0.1 -p 2222');
  assert.equal(commands[1]?.value, 'sftp -P 2222 admin@10.0.0.1');
});

test('connection commands include only SSH when SFTP is denied', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: true,
    allowSftp: false,
    connectionInfo: {
      host: '10.0.0.1',
      port: 22,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 1);
  assert.equal(commands[0]?.label.includes('SSH'), true);
  assert.equal(commands.some(item => item.label.includes('SFTP')), false);
});

test('connection commands include only SFTP when SSH is denied', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: false,
    allowSftp: true,
    connectionInfo: {
      host: '10.0.0.1',
      port: 22,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 1);
  assert.equal(commands[0]?.label.includes('SFTP'), true);
  assert.equal(commands.some(item => item.label.includes('SSH')), false);
});

test('connection commands hidden when both SSH and SFTP are denied', () => {
  const input: ConnectionCommandInput = {
    resourceType: 'host',
    allowSsh: false,
    allowSftp: false,
    connectionInfo: {
      host: '10.0.0.1',
      port: 22,
      compactUser: 'admin',
    },
    databaseConnection: null,
  };

  const commands = buildConnectionCommands(input);
  assert.equal(commands.length, 0);
});

test('ConnectionConfigDialog props use Vue-normalized casing', () => {
  const componentPath = new URL('../components/ConnectionConfigDialog.vue', import.meta.url);
  const componentSource = readFileSync(componentPath, 'utf8');

  assert.ok(/allowSsh\?: boolean/.test(componentSource));
  assert.ok(/allowSftp\?: boolean/.test(componentSource));
  assert.ok(/allowWebSql\?: boolean/.test(componentSource));
  assert.equal(/allowSSH/.test(componentSource), false);
  assert.equal(/allowSFTP/.test(componentSource), false);

  assert.ok(/allowSsh/.test(componentSource));
  assert.ok(/allowSftp/.test(componentSource));

  assert.equal(/:\s*allowSSH/.test(componentSource), false);
  assert.equal(/:\s*allowSFTP/.test(componentSource), false);
});

test('database connection dialog hides TLS metadata and setup controls while keeping direct client launch', () => {
  const componentPath = new URL('../components/ConnectionConfigDialog.vue', import.meta.url);
  const componentSource = readFileSync(componentPath, 'utf8');

  for (const removedText of [
    'TLS 验证名称',
    '证书 SHA-256 指纹',
    '数据库名称',
    '请先下载 CA 并保存为下方命令引用的文件名',
    '下载 CA',
    '复制 CA',
    '复制指纹',
    '本地客户端设置',
    '复制 DBeaver 配置命令',
  ]) {
    assert.equal(componentSource.includes(removedText), false, `unexpected control: ${removedText}`);
  }

  for (const removedSymbol of [
    'buildDBeaverConfigurationCommand',
    'dbeaverConfigurationCommand',
    'copyDBeaverConfigurationCommand',
    'downloadGatewayCA',
    'databaseGatewayCAFileName',
    'gateway-tls-panel',
    'gateway-tls-hint',
    'gateway-tls-actions',
  ]) {
    assert.equal(componentSource.includes(removedSymbol), false, `unexpected symbol: ${removedSymbol}`);
  }
  assert.match(componentSource, /data-testid="database-local-client"/);
  assert.match(componentSource, /useDatabaseClientStore/);
  assert.match(componentSource, /buildDatabaseProtocolURL/);
  assert.match(componentSource, /function openClientSettings\(tab: 'ssh' \| 'database'\)/);

  const planStart = componentSource.indexOf('const databaseConnectionPlan');
  const planEnd = componentSource.indexOf('const databaseCommandUnavailableReason', planStart);
  const planSource = componentSource.slice(planStart, planEnd);
  assert.ok(planStart >= 0 && planEnd > planStart);
  assert.match(planSource, /tls_server_name:\s*tlsServerName/);
  assert.match(planSource, /tls_ca_pem:\s*tlsCAPEM/);
  assert.match(planSource, /tls_cert_sha256:\s*tlsCertSHA256/);
  assert.doesNotMatch(planSource, /databaseName/);
});

test('SSH client registration is handled by personal settings instead of the connection dialog', () => {
  const componentPath = new URL('../components/ConnectionConfigDialog.vue', import.meta.url);
  const componentSource = readFileSync(componentPath, 'utf8');

  assert.match(
    componentSource,
    /if \(!preferences\.hasSSHClient \|\| !preferences\.sshProtocolRegistered\)[\s\S]*openClientSettings\('ssh'\)/,
  );
  assert.match(componentSource, /path:\s*'\/settings'/);
  for (const removedSymbol of [
    'initClientVisible',
    'buildSSHProtocolRegistrationCommand',
    'saveClientAndCopyCommand',
    'local-client-dialog',
  ]) {
    assert.equal(componentSource.includes(removedSymbol), false, `unexpected embedded registration symbol: ${removedSymbol}`);
  }
});

test('ConnectionConfigDialog call sites keep kebab-case attrs', () => {
  const callers = [
    { file: new URL('../views/DatabaseView.vue', import.meta.url), expected: [':allow-ssh=', ':allow-web-sql='] },
    { file: new URL('../views/HostsView.vue', import.meta.url), expected: [':allow-ssh=', ':allow-sftp='] },
  ];

  for (const caller of callers) {
    const viewSource = readFileSync(caller.file, 'utf8');

    assert.equal(/:allowSSH/.test(viewSource), false);
    assert.equal(/:allowSFTP/.test(viewSource), false);
    assert.ok(/<ConnectionConfigDialog/.test(viewSource));
    for (const attr of caller.expected) {
      assert.ok(viewSource.includes(attr), `missing ${attr} in ${caller.file}`);
    }
    assert.ok(/:allow-(ssh|sftp)=/.test(viewSource), `missing kebab prop in ${caller.file}`);
  }
});

test('SQL console stays routable but is hidden from the primary sidebar', () => {
  const navigationSource = readFileSync(new URL('../navigation.ts', import.meta.url), 'utf8');
  const appSource = readFileSync(new URL('../App.vue', import.meta.url), 'utf8');

  assert.match(navigationSource, /key:\s*'sqlConsole'[\s\S]*?hidden:\s*true/);
  assert.match(appSource, /!item\.hidden\s*&&\s*permission\.canAccessMenu\(item\.key\)/);
});
