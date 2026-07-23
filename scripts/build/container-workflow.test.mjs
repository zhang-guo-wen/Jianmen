import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const source = readFileSync('.github/workflows/container.yml', 'utf8');
const dockerignore = readFileSync('.dockerignore', 'utf8');

test('quality gate builds both lite and rdp Dockerfiles', () => {
  assert.match(source, /Dockerfile\.lite/);
  assert.match(source, /Dockerfile\.rdp/);
});

test('publish workflow emits lite and rdp tag families', () => {
  assert.match(source, /value=latest/);
  assert.match(source, /pattern=\{\{version\}\}-lite|suffix=-lite/);
  assert.match(source, /pattern=\{\{version\}\}-rdp|suffix=-rdp/);
});

test('Docker build context includes every generated container binary', () => {
  for (const arch of ['amd64', 'arm64']) {
    for (const variant of ['lite', 'rdp']) {
      assert.match(
        dockerignore,
        new RegExp(`^!dist/jianmen-linux-${arch}-${variant}$`, 'm'),
      );
    }
  }
});
