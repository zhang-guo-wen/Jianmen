import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const source = readFileSync(new URL('./AuditView.vue', import.meta.url), 'utf8');
const i18nSource = readFileSync(new URL('../i18n/index.ts', import.meta.url), 'utf8');

function pane(name: string): string {
  const marker = `name="${name}"`;
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, `missing ${name} audit pane`);
  const end = source.indexOf('</el-tab-pane>', start);
  assert.notEqual(end, -1, `missing ${name} audit pane closing tag`);
  return source.slice(start, end);
}

function assertOrder(name: string, keys: string[]) {
  const block = pane(name);
  let previous = -1;
  for (const key of keys) {
    const position = block.indexOf(`t('${key}')`, previous + 1);
    assert.ok(position > previous, `${name}: ${key} must follow the previous column`);
    previous = position;
  }
}

test('audit tables use contextual names in a stable order', () => {
  assertOrder('ssh', [
    'audit.column.startedAt',
    'audit.column.operator',
    'audit.column.authSessionId',
    'audit.column.targetHost',
    'audit.column.hostAccount',
    'audit.column.sourceIp',
    'audit.column.protocol',
    'audit.column.result',
    'audit.column.duration',
    'audit.column.eventCount',
    'common.actions',
  ]);
  assertOrder('rdp', [
    'audit.column.startedAt',
    'audit.column.operator',
    'audit.column.authSessionId',
    'audit.column.targetHost',
    'audit.column.hostAccount',
    'audit.column.sourceIp',
    'audit.column.result',
    'audit.column.duration',
    'audit.column.recordingStatus',
    'common.actions',
  ]);
  assertOrder('db', [
    'audit.column.startedAt',
    'audit.column.operator',
    'audit.column.authSessionId',
    'audit.column.databaseInstance',
    'audit.column.databaseAccount',
    'audit.column.sourceIp',
    'audit.column.protocol',
    'audit.column.result',
    'audit.column.duration',
    'audit.column.sqlCount',
    'common.actions',
  ]);
  assertOrder('online', [
    'audit.column.startedAt',
    'audit.column.operator',
    'audit.column.authSessionId',
    'audit.column.targetResource',
    'audit.column.protocol',
    'audit.column.loginAccount',
    'common.actions',
  ]);
  assertOrder('logins', [
    'audit.column.loginTime',
    'audit.column.loginAccount',
    'audit.column.sourceIp',
    'audit.column.loginResult',
    'audit.column.resultDetail',
    'audit.column.clientEnvironment',
  ]);
  assertOrder('operations', [
    'audit.column.operationTime',
    'audit.column.operator',
    'audit.column.operationType',
    'audit.column.resourceType',
    'audit.column.operationTarget',
    'audit.column.sourceIp',
    'audit.column.requestId',
    'audit.column.httpStatus',
    'audit.column.result',
  ]);
});

test('audit columns use shared semantic presets and localized headers', () => {
  for (const name of ['ssh', 'rdp', 'db', 'online']) {
    const block = pane(name);
    assert.match(block, /TABLE_COLUMNS\.time/);
    assert.match(block, /TABLE_COLUMNS\.identifier/);
    assert.match(block, /TABLE_COLUMNS\.actions(?:Compact|Wide)?/);
  }
  assert.doesNotMatch(pane('rdp'), /<el-table-column[^>]*\slabel="[^"]+"/);
  assert.equal(source.match(/<AuditSessionLink/g)?.length, 4);
});

test('the short user session identifier is named unambiguously', () => {
  assert.match(i18nSource, /'audit\.column\.authSessionId': '授权会话 ID'/);
  assert.match(i18nSource, /'audit\.sessionId': '授权会话 ID'/);
  assert.doesNotMatch(pane('ssh'), />SessionID</);
  assert.doesNotMatch(pane('db'), />SessionID</);
  assert.doesNotMatch(pane('online'), />SessionID</);
});

test('switching audit tabs refreshes only the selected scope without polling online sessions', () => {
  assert.match(source, /<el-tabs v-model="auditScope" class="page-tabs" @tab-change="loadAuditScope">/);

  const loaderStart = source.indexOf('function loadAuditScope');
  const loaderEnd = source.indexOf('onMounted(', loaderStart);
  assert.notEqual(loaderStart, -1);
  assert.notEqual(loaderEnd, -1);
  const loader = source.slice(loaderStart, loaderEnd);
  assert.match(loader, /case 'ssh':[\s\S]*loadSessions\(\)/);
  assert.match(loader, /case 'rdp':[\s\S]*loadRDPSessions\(\)/);
  assert.match(loader, /case 'db':[\s\S]*loadDBConnections\(\)/);
  assert.match(loader, /case 'online':[\s\S]*loadOnlineSessions\(\)/);
  assert.match(loader, /case 'logins':[\s\S]*loadLoginAuditLogs\(\)/);
  assert.match(loader, /case 'operations':[\s\S]*loadOperationAuditLogs\(\)/);

  const mountedStart = source.indexOf('onMounted(');
  const mountedEnd = source.indexOf('watch(', mountedStart);
  const mounted = source.slice(mountedStart, mountedEnd);
  assert.match(mounted, /loadAuditScope\(auditScope\.value\)/);
  assert.doesNotMatch(source, /onlineRefreshTimer/);
  assert.doesNotMatch(source, /setInterval\([\s\S]{0,240}loadOnlineSessions/);
});
