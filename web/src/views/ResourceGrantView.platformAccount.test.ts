import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const source = readFileSync(new URL('./ResourceGrantView.vue', import.meta.url), 'utf8');

test('资源授权对话框提供平台账号资源页签', () => {
  assert.match(source, /<el-tab-pane[^>]*name="platform_account"/);
  assert.ok(source.includes("t('resourceGrant.platformAccounts')"), '页签文案应使用 resourceGrant.platformAccounts');
});

test('平台账号页签展示账号名称、平台、登录账号与平台地址', () => {
  assert.match(source, /resourceTabType === 'platform_account'/);
  for (const key of [
    'platformAccounts.column.name',
    'platformAccounts.column.platform',
    'platformAccounts.column.username',
    'platformAccounts.column.url',
  ]) {
    assert.ok(source.includes(key), `平台账号资源列表缺少列：${key}`);
  }
});

test('平台账号资源通过平台账号接口分页加载并支持搜索', () => {
  assert.match(source, /getPlatformAccounts\(params\)/);
  assert.match(source, /resourceTabType\.value === 'platform_account'/);
});
