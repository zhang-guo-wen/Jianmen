import assert from 'node:assert/strict';
import { test } from 'node:test';

import type { TargetPayload } from '@/api/client';

import { buildConnectionTestPayload } from '@/utils/hostsConnectionTest';

function basePayload(): TargetPayload {
  return {
    id: '192-168-1-10-admin',
    host_id: 'host-1',
    name: 'admin',
    host: '192.168.1.10',
    port: 22,
    protocol: 'ssh',
    username: 'admin',
    password: 'secret',
    private_key_path: '',
    private_key_pem: '',
    passphrase: '',
    insecure_ignore_host_key: false,
    host_key_fingerprint: '',
    known_hosts_path: '',
    rdp_security: 'any',
    rdp_ignore_certificate: false,
    rdp_cert_fingerprints: '',
    rdp_clipboard_read: false,
    rdp_clipboard_write: false,
    rdp_file_upload: false,
    rdp_file_download: false,
    rdp_drive_mapping: false,
  };
}

test('新增账号（未保存）测试连接时不携带临时 ID，避免后端按 ID 查存储报 target not found', () => {
  const payload = basePayload();
  const result = buildConnectionTestPayload(payload, true);

  assert.equal(result.id, '');
  assert.equal(result.host_id, 'host-1');
  assert.equal(result.username, 'admin');
  assert.equal(result.password, 'secret');
});

test('编辑已有账号时保留原 ID（后端按存储目标加载并合并凭据）', () => {
  const payload = basePayload();
  const result = buildConnectionTestPayload(payload, false);

  assert.equal(result.id, '192-168-1-10-admin');
});

test('不修改传入的原始 payload', () => {
  const payload = basePayload();
  buildConnectionTestPayload(payload, true);

  assert.equal(payload.id, '192-168-1-10-admin');
});
