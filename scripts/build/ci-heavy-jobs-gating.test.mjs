import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const source = readFileSync('.github/workflows/ci.yml', 'utf8');

test('CI workflow exposes manual trigger for heavy Docker jobs', () => {
  assert.match(source, /workflow_dispatch:/);
});

test('integration and container jobs are gated to main pushes or manual dispatch', () => {
  assert.match(source, /integration:[\s\S]*if:\s*\$\{\{[\s\S]*github\.event_name == 'workflow_dispatch'[\s\S]*github\.ref == 'refs\/heads\/main'/);
  assert.match(source, /container:[\s\S]*if:\s*\$\{\{[\s\S]*github\.event_name == 'workflow_dispatch'[\s\S]*github\.ref == 'refs\/heads\/main'/);
});
