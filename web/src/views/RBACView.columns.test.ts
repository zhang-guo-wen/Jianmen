import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const permissionFiles = [
  './RBACView.vue',
  './RolesView.vue',
  './UserGroupsView.vue',
  './ResourceGrantView.vue',
  '../components/AccountGroupsContent.vue',
  '../components/ResourceGroupsContent.vue',
  '../components/ResourceSelector.vue',
];

test('permission management consistently names descriptions as remarks', () => {
  for (const file of permissionFiles) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8');
    assert.doesNotMatch(source, /t\('common\.description'\)/, file);
  }
});

test('permission list remarks are the last data column before actions', () => {
  for (const file of [
    './RolesView.vue',
    './UserGroupsView.vue',
    '../components/AccountGroupsContent.vue',
    '../components/ResourceGroupsContent.vue',
  ]) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8');
    const remark = source.indexOf("t('common.remark')");
    const actions = source.indexOf("t('common.actions')", remark);
    assert.ok(remark >= 0, `${file}: missing remark column`);
    assert.ok(actions > remark, `${file}: remark must be the last data column before actions`);
    assert.doesNotMatch(
      source.slice(remark, actions),
      /<el-table-column[\s\S]*<el-table-column/,
      `${file}: unexpected data column after remark`,
    );
  }
});
