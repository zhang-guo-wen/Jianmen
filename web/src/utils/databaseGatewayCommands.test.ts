import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

import {
  DATABASE_COMMAND_PLATFORM,
  REDIS_COMMAND_UNAVAILABLE_REASON,
  buildDatabaseGatewayConnection,
  databaseGatewayCAFileName,
  databaseGatewayFallbackPort,
  hasDatabaseGatewayTLSIdentity,
  isSafeDatabaseCommandValue,
  resolveDatabaseGatewayPort,
  type DatabaseGatewayTLSIdentity,
} from './databaseGatewayCommands.ts';
import {
  databaseGatewayConnectionError,
  databaseProtocolLabel,
} from './databaseGatewayAvailability.ts';
import {
  beginInFlight,
  beginInFlightIfIdle,
  createLatestKeyedRequest,
  createRequestGenerationGuard,
  endInFlight,
  isInFlight,
} from './connectionRequestState.ts';
import { loadDatabaseConnectionResources } from './databaseConnectionOrchestration.ts';

const secureGateway: DatabaseGatewayTLSIdentity = {
  enabled: true,
  tls_enabled: true,
  tls_server_name: 'gateway.db.example',
  tls_ca_pem: '-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----',
  tls_cert_sha256: 'aa:bb',
};

test('database gateway availability distinguishes disabled entry from missing PostgreSQL TLS', () => {
  assert.equal(databaseProtocolLabel('postgresql'), 'PG');
  assert.equal(
    databaseGatewayConnectionError({
      enabled: false,
      connectable: false,
      unavailable_reason: 'listener_disabled',
      mode: 'independent',
      protocol: 'postgresql',
      listen_addr: '127.0.0.1:33062',
      host: '127.0.0.1',
      port: 33062,
      mysql_detection_delay_ms: 0,
      tls_enabled: false,
    }, 'postgres'),
    'PG 数据库网关未启用',
  );
  assert.equal(
    databaseGatewayConnectionError({
      enabled: true,
      connectable: false,
      unavailable_reason: 'tls_identity_missing',
      mode: 'unified',
      protocol: 'postgresql',
      listen_addr: '127.0.0.1:33060',
      host: '127.0.0.1',
      port: 33060,
      mysql_detection_delay_ms: 200,
      tls_enabled: false,
    }, 'postgresql'),
    '统一数据库入口已启用，但 PG 连接所需的 TLS 证书尚未就绪',
  );
});

test('database gateway fallback ports follow unified and independent modes', () => {
  for (const protocol of ['mysql', 'postgres', 'postgresql', 'redis']) {
    assert.equal(databaseGatewayFallbackPort(protocol, 'unified'), 33060);
    assert.equal(databaseGatewayFallbackPort(protocol, undefined), 33060);
  }

  assert.equal(databaseGatewayFallbackPort('mysql', 'independent'), 33061);
  assert.equal(databaseGatewayFallbackPort('postgres', 'independent'), 33062);
  assert.equal(databaseGatewayFallbackPort('postgresql', 'independent'), 33062);
  assert.equal(databaseGatewayFallbackPort('redis', 'independent'), 33063);
});

test('database gateway API port wins over every mode fallback', () => {
  assert.equal(resolveDatabaseGatewayPort('mysql', { mode: 'independent', port: 44001 }), 44001);
  assert.equal(resolveDatabaseGatewayPort('postgres', { mode: 'unified', port: 44002 }), 44002);
  assert.equal(resolveDatabaseGatewayPort('redis', { mode: 'independent', port: 0 }), 33063);
  assert.equal(resolveDatabaseGatewayPort('postgres', { mode: 'unified', port: 70000 }), 33060);
});

test('quick connect and connection dialog use the API-aware gateway resolver without the MySQL delay notice', () => {
  const quickConnect = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  const dialog = readFileSync(new URL('../components/ConnectionConfigDialog.vue', import.meta.url), 'utf8');

  for (const source of [quickConnect, dialog]) {
    assert.match(source, /resolveDatabaseGatewayPort/);
    assert.doesNotMatch(source, /统一入口需要短暂等待以识别连接协议/);
    assert.doesNotMatch(source, /function databaseGatewayDefaultPort/);
  }
});

