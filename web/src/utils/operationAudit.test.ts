import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import type { TranslationKey } from '@/i18n';
import {
  channelLabel,
  familyOf,
  operationLabel,
  operationTargetLabel,
  parseOperationFilter,
} from './operationAudit';

const i18nSource = readFileSync(new URL('../i18n/index.ts', import.meta.url), 'utf8');

/** 只验结构（选了哪些键），文案本身由下面的键存在性断言兜底。 */
const labels: Record<string, string> = {
  'audit.action.create': '新增',
  'audit.action.update': '修改',
  'audit.action.delete': '删除',
  'audit.operation.issue_credential': '签发临时连接密码',
  'audit.operation.uncategorized': '其他操作',
  'audit.family.host': '主机',
  'audit.family.host_account': '主机账号',
  'audit.family.ai_token': 'AI 令牌',
  'audit.channel.aiApi': 'AI API',
  'audit.channel.adminConsole': '管理后台',
  'audit.channel.unknown': '未知来源',
  'common.none': '无',
};
const t = ((key: string) => labels[key] ?? key) as (key: TranslationKey) => string;

test('business actions are translated and generic verbs stay readable', () => {
  assert.equal(operationLabel({ operation: 'issue_credential', action: 'create' }, t), '签发临时连接密码');
  assert.equal(operationLabel({ operation: 'create:host', action: 'create' }, t), '新增主机');
  // 中文动词接拉丁字母字段要留空格
  assert.equal(operationLabel({ operation: 'create:ai_token', action: 'create' }, t), '新增 AI 令牌');
  assert.equal(operationLabel({ operation: '', action: 'update' }, t), '修改');
  // 未知键不能显示成英文键名
  assert.equal(operationLabel({ operation: 'legacy_flow', action: 'create' }, t), '其他操作');
});

test('operation target prefers the readable label and never shows the request path', () => {
  assert.equal(
    operationTargetLabel({
      operation: 'issue_credential',
      target_label: '47.107.41.233:22 · root',
      resource_type: 'host_account',
      resource_id: 'acc-1',
      resource_name: '/api/ai/resources/host_account/acc-1/credentials',
    }, t),
    '47.107.41.233:22 · root',
  );
  assert.equal(
    operationTargetLabel({
      operation: 'issue_credential',
      resource_type: 'host_account',
      resource_id: '47-107-41-233-22-root',
      resource_name: '/api/ai/resources/host_account/47-107-41-233-22-root/credentials',
    }, t),
    '主机账号 · 47-107-41-23…',
  );
  assert.equal(
    operationTargetLabel({ operation: 'update:host', resource_type: 'hosts', resource_id: 'h1' }, t),
    '主机 · h1',
  );
  assert.equal(
    operationTargetLabel({ operation: '', resource_type: 'unknown-thing', resource_id: '' }, t),
    '无',
  );
});

test('family falls back to the stored resource type for historical rows', () => {
  assert.equal(familyOf({ operation: 'update:host', resource_type: 'hosts' }), 'host');
  assert.equal(familyOf({ operation: '', resource_type: 'db/accounts' }), 'database_account');
  assert.equal(familyOf({ operation: '', resource_type: 'nothing' }), '');
});

test('channel labels separate AI traffic from the admin console', () => {
  assert.equal(channelLabel('ai_api', t), 'AI API');
  assert.equal(channelLabel('admin_console', t), '管理后台');
  assert.equal(channelLabel(undefined, t), '未知来源');
  assert.equal(channelLabel('something-else', t), '未知来源');
});

test('operation filter values separate generic verbs from fixed actions', () => {
  assert.deepEqual(parseOperationFilter('verb:create'), {verb: 'create'});
  assert.deepEqual(parseOperationFilter('op:issue_credential'), {operation: 'issue_credential'});
  assert.deepEqual(parseOperationFilter(''), {});
  assert.deepEqual(parseOperationFilter('verb:'), {});
  assert.deepEqual(parseOperationFilter('nonsense:value'), {});
});

test('every operation and family key used by the UI exists in i18n', () => {  const source = readFileSync(new URL('./operationAudit.ts', import.meta.url), 'utf8');
  const keys = [...source.matchAll(/'(audit\.(?:operation|family|action|channel)\.[a-zA-Z_]+)'/g)]
    .map(match => match[1]);
  assert.ok(keys.length >= 40, `expected many referenced keys, got ${keys.length}`);
  for (const key of new Set(keys)) {
    assert.match(i18nSource, new RegExp(`'${key.split('.').join('\\.')}':`), `missing i18n key ${key}`);
  }
});
