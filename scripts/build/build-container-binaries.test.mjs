import assert from 'node:assert/strict';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { readFileSync } from 'node:fs';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');

test('container build script emits both lite and rdp linux binaries', () => {
  const source = readFileSync(path.join(root, 'scripts/build/build-container-binaries.sh'), 'utf8');
  assert.match(source, /jianmen-linux-\$\{arch\}-lite|jianmen-linux-\$arch-lite/);
  assert.match(source, /jianmen-linux-\$\{arch\}-rdp|jianmen-linux-\$arch-rdp/);
  assert.match(source, /prepare-guacd-runtime\.sh/);
  assert.match(source, /embedded_guacd/);
});

test('lite and rdp Dockerfiles copy the matching binary and config', () => {
  const lite = readFileSync(path.join(root, 'deploy/docker/Dockerfile.lite'), 'utf8');
  const rdp = readFileSync(path.join(root, 'deploy/docker/Dockerfile.rdp'), 'utf8');

  assert.match(lite, /COPY .*jianmen-linux-\$\{TARGETARCH\}-lite/);
  assert.match(lite, /config\.docker\.json/);
  assert.doesNotMatch(lite, /config\.docker\.web-rdp\.example\.json/);
  assert.match(lite, /-disable-web-rdp/);

  assert.match(rdp, /COPY .*jianmen-linux-\$\{TARGETARCH\}-rdp/);
  assert.match(rdp, /config\.docker\.web-rdp\.example\.json/);
  assert.doesNotMatch(rdp, /-disable-web-rdp/);
});
