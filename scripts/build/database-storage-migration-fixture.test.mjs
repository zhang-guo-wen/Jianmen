import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const source = readFileSync('internal/integration/database_storage_migration_integration_test.go', 'utf8');

test('database storage migration fixture covers the latest storage migration versions', () => {
  assert.match(source, /"202607200001"/);
  assert.match(source, /"202607200002"/);
  assert.match(source, /"202607210001"/);
  assert.match(source, /"202607210002"/);
  assert.match(source, /"SSH host identity"/);
  assert.match(source, /"database gateway client TLS mode"/);
  assert.match(source, /"user preference local client fields"/);
  assert.match(source, /"remove RDP access approval"/);
});
