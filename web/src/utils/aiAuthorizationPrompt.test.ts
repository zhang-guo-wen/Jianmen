import assert from 'node:assert/strict';
import test from 'node:test';

import type { IssuedAIAccessToken } from '../api/client';
import { buildAIAuthorizationPrompt } from './aiAuthorizationPrompt.ts';

function issuedToken(overrides: Partial<IssuedAIAccessToken> = {}): IssuedAIAccessToken {
  return {
    id: 'token-1',
    name: 'AI client',
    access_token: 'access-secret',
    refresh_token: 'refresh-secret',
    access_expires_at: '2030-01-01T00:00:00Z',
    refresh_expires_at: '2030-01-30T00:00:00Z',
    created_at: '2029-12-31T00:00:00Z',
    ...overrides,
  };
}

test('AI authorization prompt fills issued tokens and keeps the absolute documentation URL', () => {
  const prompt = buildAIAuthorizationPrompt(issuedToken({
    copy_prompt: [
      '\u4f60\u53ef\u4ee5\u4f7f\u7528\u6211\u7684\u6743\u9650\u8bbf\u95ee\u6211\u7684\u670d\u52a1\u5668\u3001\u6570\u636e\u5e93\u7b49\u8d44\u6e90\uff0c',
      '\u8bbf\u95ee\u4ee4\u724c\uff1a<access_token>',
      '\u5237\u65b0\u4ee4\u724c\uff1a<refresh_token>',
      '\u5177\u4f53\u89c1\u6587\u6863\uff1a[https://bastion.example/api/ai/docs](https://bastion.example/api/ai/docs)',
    ].join('\n'),
    docs_url: 'https://bastion.example/api/ai/docs',
    docs_content: '# Jianmen AI Bastion API\nHidden documentation body',
  }));

  assert.match(prompt, /\u8bbf\u95ee\u4ee4\u724c\uff1aaccess-secret/);
  assert.match(prompt, /\u5237\u65b0\u4ee4\u724c\uff1arefresh-secret/);
  assert.match(prompt, /https:\/\/bastion\.example\/api\/ai\/docs/);
  assert.doesNotMatch(prompt, /<access_token>|<refresh_token>|Hidden documentation body/);
});

test('AI authorization prompt appends required credentials and URL to an incomplete template', () => {
  const prompt = buildAIAuthorizationPrompt(issuedToken({
    copy_prompt: '\u4f7f\u7528\u5821\u5792\u673a AI \u6388\u6743\u8bbf\u95ee\u8d44\u6e90\u3002',
    docs_url: 'https://bastion.example/api/ai/docs',
  }));

  assert.equal(prompt, [
    '\u4f7f\u7528\u5821\u5792\u673a AI \u6388\u6743\u8bbf\u95ee\u8d44\u6e90\u3002',
    '\u8bbf\u95ee\u4ee4\u724c\uff1aaccess-secret',
    '\u5237\u65b0\u4ee4\u724c\uff1arefresh-secret',
    '\u5177\u4f53\u89c1\u6587\u6863\uff1ahttps://bastion.example/api/ai/docs',
  ].join('\n'));
});