test('PostgreSQL command has a bounded POSIX argv with dbname and GSS disabled', (context) => {
  const connection = buildDatabaseGatewayConnection({
    protocol: 'postgresql',
    gateway: secureGateway,
    port: 54330,
    username: 'ops+account@jianmen',
    databaseName: 'main_db',
  });

  assert.ok(connection);
  assert.ok(connection.command);
  assert.equal(DATABASE_COMMAND_PLATFORM, 'Linux/macOS/Git Bash');
  assert.equal(connection.commandPlatform, DATABASE_COMMAND_PLATFORM);
  assert.equal(connection.caFileName, 'jianmen-postgres-gateway-ca.pem');
  assert.equal(connection.caFilePath, './jianmen-postgres-gateway-ca.pem');
  assert.doesNotMatch(connection.command, /[&|<>^%!?;\r\n]/);

  const shell = findPOSIXShell();
  if (!shell) {
    context.skip('当前环境没有可用 POSIX shell；严格白名单测试仍会执行');
    return;
  }
  assert.deepEqual(captureCommandArguments(shell, 'psql', connection.command), [
    "host='gateway.db.example' port=54330 user='ops+account@jianmen' dbname='main_db' sslmode=verify-full sslrootcert='./jianmen-postgres-gateway-ca.pem' gssencmode=disable",
  ]);
});

test('PostgreSQL defaults dbname to postgres', () => {
  const connection = buildDatabaseGatewayConnection({
    protocol: 'postgres',
    gateway: secureGateway,
    port: 54330,
    username: 'ops',
  });

  assert.ok(connection?.command);
  assert.match(connection.command, /dbname=/);
  assert.match(connection.command, /postgres/);
  assert.match(connection.command, /gssencmode=disable/);
});

test('MySQL command has a bounded POSIX argv and VERIFY_IDENTITY', (context) => {
  const connection = buildDatabaseGatewayConnection({
    protocol: 'mysql',
    gateway: secureGateway,
    port: 33060,
    username: 'ops+account@jianmen',
  });

  assert.ok(connection?.command);
  assert.equal(connection.commandPlatform, 'Linux/macOS/Git Bash');
  assert.doesNotMatch(connection.command, /[&|<>^%!?;\r\n]/);
  const shell = findPOSIXShell();
  if (!shell) {
    context.skip('当前环境没有可用 POSIX shell；严格白名单测试仍会执行');
    return;
  }
  assert.deepEqual(captureCommandArguments(shell, 'mysql', connection.command), [
    '--protocol=tcp',
    '--ssl-mode=VERIFY_IDENTITY',
    '--ssl-ca=./jianmen-mysql-gateway-ca.pem',
    '-h',
    'gateway.db.example',
    '-P',
    '33060',
    '-u',
    'ops+account@jianmen',
    '-p',
  ]);
});

test('Redis keeps CA metadata but never emits a redis-cli command', () => {
  const connection = buildDatabaseGatewayConnection({
    protocol: 'redis',
    gateway: secureGateway,
    port: 63790,
    username: 'redis+account@jianmen',
  });

  assert.ok(connection);
  assert.equal(connection.command, null);
  assert.equal(connection.commandPlatform, null);
  assert.equal(REDIS_COMMAND_UNAVAILABLE_REASON, 'redis-cli 无法验证主机名，安全命令暂不可用');
  assert.equal(connection.unavailableReason, REDIS_COMMAND_UNAVAILABLE_REASON);
  assert.equal(connection.caFileName, 'jianmen-redis-gateway-ca.pem');
  assert.equal(connection.caFilePath, './jianmen-redis-gateway-ca.pem');
});

