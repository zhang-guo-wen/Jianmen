import assert from 'node:assert/strict';
import test from 'node:test';

import { buildSSHDeepLink } from './connectionLinks.ts';

test('SSH deep links embed the temporary password for local clients', () => {
  const link = buildSSHDeepLink({
    username: 'ops@example',
    password: 'temporary:/@ password',
    host: 'gateway.example',
    port: 47102,
  });
  assert.equal(link, 'ssh://ops%40example:temporary%3A%2F%40%20password@gateway.example:47102');
});

test('SSH deep links remain valid when no password is provided', () => {
  const link = buildSSHDeepLink({ username: 'ops@example', host: '2001:db8::1', port: 47102 });
  assert.equal(link, 'ssh://ops%40example@[2001:db8::1]:47102');
});
