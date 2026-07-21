import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const releaseDoc = readFileSync('docs/guides/release.md', 'utf8');

test('release guide documents lite/rdp tags and latest semantics', () => {
  assert.match(releaseDoc, /Lite：默认镜像|默认示例使用\s*Lite/);
  assert.match(releaseDoc, /只有\s*Lite\s*会更新\s*`latest`/);
  assert.match(releaseDoc, /vX\.Y\.Z-rdp|rdp 镜像/);
  assert.match(releaseDoc, /vX\.Y\.Z.*等价于.*lite|无后缀.*lite/i);
});
