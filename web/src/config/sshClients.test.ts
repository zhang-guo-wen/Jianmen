import assert from 'node:assert/strict';
import test from 'node:test';

import {
  SETTINGS_CLIENT_PLATFORM_OPTIONS,
  SETTINGS_SSH_CLIENT_OPTIONS,
  buildSettingsSSHClientOptions,
  isSupportedSSHClientForActivation,
} from './sshClients.ts';

test('settings SSH options exclude system clients but keep the current legacy value visible', () => {
  assert.deepEqual(
    SETTINGS_SSH_CLIENT_OPTIONS.map(option => option.command),
    ['xshell', 'putty', 'securecrt', 'mobaxterm', 'winterm'],
  );

  assert.equal(isSupportedSSHClientForActivation('xshell'), true);
  assert.equal(isSupportedSSHClientForActivation('default'), false);
  assert.equal(isSupportedSSHClientForActivation('system'), false);

  const legacyOptions = buildSettingsSSHClientOptions('default');
  assert.equal(legacyOptions[0].command, 'default');
  assert.equal(legacyOptions[0].disabled, true);
  assert.match(legacyOptions[0].label, /系统默认 SSH 协议/);
});

test('settings platform options keep windows enabled and disable macOS/linux', () => {
  assert.deepEqual(SETTINGS_CLIENT_PLATFORM_OPTIONS, [
    { label: 'Windows', value: 'windows' },
    { label: 'macOS', value: 'macos', disabled: true },
    { label: 'Linux', value: 'linux', disabled: true },
  ]);
});
