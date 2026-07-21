import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const sourceRoot = dirname(dirname(fileURLToPath(import.meta.url)));

function collectVueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return collectVueFiles(path);
    return entry.isFile() && entry.name.endsWith('.vue') ? [path] : [];
  });
}

function source(relativePath: string): string {
  return readFileSync(join(sourceRoot, relativePath), 'utf8');
}

function dialogOpeningTag(componentSource: string, marker: string): string {
  const modelIndex = componentSource.indexOf(marker);
  assert.notEqual(modelIndex, -1, `missing dialog marker ${marker}`);

  const start = componentSource.lastIndexOf('<el-dialog', modelIndex);
  const end = componentSource.indexOf('>', modelIndex);
  assert.notEqual(start, -1, `missing opening dialog for ${marker}`);
  assert.notEqual(end, -1, `missing closing bracket for ${marker}`);
  return componentSource.slice(start, end + 1);
}

test('shared form dialog owns width and uses the dialog body as the only scroller', () => {
  const componentSource = source('components/FormDialog.vue');
  const globalStyles = source('styles/main.css');

  assert.match(componentSource, /class="form-dialog crud-form-dialog"/);
  assert.doesNotMatch(componentSource, /width\?:\s*string/);
  assert.doesNotMatch(componentSource, /dialogWidth/);
  assert.doesNotMatch(componentSource, /overflow-y:\s*auto/);
  assert.doesNotMatch(componentSource, /max-height:/);

  assert.match(globalStyles, /--form-dialog-width:\s*640px/);
  assert.match(globalStyles, /\.el-overlay-dialog\s*\{[\s\S]*?box-sizing:\s*border-box;[\s\S]*?overflow:\s*hidden;/);
  assert.match(globalStyles, /\.el-dialog__body\s*\{[\s\S]*?flex:\s*1 1 auto;[\s\S]*?overflow-x:\s*hidden;[\s\S]*?overflow-y:\s*auto;/);
  assert.match(globalStyles, /\.crud-form-dialog\s*\{[\s\S]*?width:\s*min\(var\(--form-dialog-width\), 100%\)\s*!important;/);
  assert.match(globalStyles, /\.crud-form-dialog \.form-grid\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\)\s*!important;/);
});

test('FormDialog callers cannot override width and form fields stay vertical', () => {
  for (const file of collectVueFiles(sourceRoot)) {
    const componentSource = readFileSync(file, 'utf8');
    const blocks = componentSource.match(/<FormDialog\b[\s\S]*?<\/FormDialog>/g) ?? [];

    for (const block of blocks) {
      const openingTag = block.slice(0, block.indexOf('>') + 1);
      assert.doesNotMatch(openingTag, /\bwidth=/, `${file} overrides the shared dialog width`);
      if (block.includes('<el-form')) {
        assert.match(
          block,
          /<el-form\b[^>]*\blabel-position="top"/,
          `${file} contains a non-vertical FormDialog form`,
        );
      }
    }
  }
});

test('direct mutation dialogs opt into the shared form-dialog shell', () => {
  const dialogs = [
    { file: 'components/AccountGroupsContent.vue', model: 'dialogVisible', topForm: true },
    { file: 'components/BatchCreateUsersDialog.vue', model: 'visible', marker: ':model-value="visible"', topForm: false },
    { file: 'components/ResourceGroupsContent.vue', model: 'dialogVisible', topForm: true },
    { file: 'views/ContainersView.vue', model: 'quickAccountVisible', topForm: true },
    { file: 'components/database/DatabaseAutoProvisionDialog.vue', model: 'visible', topForm: true },
    { file: 'views/ResourceGrantView.vue', model: 'grantDialogVisible', topForm: true },
    { file: 'views/TemporaryAccountsView.vue', model: 'temporaryDialogVisible', topForm: true },
    { file: 'views/TemporaryAccountsView.vue', model: 'aiDialogVisible', topForm: true },
    { file: 'views/TemporaryAccountsView.vue', model: 'extendDialogVisible', topForm: false },
    { file: 'views/UserGroupsView.vue', model: 'groupDialogVisible', topForm: true },
    { file: 'views/UserGroupsView.vue', model: 'membersDialogVisible', topForm: false },
    { file: 'views/UsersView.vue', model: 'roleDialogVisible', topForm: false },
  ];

  for (const dialog of dialogs) {
    const componentSource = source(dialog.file);
    const openingTag = dialogOpeningTag(componentSource, dialog.marker ?? `v-model="${dialog.model}"`);
    assert.match(openingTag, /\bclass="[^"]*\bcrud-form-dialog\b[^"]*"/, `${dialog.file}:${dialog.model} is not standardized`);
    assert.doesNotMatch(openingTag, /\bwidth=/, `${dialog.file}:${dialog.model} overrides the shared width`);

    if (dialog.topForm) {
      const dialogEnd = componentSource.indexOf('</el-dialog>', componentSource.indexOf(openingTag));
      const dialogBlock = componentSource.slice(componentSource.indexOf(openingTag), dialogEnd);
      assert.match(
        dialogBlock,
        /<el-form\b[^>]*\blabel-position="top"/,
        `${dialog.file}:${dialog.model} is not a vertical form`,
      );
    }
  }
});

test('host and database account creation forms keep expiry controls concise', () => {
  const hostsSource = source('views/HostsView.vue');
  const databaseSource = source('views/DatabaseView.vue');
  const databaseAccountFormSource = source('components/database/DatabaseAccountFormDialog.vue');

  assert.doesNotMatch(
    hostsSource,
    /SSH 主机密钥会在新增和重新启用时自动获取并校验/,
  );
  assert.doesNotMatch(hostsSource, /class="expiry-text"/);
  assert.doesNotMatch(hostsSource, /\.expiry-text\s*\{/);

  assert.match(databaseAccountFormSource, /type="datetime"/);
  assert.match(databaseAccountFormSource, />\s*永久\s*</);
  assert.match(databaseAccountFormSource, /8小时|7天|1年/);
  assert.match(databaseSource, /createDBAccount\([\s\S]*?expires_at:\s*accountForm\.expiresAt\?\.toISOString\(\) \?\? null/);
  assert.match(databaseSource, /updateDBAccount\([\s\S]*?expires_at:\s*accountForm\.expiresAt\?\.toISOString\(\) \?\? null/);
  assert.match(databaseSource, /toggleAccountStatus\([\s\S]*?expires_at:\s*account\.expires_at \|\| null/);
});

test('database account form uses login language without saved-credential hints', () => {
  const componentSource = source('components/database/DatabaseAccountFormDialog.vue');
  const databaseSource = source('views/DatabaseView.vue');

  assert.match(componentSource, /label="登录账号"/);
  assert.match(componentSource, /label="登录密码"/);
  assert.doesNotMatch(componentSource, /目标用户名|目标密码/);
  assert.doesNotMatch(componentSource, /点击测试连接时必须重新输入数据库密码/);
  assert.doesNotMatch(componentSource, /已保存凭据/);
  assert.doesNotMatch(databaseSource, /savedCredentialTestRequests|testSavedAccountConnection/);
});

test('database auto provisioning stays in one dialog and loads a scrollable database list from the selected credential', () => {
  const componentSource = source('components/database/DatabaseAutoProvisionDialog.vue');

  assert.equal((componentSource.match(/<el-dialog\b/g) ?? []).length, 1);
  assert.match(componentSource, /v-model="selectedAdminAccountId"/);
  assert.match(componentSource, /watch\(selectedAdminAccountId/);
  assert.match(componentSource, /loadDatabases\(instanceID, accountID\)/);
  assert.match(componentSource, /max-height="340"/);
  assert.doesNotMatch(componentSource, /<el-form-item label="主机"/);
  assert.doesNotMatch(componentSource, /下一步|上一步|provisionStep/);
  assert.doesNotMatch(componentSource, /\bhost\s*:/);
});

test('host and database management toolbars expose top-level refresh actions', () => {
  const hostsSource = source('views/HostsView.vue');
  const databaseSource = source('views/DatabaseView.vue');

  assert.match(hostsSource, /:loading="hostsLoading" :icon="Refresh" @click="fetchHosts"/);
  assert.match(databaseSource, /:loading="instancesLoading" :icon="Refresh" @click="loadInstances"/);
});

test('RDP account advanced fields live under more settings', () => {
  const hostsSource = source('views/HostsView.vue');
  const dialogStart = hostsSource.indexOf(':title="editingAccountId ? \'编辑账号\' : \'新增账号\'"');
  const dialogEnd = hostsSource.indexOf('</FormDialog>', dialogStart);
  const dialogSource = hostsSource.slice(dialogStart, dialogEnd);

  const moreSettingsStart = dialogSource.indexOf('title="更多设置"');
  assert.ok(moreSettingsStart >= 0, 'missing 更多设置 collapse');

  const advancedBlock = dialogSource.slice(moreSettingsStart);
  assert.match(advancedBlock, /<el-form-item label="Windows 域">/);
  assert.match(advancedBlock, /<el-form-item label="安全模式">/);
  assert.match(advancedBlock, /<el-form-item label="忽略证书">/);
  assert.match(advancedBlock, /<el-form-item v-if="!accountForm\.rdp_ignore_certificate" label="证书指纹">/);
  assert.match(advancedBlock, /<el-form-item label="通道权限">/);
});

test('new RDP accounts default to collapsed advanced settings and permissive toggles', () => {
  const hostsSource = source('views/HostsView.vue');

  assert.match(hostsSource, /async function openCreateAccountDialog\(host: HostView\) \{[\s\S]*?accountMorePanels\.value = \[\]/);
  assert.match(hostsSource, /function emptyAccountForm\(\): AccountForm \{[\s\S]*?rdp_ignore_certificate: true,/);
  assert.match(hostsSource, /function emptyAccountForm\(\): AccountForm \{[\s\S]*?rdp_clipboard_read: true,/);
  assert.match(hostsSource, /function emptyAccountForm\(\): AccountForm \{[\s\S]*?rdp_clipboard_write: true,/);
  assert.match(hostsSource, /function emptyAccountForm\(\): AccountForm \{[\s\S]*?rdp_file_upload: true,/);
  assert.match(hostsSource, /function emptyAccountForm\(\): AccountForm \{[\s\S]*?rdp_file_download: true,/);
  assert.match(hostsSource, /function emptyAccountForm\(\): AccountForm \{[\s\S]*?rdp_drive_mapping: true,/);
});

test('database instance name is optional, advanced, and defaults to its address', () => {
  const databaseSource = source('views/DatabaseView.vue');
  const dialogStart = databaseSource.indexOf(':title="editingInstance');
  const dialogEnd = databaseSource.indexOf('</FormDialog>', dialogStart);
  const dialogSource = databaseSource.slice(dialogStart, dialogEnd);

  const moreSettingsStart = dialogSource.indexOf('title="更多设置"');
  const nameFieldStart = dialogSource.indexOf('<el-form-item label="名称">');
  assert.ok(moreSettingsStart >= 0 && nameFieldStart > moreSettingsStart);
  assert.doesNotMatch(dialogSource, /<el-form-item label="名称" required>/);
  assert.match(dialogSource, /placeholder="默认 = 上游地址"/);
  assert.match(databaseSource, /const instanceNameTouched = ref\(false\)/);
  assert.match(databaseSource, /function syncDefaultInstanceName\(\)/);
  assert.match(databaseSource, /name:\s*instanceForm\.name\.trim\(\) \|\| defaultInstanceName\(\)/);
});
