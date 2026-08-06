import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { sqlSchemaFromMetadata, type SQLTableMetadata } from './sqlSchema';

describe('sqlSchemaFromMetadata', () => {
  it('转换为 lang-sql self/children 格式', () => {
    const tables: SQLTableMetadata[] = [
      { name: 'users', columns: [{ name: 'id', type: 'bigint' }, { name: 'name', type: 'varchar' }] },
    ];
    const schema = sqlSchemaFromMetadata(tables);
    assert.equal(schema.users.self.label, 'users');
    assert.equal(schema.users.children[0].label, 'id');
    assert.equal(schema.users.children[0].detail, 'bigint');
  });

  it('空元数据返回空对象', () => {
    assert.deepEqual(sqlSchemaFromMetadata([]), {});
  });
});