test('strict value allowlists reject CMD and POSIX metacharacters, whitespace, quotes, and NUL', () => {
  assert.equal(isSafeDatabaseCommandValue('host', 'gateway.db.example'), true);
  assert.equal(isSafeDatabaseCommandValue('username', 'ops+account@jianmen'), true);
  assert.equal(isSafeDatabaseCommandValue('databaseName', 'main_db-01'), true);

  const forbidden = ['&', '|', '<', '>', '^', '%', '!', '?', ';', ' ', '\t', '\r', '\n', "'", '"', '`', '$', '(', ')', '\0'];
  for (const character of forbidden) {
    assert.equal(isSafeDatabaseCommandValue('host', `gateway${character}evil`), false, `host accepted ${JSON.stringify(character)}`);
    assert.equal(isSafeDatabaseCommandValue('username', `ops${character}evil`), false, `username accepted ${JSON.stringify(character)}`);
    assert.equal(isSafeDatabaseCommandValue('databaseName', `db${character}evil`), false, `dbname accepted ${JSON.stringify(character)}`);
  }

  const fields = [
    { gateway: { ...secureGateway, tls_server_name: 'gateway&whoami' }, username: 'ops', databaseName: 'postgres' },
    { gateway: secureGateway, username: 'ops|whoami', databaseName: 'postgres' },
    { gateway: secureGateway, username: 'ops', databaseName: 'db;whoami' },
  ];
  for (const field of fields) {
    assert.equal(buildDatabaseGatewayConnection({
      protocol: 'postgres',
      port: 54330,
      ...field,
    }), null);
  }
});

