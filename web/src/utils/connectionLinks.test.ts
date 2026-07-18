import assert from 'node:assert/strict';
import test from 'node:test';

import { buildSSHDeepLink } from './connectionLinks.ts';

test('SSH deep links never embed a password', () => {
  const link = buildSSHDeepLink({ username: 'ops@example', host: 'gateway.example', port: 47102 });
  assert.equal(link, 'ssh://ops%40example@gateway.example:47102');
  assert.doesNotMatch(link, /ssh:\/\/[^/]*:[^/@]+@/);
});
