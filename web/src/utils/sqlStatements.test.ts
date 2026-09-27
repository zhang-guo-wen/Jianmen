import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { parseSQLStatements, statementAtPosition } from './sqlStatements';

describe('parseSQLStatements', () => {
  it('空文档返回空数组', () => {
    assert.deepEqual(parseSQLStatements(''), []);
  });

  it('单条语句', () => {
    const [stmt] = parseSQLStatements('SELECT * FROM users;');
    assert.equal(stmt.from, 0);
    assert.equal(stmt.to, 20);
    assert.equal(stmt.text, 'SELECT * FROM users;');
  });

  it('多条语句精确切分', () => {
    const stmts = parseSQLStatements('UPDATE t SET a=1; SELECT 2;');
    assert.equal(stmts.length, 2);
    assert.equal(stmts[0].from, 0);
    assert.equal(stmts[0].to, 17);
    assert.equal(stmts[1].from, 18);
    assert.equal(stmts[1].to, 27);
  });

  it('注释不进入语句范围', () => {
    const stmts = parseSQLStatements('-- 注释\nSELECT 1;');
    assert.equal(stmts.length, 1);
    assert.equal(stmts[0].from, 6);
    assert.equal(stmts[0].text, 'SELECT 1;');
  });

  it('字符串内的分号不误切', () => {
    const stmts = parseSQLStatements("SELECT 'abc;def' FROM t;");
    assert.equal(stmts.length, 1);
    assert.equal(stmts[0].to, 24);
  });

  it('未闭合语句覆盖到文档末尾', () => {
    const stmts = parseSQLStatements('SELECT * FROM users');
    assert.equal(stmts.length, 1);
    assert.equal(stmts[0].to, 19);
  });

  it('startLine / endLine 从 0 计数', () => {
    const stmts = parseSQLStatements('-- a\nSELECT 1;\n-- b\nSELECT 2;');
    assert.equal(stmts.length, 2);
    assert.equal(stmts[0].startLine, 1);
    assert.equal(stmts[0].endLine, 1);
    assert.equal(stmts[1].startLine, 3);
    assert.equal(stmts[1].endLine, 3);
  });
});

describe('statementAtPosition', () => {
  const stmts = parseSQLStatements('SELECT 1;\nSELECT 2;');

  it('命中语句范围', () => {
    assert.equal(statementAtPosition(stmts, 0)?.from, 0);
    assert.equal(statementAtPosition(stmts, 11)?.from, 10);
  });

  it('语句内部及边界(含 to)命中语句', () => {
    // 位置 8 是语句 1 的分号,位于 [0,9] 区间内,应命中语句 1;
    // 位置 9 是两条语句之间的换行,按含 to 的区间语义仍命中语句 1
    assert.equal(statementAtPosition(stmts, 8)?.from, 0);
    assert.equal(statementAtPosition(stmts, 9)?.from, 0);
  });

  it('注释/空白位置返回 null', () => {
    // 语句之间的空行(位置 10 落在 [0,9] 与 [11,20] 之外)
    const withGap = parseSQLStatements('SELECT 1;\n\nSELECT 2;');
    assert.equal(statementAtPosition(withGap, 10), null);
    // 行注释处
    const withComment = parseSQLStatements('-- 注释\nSELECT 1;');
    assert.equal(statementAtPosition(withComment, 0), null);
  });

  it('空语句列表返回 null', () => {
    assert.equal(statementAtPosition([], 0), null);
  });
});