test('fixed CA paths contain no CMD control characters', () => {
  for (const protocol of ['mysql', 'postgres', 'postgresql', 'redis']) {
    const fileName = databaseGatewayCAFileName(protocol);
    assert.match(fileName, /^[A-Za-z0-9._-]+$/);
    assert.doesNotMatch(`./${fileName}`, /[&|<>^%!?;\s"'`\0]/);
  }
});

test('all database protocols fail closed for incomplete TLS identity or invalid ports', () => {
  const noIdentity = { ...secureGateway, tls_ca_pem: '' };
  assert.equal(hasDatabaseGatewayTLSIdentity(noIdentity), false);
  for (const protocol of ['mysql', 'postgres', 'redis']) {
    assert.equal(buildDatabaseGatewayConnection({ protocol, gateway: noIdentity, port: 33060, username: 'ops' }), null);
    assert.equal(buildDatabaseGatewayConnection({ protocol, gateway: secureGateway, port: 0, username: 'ops' }), null);
    assert.equal(buildDatabaseGatewayConnection({ protocol, gateway: secureGateway, port: 65536, username: 'ops' }), null);
  }
});

test('request generations reject stale target results and invalidation', () => {
  const guard = createRequestGenerationGuard();
  const first = guard.begin('database:a');
  const second = guard.begin('database:b');

  assert.equal(guard.isCurrent(first, 'database:a'), false);
  assert.equal(guard.isCurrent(second, 'database:a'), false);
  assert.equal(guard.isCurrent(second, 'database:b'), true);
  guard.invalidate();
  assert.equal(guard.isCurrent(second, 'database:b'), false);
});

test('per-operation counters remain loading until every overlapping request finishes', () => {
  const counters: Record<string, { copy?: number; download?: number }> = {};

  beginInFlight(counters, 'account-a', 'copy');
  beginInFlight(counters, 'account-a', 'copy');
  beginInFlight(counters, 'account-a', 'download');
  endInFlight(counters, 'account-a', 'copy');
  assert.equal(isInFlight(counters, 'account-a', 'copy'), true);
  assert.equal(isInFlight(counters, 'account-a', 'download'), true);

  endInFlight(counters, 'account-a', 'copy');
  assert.equal(isInFlight(counters, 'account-a', 'copy'), false);
  assert.equal(isInFlight(counters, 'account-a', 'download'), true);
  endInFlight(counters, 'account-a', 'download');
  endInFlight(counters, 'account-a', 'download');
  assert.equal(isInFlight(counters, 'account-a', 'download'), false);
});

test('single-flight guard ignores a rapid duplicate operation while keeping the counter', () => {
  const counters: Record<string, { copy?: number; download?: number }> = {};
  assert.equal(beginInFlightIfIdle(counters, 'account-a', 'copy'), true);
  assert.equal(beginInFlightIfIdle(counters, 'account-a', 'copy'), false);
  assert.equal(counters['account-a']?.copy, 1);
  endInFlight(counters, 'account-a', 'copy');
});

test('database resource single-flight runs one loader for a rapid double click', async () => {
  const flight = createLatestKeyedRequest<number>();
  let calls = 0;
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const first = flight.begin('database:a', async () => {
    calls += 1;
    await gate;
    return calls;
  });
  const second = flight.begin('database:a', async () => {
    calls += 1;
    return calls;
  });
  assert.equal(first.promise, second.promise);
  assert.equal(flight.isLoading(), true);
  release();
  assert.equal(await first.promise, 1);
  assert.equal(calls, 1);
  assert.equal(flight.isLoading(), false);
  assert.equal(await flight.begin('database:a', async () => ++calls).promise, 2);
});

test('latest keyed requests keep loading until old and new targets both finish', async () => {
  const requests = createLatestKeyedRequest<string>();
  let releaseOld!: () => void;
  let releaseNew!: () => void;
  const oldGate = new Promise<void>(resolve => { releaseOld = resolve; });
  const newGate = new Promise<void>(resolve => { releaseNew = resolve; });
  const old = requests.begin('instance:old', async () => { await oldGate; return 'old'; });
  const newer = requests.begin('instance:new', async () => { await newGate; return 'new'; });

  assert.equal(requests.isLoading(), true);
  assert.equal(requests.isCurrent(old.token, 'instance:old'), false);
  assert.equal(requests.isCurrent(newer.token, 'instance:new'), true);
  releaseNew();
  assert.equal(await newer.promise, 'new');
  assert.equal(requests.isLoading(), true);
  releaseOld();
  assert.equal(await old.promise, 'old');
  assert.equal(requests.isLoading(), false);
});

test('disabled database client symbols are absent from production frontend sources', () => {
  const apiSource = readFileSync(new URL('../api/client.ts', import.meta.url), 'utf8');
  const preferencesSource = readFileSync(new URL('../stores/preferences.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(apiSource, /database_client|database_client_path/);
  assert.doesNotMatch(preferencesSource, /database_client|database_client_path/);
});

test('settings exposes local-only database client registration and never stores it as an account preference', () => {
  const settingsSource = readFileSync(new URL('../views/SettingsView.vue', import.meta.url), 'utf8');
  const localStoreSource = readFileSync(new URL('../stores/databaseClient.ts', import.meta.url), 'utf8');
  assert.match(settingsSource, /DBeaver/);
  assert.match(settingsSource, /useDatabaseClientStore/);
  assert.match(settingsSource, /buildDatabaseProtocolRegistrationCommand/);
  assert.match(settingsSource, /database_client_ca_path/);
  assert.match(settingsSource, /downloadDatabaseGatewayCA/);
  assert.match(settingsSource, /下载网关 CA/);
  assert.match(settingsSource, /我已执行以上命令/);
  assert.match(localStoreSource, /localStorage/);
  assert.match(localStoreSource, /caFilePath/);
  assert.match(localStoreSource, /directLaunchReady/);
  assert.doesNotMatch(localStoreSource, /apiClient/);
  const preferencesSource = readFileSync(new URL('../stores/preferences.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(preferencesSource, /dbeaver/i);
});

test('quick database client launch requires gateway TLS and embeds only the issued temporary password', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  const start = source.indexOf('async function openDatabaseClient');
  const end = source.indexOf('function openClientSettings', start);
  const launchSource = source.slice(start, end);
  assert.match(launchSource, /ensureDatabaseConnectionInfo\(account\)/);
  assert.match(launchSource, /state\.tlsIdentityReady/);
  assert.match(launchSource, /buildDatabaseProtocolURL/);
  assert.match(launchSource, /password:\s*state\.password/);
  assert.match(
    launchSource,
    /databaseName:\s*\['postgres', 'postgresql'\]\.includes\(protocol\.toLowerCase\(\)\) \? 'postgres' : ''/,
  );
  assert.match(launchSource, /databaseClient\.directLaunchReady/);
  assert.match(launchSource, /window\.location\.href = launchURL/);
  assert.doesNotMatch(launchSource, /downloadDatabaseGatewayCA|new Blob|已下载网关 CA/);
  assert.match(source, /createPassword:\s*accountID => apiClient\.createConnectionPassword\(accountID\)/);
});

test('database connection dialog opens the configured local client instead of copying a command', () => {
  const source = readFileSync(new URL('../components/ConnectionConfigDialog.vue', import.meta.url), 'utf8');
  assert.match(source, /data-testid="database-local-client"/);
  assert.match(source, /@click="openDatabaseClient"/);
  assert.match(source, /buildDatabaseProtocolURL\(\{/);
  assert.match(source, /password:\s*temporaryPassword\.value/);
  assert.match(source, /window\.location\.href = launchURL/);
  assert.match(source, /if \(!databaseClient\.configured\)[\s\S]*openDatabaseClientSettings\(\)/);
  assert.doesNotMatch(source, /复制 DBeaver 配置命令|copyDBeaverConfigurationCommand/);
});

test('quick database cards copy temporary connection credentials with an in-flight state', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  assert.match(source, /@click="copyDatabaseConnectionInfo\(account\)"/);
  assert.match(source, /:loading="databaseCredentialLoading\(account\)"/);
  assert.match(source, /loadDatabaseConnectionResources\(\{/);
  assert.match(source, /`连接地址：\$\{state\.host\}:\$\{state\.port\}`/);
  assert.match(source, /`连接账户：\$\{state\.compactUser\}`/);
  assert.match(source, /`连接临时密码：\$\{state\.password\}`/);
  assert.match(source, /Redis 暂不支持复制临时连接凭据/);
  assert.match(
    source,
    /\.database-card__actions\s*\{[\s\S]*grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)/,
  );
});

test('quick-connect client buttons redirect to settings only when local configuration is missing', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  assert.doesNotMatch(source, /<el-button @click="openDatabaseClientSettings">/);
  assert.match(source, /if \(!preferences\.hasSSHClient\)[\s\S]*openClientSettings\('ssh'\)/);
  assert.match(source, /if \(!databaseClient\.configured\)[\s\S]*openClientSettings\('database'\)/);
  assert.match(source, /query:\s*\{\s*tab,\s*return_to:\s*router\.currentRoute\.value\.fullPath\s*\}/);
  assert.match(
    source,
    /:loading="databaseClientLoading\(account\)"[\s\S]*?@click="openDatabaseClient\(account\)"/,
  );
  const databaseClientLoadingIndex = source.indexOf(':loading="databaseClientLoading(account)"');
  const databaseClientButtonStart = source.lastIndexOf('<el-button', databaseClientLoadingIndex);
  const databaseClientButtonEnd = source.indexOf('</el-button>', databaseClientLoadingIndex);
  const databaseClientButton = source.slice(databaseClientButtonStart, databaseClientButtonEnd);
  assert.doesNotMatch(databaseClientButton, /type="primary"/);
});

test('only disabled database gateways redirect super admins to system settings', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  assert.match(source, /function requireConnectableDatabaseGateway\(/);
  assert.match(source, /if \(gateway\?\.enabled\) throw new Error\(message\)/);
  assert.match(source, /permission\.canAccessMenu\('systemSettings'\)/);
  assert.match(source, /path:\s*'\/system-settings'/);
  assert.match(source, /请联系超级管理员完成系统配置/);
  assert.match(source, /error instanceof DatabaseGatewayConfigurationRedirect/);
  assert.doesNotMatch(source, /if \(!gateway\?\.enabled\) throw new Error/);
});

test('database and quick-connect loaders keep request snapshots isolated', () => {
  const databaseViewSource = readFileSync(new URL('../views/DatabaseView.vue', import.meta.url), 'utf8');
  const quickConnectSource = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  assert.match(databaseViewSource, /const instanceID = selectedInstance\.value\.id[\s\S]*const page = accountPage\.value[\s\S]*const pageSize = accountPageSize\.value/);
  assert.match(databaseViewSource, /getDBAccounts\(instanceID, \{[\s\S]*page,[\s\S]*page_size: pageSize/);
  assert.doesNotMatch(databaseViewSource, /savedCredentialTestRequests|savedCredentialTesting|savedCredentialTestResult/);
  assert.doesNotMatch(databaseViewSource, /testSavedAccountConnection|已保存凭据/);
  assert.match(quickConnectSource, /const sshRequests = createLatestKeyedRequest/);
  assert.match(quickConnectSource, /sshRequests\.begin\(keyword/);
  assert.match(quickConnectSource, /sshLoading\.value = sshRequests\.isLoading\(\)/);
});

test('database TLS mode preserves hidden inputs and clears persisted CA only on submit', () => {
  const source = readFileSync(new URL('../views/DatabaseView.vue', import.meta.url), 'utf8');
  const changeStart = source.indexOf('async function onTLSModeChange');
  const changeEnd = source.indexOf('function chooseTLSCAFile', changeStart);
  const changeSource = source.slice(changeStart, changeEnd);
  assert.match(changeSource, /上游数据库链路将不再使用 TLS/);
  assert.doesNotMatch(changeSource, /客户端到 Jianmen 的 TLS 不受影响/);
  assert.doesNotMatch(changeSource, /instanceForm\.(?:tlsCaPem|tlsServerName)\s*=\s*''/);
  assert.doesNotMatch(changeSource, /instanceForm\.hasTlsCa\s*=\s*false/);

  const submitStart = source.indexOf('async function submitInstance');
  const submitEnd = source.indexOf('async function toggleInstance', submitStart);
  const submitSource = source.slice(submitStart, submitEnd);
  assert.match(submitSource, /instanceForm\.tlsMode === 'disable' && originalHasTLSCA\.value/);
  assert.match(submitSource, /if \(clearStoredTLSCA\) payload\.clear_tls_ca = true/);
});

test('database account status switch has an account-specific accessible label', () => {
  const source = readFileSync(new URL('../views/DatabaseView.vue', import.meta.url), 'utf8');
  assert.match(source, /:aria-label="`\$\{row\.username \|\| '未命名账号'\}账号状态：\$\{row\.status === 'active' \? '启用' : '停用'\}`"/);
});

test('RDP quick connect is click-initiated and never hydrates SSH credentials', () => {
  const source = readFileSync(new URL('../views/QuickConnectView.vue', import.meta.url), 'utf8');
  assert.match(source, /path: isRDPTarget\(target\) \? '\/web-rdp' : '\/web-terminal'/);
  assert.match(source, /const queue = items\.filter\(target => !isRDPTarget\(target\)\)/);

  const ensureStart = source.indexOf('function ensureConnectionInfo');
  const ensureEnd = source.indexOf('async function retryConnectionInfo', ensureStart);
  const ensureSource = source.slice(ensureStart, ensureEnd);
  assert.match(
    ensureSource,
    /if \(isRDPTarget\(target\)\) \{[\s\S]*return Promise\.resolve\(initializeConnectionState\(target\)\);[\s\S]*createUserSession/,
  );
});

test('RDP audit playback uses object recording endpoint without replay directory exposure', () => {
  const source = readFileSync(new URL('../views/AuditView.vue', import.meta.url), 'utf8');
  assert.match(source, /new GuacamoleReplay\.SessionRecording\(tunnel\)/);
  assert.match(source, /apiClient\.getRDPRecordingURL\(session\.id\)/);

  const rdpReplayStart = source.indexOf('async function openRDPReplay');
  const rdpReplayEnd = source.indexOf('function toggleRDPReplay', rdpReplayStart);
  assert.doesNotMatch(source.slice(rdpReplayStart, rdpReplayEnd), /replay_dir/);
});

test('temporary password copy button exposes its in-flight state', () => {
  const dialogSource = readFileSync(new URL('../components/ConnectionConfigDialog.vue', import.meta.url), 'utf8');
  assert.match(
    dialogSource,
    /InfoValue label="临时密码"[\s\S]*:loading="isCopyInFlight\(temporaryPassword, '临时密码'\)"/,
  );
});

test('connection dialog clears temporary credentials when it closes', () => {
  const dialogSource = readFileSync(new URL('../components/ConnectionConfigDialog.vue', import.meta.url), 'utf8');
  assert.match(
    dialogSource,
    /function openDatabaseClient\(\)[\s\S]*!connectionInfo\.value \|\| !temporaryPassword\.value \|\| !secureGatewayTLS\.value/,
  );
  assert.match(
    dialogSource,
    /if \(!isVisible \|\| !targetID\)[\s\S]*clearConnectionState\(\)/,
  );
  assert.match(
    dialogSource,
    /function clearConnectionState\(\)[\s\S]*temporaryPassword\.value = ''/,
  );
});

test('auto-provision view does not display generated upstream identity or secret', () => {
  const databaseViewSource = readFileSync(new URL('../views/DatabaseView.vue', import.meta.url), 'utf8');
  assert.doesNotMatch(databaseViewSource, /provisionResult\.account\.username|generated_password|provision\.newUsername/);
});

test('Redis connection orchestration calls only the gateway loader', async () => {
  const calls: string[] = [];
  let sessionCalls = 0;
  let passwordCalls = 0;
  const result = await loadDatabaseConnectionResources({
    protocol: 'redis',
    targetID: 'database-account-1',
    getGateway: async protocol => {
      calls.push(`gateway:${protocol}`);
      return { protocol };
    },
    createSession: async targetID => {
      sessionCalls += 1;
      calls.push(`session:${targetID}`);
      return { compact_username: 'must-not-exist' };
    },
    createPassword: async targetID => {
      passwordCalls += 1;
      calls.push(`password:${targetID}`);
      return { password: 'must-not-exist' };
    },
  });

  assert.deepEqual(calls, ['gateway:redis']);
  assert.equal(sessionCalls, 0, 'Redis must not call createUserSession');
  assert.equal(passwordCalls, 0, 'Redis must not call createConnectionPassword');
  assert.deepEqual(result, {
    gateway: { protocol: 'redis' },
    session: null,
    credential: null,
  });
});

test('PostgreSQL and MySQL connection orchestration keep session and password loading', async () => {
  for (const protocol of ['postgres', 'mysql']) {
    const calls: string[] = [];
    const result = await loadDatabaseConnectionResources({
      protocol,
      targetID: 'database-account-1',
      getGateway: async value => {
        calls.push(`gateway:${value}`);
        return { protocol: value };
      },
      createSession: async targetID => {
        calls.push(`session:${targetID}`);
        return { compact_username: `${protocol}-user` };
      },
      createPassword: async targetID => {
        calls.push(`password:${targetID}`);
        return { password: `${protocol}-password` };
      },
    });

    assert.deepEqual(calls.sort(), [
      `gateway:${protocol}`,
      'password:database-account-1',
      'session:database-account-1',
    ]);
    assert.deepEqual(result, {
      gateway: { protocol },
      session: { compact_username: `${protocol}-user` },
      credential: { password: `${protocol}-password` },
    });
  }
});

test('database connection orchestration validates the gateway before issuing credentials', async () => {
  const calls: string[] = [];
  await assert.rejects(
    loadDatabaseConnectionResources({
      protocol: 'postgres',
      targetID: 'database-account-1',
      getGateway: async () => {
        calls.push('gateway');
        return { connectable: false };
      },
      validateGateway: () => {
        calls.push('validate');
        throw new Error('gateway unavailable');
      },
      createSession: async () => {
        calls.push('session');
        return {};
      },
      createPassword: async () => {
        calls.push('password');
        return {};
      },
    }),
    /gateway unavailable/,
  );
  assert.deepEqual(calls, ['gateway', 'validate']);
});

function findPOSIXShell(): string | null {
  const candidates = process.platform === 'win32'
    ? ['C:\\Program Files\\Git\\bin\\bash.exe', 'C:\\Program Files\\Git\\usr\\bin\\sh.exe']
    : [process.env.SHELL || '', '/bin/sh', '/bin/bash'];
  return candidates.find(candidate => candidate && existsSync(candidate)) || null;
}

function captureCommandArguments(shell: string, executable: 'mysql' | 'psql', command: string): string[] {
  const script = `${executable}() { printf '%s\\037' "$@"; }\n${command}`;
  const result = spawnSync(shell, ['-c', script], { encoding: 'utf8', windowsHide: true });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.split('\u001f').filter(Boolean);
}
