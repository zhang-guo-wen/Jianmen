import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import {
  BYTES_PER_GIB,
  SYSTEM_SETTINGS_FIELDS,
  changedSystemSettingsFields,
  replayBytesToGiB,
  replayGiBToBytes,
  weakerProtectionReasons,
  type SystemSettingsValues,
} from './systemSettings.ts';

function settings(overrides: Partial<SystemSettingsValues> = {}): SystemSettingsValues {
  return {
    database_gateway_mode: 'unified',
    web_rdp_enabled: true,
    web_rdp_connect_timeout_seconds: 15,
    web_rdp_allow_unrecorded: false,
    recording_enabled: true,
    recording_record_input: false,
    recording_record_commands: true,
    recording_retention_days: 30,
    recording_max_replay_bytes: 10 * BYTES_PER_GIB,
    recording_cleanup_batch_size: 100,
    ...overrides,
  };
}

test('database gateway mode participates in global settings differences', () => {
  const current = settings();
  const next = settings({ database_gateway_mode: 'independent' });

  assert.equal(SYSTEM_SETTINGS_FIELDS.includes('database_gateway_mode'), true);
  assert.deepEqual(changedSystemSettingsFields(current, next), ['database_gateway_mode']);
  assert.deepEqual(changedSystemSettingsFields(next, next), []);
});

test('system settings presents unified database entry as the default with the MySQL delay notice', () => {
  const source = readFileSync(new URL('../views/SystemSettingsView.vue', import.meta.url), 'utf8');

  assert.match(source, /database_gateway_mode:\s*'unified'/);
  assert.match(source, /label="运行策略"/);
  assert.match(source, /统一入口（默认）/);
  assert.match(source, /独立端口/);
  assert.match(source, /统一入口的 MySQL 每次连接会增加约 200ms 建连时间/);
});

test('GiB conversion preserves normal configuration values', () => {
  assert.equal(replayBytesToGiB(10 * BYTES_PER_GIB), 10);
  assert.equal(replayGiBToBytes(1.5), 1.5 * BYTES_PER_GIB);
  assert.equal(replayGiBToBytes(Number.NaN), 0);
});

test('risk detection covers audit and unrecorded-session downgrades', () => {
  const current = settings();
  const next = settings({
    web_rdp_allow_unrecorded: true,
    recording_enabled: false,
    recording_record_input: true,
    recording_record_commands: false,
    recording_retention_days: 7,
    recording_max_replay_bytes: 5 * BYTES_PER_GIB,
  });

  assert.deepEqual(weakerProtectionReasons(current, next), [
    '允许录制失败时继续建立 Web RDP 会话',
    '关闭会话录制',
    '开启原始输入记录（可能包含敏感信息）',
    '关闭命令记录',
    '审计保留期从 30 天缩短为 7 天',
    '降低本地回放容量上限，可能触发更积极的旧录像清理',
  ]);
});

test('stronger or operational-only changes do not require risk confirmation', () => {
  const current = settings({
    web_rdp_enabled: false,
    web_rdp_connect_timeout_seconds: 10,
    recording_retention_days: 7,
    recording_max_replay_bytes: 5 * BYTES_PER_GIB,
  });
  const next = settings({
    web_rdp_enabled: true,
    web_rdp_connect_timeout_seconds: 30,
    recording_retention_days: 30,
    recording_max_replay_bytes: 10 * BYTES_PER_GIB,
    recording_cleanup_batch_size: 200,
  });

  assert.deepEqual(weakerProtectionReasons(current, next), []);
});

test('changing replay from unlimited to bounded requires confirmation', () => {
  const current = settings({ recording_max_replay_bytes: 0 });
  const next = settings({ recording_max_replay_bytes: BYTES_PER_GIB });

  assert.deepEqual(weakerProtectionReasons(current, next), [
    '降低本地回放容量上限，可能触发更积极的旧录像清理',
  ]);
});
