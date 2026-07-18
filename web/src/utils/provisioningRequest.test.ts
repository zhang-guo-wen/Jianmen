import assert from 'node:assert/strict';
import test from 'node:test';

import {
  createProvisionIdempotencySession,
  normalizeProvisionRequest,
  withIdempotencyKey,
} from './provisioningRequest.ts';

const request = {
  admin_account_id: 'admin-1',
  host: '10.0.0.8',
  grants: [
    { database: 'app', privilege: 'SELECT' },
    { database: 'audit', privilege: 'UPDATE' },
  ],
};

test('normalizes equivalent provisioning requests deterministically', () => {
  assert.equal(
    normalizeProvisionRequest({ ...request, grants: [...request.grants].reverse() }),
    normalizeProvisionRequest(request),
  );
});

test('reuses an idempotency key after failure and rotates after success or request change', () => {
  const session = createProvisionIdempotencySession(() => 'key-1', () => 'key-2', () => 'key-3', () => 'key-4', () => 'key-5');
  const first = session.keyFor(request);
  assert.equal(first, 'key-1');
  assert.equal(session.keyFor(request), first);

  session.markFailed();
  assert.equal(session.keyFor(request), first);

  session.markSucceeded();
  assert.equal(session.keyFor(request), 'key-2');

  assert.equal(session.keyFor({ ...request, host: '10.0.0.9' }), 'key-3');
  session.reset();
  assert.equal(session.keyFor(request, 'instance-a'), 'key-4');
  assert.equal(session.keyFor(request, 'instance-b'), 'key-5');
});

test('adds Idempotency-Key without putting secrets in URL or body', () => {
  const init = withIdempotencyKey({ method: 'POST', body: JSON.stringify(request) }, 'key-abc');
  const headers = new Headers(init.headers);
  assert.equal(headers.get('Idempotency-Key'), 'key-abc');
  assert.equal(init.body, JSON.stringify(request));
  assert.doesNotMatch('/api/db/provision-account', /password@|ssh:\/\/[^/]*:[^/@]+@/);
});
