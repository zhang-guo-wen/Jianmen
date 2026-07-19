import assert from 'node:assert/strict';
import test from 'node:test';

import {
  BYTES_PER_GIB,
  BYTES_PER_MIB,
  DATABASE_MAX_CLIENT_MESSAGE_BYTES_DEFAULT,
  clientMessageBytesToMiB,
  clientMessageMiBToBytes,
  formatClientMessageBytes,
  replayBytesToGiB,
  replayGiBToBytes,
  weakerProtectionReasons,
  type SystemSettingsValues,
} from './systemSettings.ts';

function settings(overrides: Partial<SystemSettingsValues> = {}): SystemSettingsValues {
  return {
    web_rdp_enabled: true,
    web_rdp_connect_timeout_seconds: 15,
    web_rdp_allow_unrecorded: false,
    database_max_client_message_bytes: DATABASE_MAX_CLIENT_MESSAGE_BYTES_DEFAULT,
    recording_enabled: true,
    recording_record_input: false,
    recording_record_commands: true,
    recording_retention_days: 30,
    recording_max_replay_bytes: 10 * BYTES_PER_GIB,
    recording_cleanup_batch_size: 100,
    ...overrides,
  };
}

test('GiB conversion preserves normal configuration values', () => {
  assert.equal(replayBytesToGiB(10 * BYTES_PER_GIB), 10);
  assert.equal(replayGiBToBytes(1.5), 1.5 * BYTES_PER_GIB);
  assert.equal(replayGiBToBytes(Number.NaN), 0);
});

test('MiB conversion preserves database and Redis client message limits', () => {
  assert.equal(DATABASE_MAX_CLIENT_MESSAGE_BYTES_DEFAULT, 10 * BYTES_PER_MIB);
  assert.equal(clientMessageBytesToMiB(10 * BYTES_PER_MIB), 10);
  assert.equal(clientMessageBytesToMiB(64 * 1024), 0.0625);
  assert.equal(clientMessageMiBToBytes(10), 10 * BYTES_PER_MIB);
  assert.equal(clientMessageMiBToBytes(0.0625), 64 * 1024);
  assert.equal(clientMessageMiBToBytes(Number.NaN), 0);
  assert.equal(formatClientMessageBytes(10 * BYTES_PER_MIB), '10 MiB');
  assert.equal(
    formatClientMessageBytes(10 * BYTES_PER_MIB + 1),
    '10 MiB（10485761 字节）',
  );
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

test('changing the database and Redis client message limit requires confirmation', () => {
  const current = settings();
  const next = settings({ database_max_client_message_bytes: 16 * BYTES_PER_MIB });

  assert.deepEqual(weakerProtectionReasons(current, next), [
    '数据库与 Redis 客户端报文上限从 10 MiB 调整为 16 MiB',
  ]);
});
