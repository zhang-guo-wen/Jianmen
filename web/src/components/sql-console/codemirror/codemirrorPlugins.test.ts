import assert from 'node:assert/strict';
import { EditorState } from '@codemirror/state';
import { describe, it } from 'vitest';

import { parseSQLStatements } from '@/utils/sqlStatements';

import {
  executingStatement,
  executionGutter,
  gutterMarkersFor,
  setExecutingStatement,
} from './executionGutter';
import {
  decorationsForStatements,
  statementDecoField,
  statementDecorator,
} from './statementDecorator';

const DOC = 'SELECT 1;\nSELECT 2;';

describe('executingStatement 状态字段', () => {
  it('默认值为 null(无执行中语句)', () => {
    const state = EditorState.create({ extensions: [executingStatement] });
    assert.equal(state.field(executingStatement), null);
  });

  it('分发 setExecutingStatement effect 后记录执行中的语句 from', () => {
    const state = EditorState.create({ extensions: [executingStatement] });
    const next = state.update({ effects: setExecutingStatement.of(42) }).state;
    assert.equal(next.field(executingStatement), 42);
  });

  it('分发 null 清除执行中的语句', () => {
    const state = EditorState.create({ extensions: [executingStatement] });
    const next = state.update({ effects: setExecutingStatement.of(null) }).state;
    assert.equal(next.field(executingStatement), null);
  });
});

describe('gutterMarkersFor 标记数据(纯函数)', () => {
  const statements = parseSQLStatements(DOC);

  it('每条语句生成一个标记,from 为语句起始偏移', () => {
    assert.deepEqual(
      gutterMarkersFor(statements, null).map((marker) => marker.from),
      [0, 10],
    );
  });

  it('executingFrom 匹配的语句标记为执行中', () => {
    assert.deepEqual(gutterMarkersFor(statements, 10), [
      { from: 0, executing: false },
      { from: 10, executing: true },
    ]);
  });

  it('executingFrom 为 null 或未匹配时全部非执行中', () => {
    assert.ok(gutterMarkersFor(statements, null).every((marker) => !marker.executing));
    assert.ok(gutterMarkersFor(statements, 12345).every((marker) => !marker.executing));
  });

  it('无语句时返回空数组', () => {
    assert.deepEqual(gutterMarkersFor([], null), []);
  });
});

describe('decorationsForStatements 虚线框装饰(纯函数)', () => {
  const statements = parseSQLStatements(DOC);

  it('单行语句生成一条装饰,同时带首行与末行类名', () => {
    assert.deepEqual(decorationsForStatements(statements, 1, DOC), [
      { from: 0, to: 9, className: 'sql-stmt-line sql-stmt-line--first sql-stmt-line--last' },
    ]);
  });

  it('多行语句按行生成装饰,首行/中间行/末行类名区分', () => {
    const multi = 'SELECT 1,\n  2\nFROM t;';
    const stmts = parseSQLStatements(multi);
    assert.deepEqual(decorationsForStatements(stmts, 5, multi), [
      { from: 0, to: 9, className: 'sql-stmt-line sql-stmt-line--first' },
      { from: 10, to: 13, className: 'sql-stmt-line' },
      { from: 14, to: 21, className: 'sql-stmt-line sql-stmt-line--last' },
    ]);
  });

  it('光标在语句外(空白行)时返回空数组', () => {
    // 语句 1 结束于偏移 9,语句 2 起始于偏移 11,偏移 10 位于两语句之间。
    const withGap = 'SELECT 1;\n\nSELECT 2;';
    const stmts = parseSQLStatements(withGap);
    assert.deepEqual(decorationsForStatements(stmts, 10, withGap), []);
  });

  it('无语句时返回空数组', () => {
    assert.deepEqual(decorationsForStatements([], 0, '  '), []);
  });

  it('光标在不同语句上时装饰集合不同', () => {
    const first = decorationsForStatements(statements, 1, DOC);
    const second = decorationsForStatements(statements, 12, DOC);
    assert.notDeepEqual(first, second);
  });
});

describe('statementDecoField 状态层集成', () => {
  it('默认光标在首条语句,装饰非空且作用于首行', () => {
    const state = EditorState.create({ doc: DOC, extensions: [statementDecorator()] });
    const deco = state.field(statementDecoField);
    assert.ok(deco.size > 0);
    const classes: string[] = [];
    deco.between(0, DOC.length, (_from, _to, value) => {
      classes.push(value.spec.class);
    });
    assert.deepEqual(classes, ['sql-stmt-line sql-stmt-line--first sql-stmt-line--last']);
  });

  it('光标移到第二条语句,装饰行随之变化', () => {
    const state = EditorState.create({
      doc: DOC,
      selection: { anchor: 12 },
      extensions: [statementDecorator()],
    });
    const deco = state.field(statementDecoField);
    const lines: { from: number; to: number }[] = [];
    deco.between(0, DOC.length, (from, to) => {
      lines.push({ from, to });
    });
    assert.deepEqual(lines, [{ from: 10, to: 10 }]);
  });

  it('光标在语句外时装饰为空集', () => {
    const state = EditorState.create({
      doc: 'SELECT 1;\n\nSELECT 2;',
      selection: { anchor: 10 },
      extensions: [statementDecorator()],
    });
    assert.equal(state.field(statementDecoField).size, 0);
  });
});

describe('executionGutter 扩展', () => {
  it('可随 EditorState 创建而不抛错(按钮 DOM 渲染依赖真实视图)', () => {
    const state = EditorState.create({
      doc: DOC,
      extensions: [
        executingStatement,
        executionGutter({ onExecute: () => {} }),
      ],
    });
    assert.ok(state);
  });
});
