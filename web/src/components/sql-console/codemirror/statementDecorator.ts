import { StateField, type EditorState, type Extension } from '@codemirror/state';
import { Decoration, EditorView, type DecorationSet } from '@codemirror/view';

import { parseSQLStatements, statementAtPosition, type SQLStatementRange } from '@/utils/sqlStatements';

/** 一条语句虚线框的行级装饰(纯数据,不依赖 CM6 对象,便于测试)。 */
export interface StatementLineDecoration {
  /** 该行的行首偏移。 */
  from: number;
  /** 该行的行尾偏移(不含换行符)。 */
  to: number;
  /** 应用到该行的 CSS 类名。 */
  className: string;
}

/**
 * 计算光标所在语句的虚线框装饰:从语句首行到末行逐行生成一条行装饰,
 * 首行附加 `--first`、末行附加 `--last` 类名;光标不在语句内(空白/注释)
 * 或没有语句时返回空数组。
 */
export function decorationsForStatements(
  statements: readonly SQLStatementRange[],
  pos: number,
  doc: string,
): StatementLineDecoration[] {
  const current = statementAtPosition(statements, pos);
  if (!current) return [];
  const lineStart = (offset: number) => doc.lastIndexOf('\n', offset - 1) + 1;
  const lineEnd = (offset: number) => {
    const newline = doc.indexOf('\n', offset);
    return newline === -1 ? doc.length : newline;
  };
  const firstLineFrom = lineStart(current.from);
  const lastLineTo = lineEnd(current.to);
  const decorations: StatementLineDecoration[] = [];
  let from = firstLineFrom;
  for (;;) {
    const to = lineEnd(from);
    const isFirst = from === firstLineFrom;
    const isLast = to >= lastLineTo;
    const className = `sql-stmt-line${isFirst ? ' sql-stmt-line--first' : ''}${isLast ? ' sql-stmt-line--last' : ''}`;
    decorations.push({ from, to, className });
    if (isLast) break;
    from = to + 1;
  }
  return decorations;
}

/** 依据光标位置计算当前语句的虚线框装饰集。 */
function computeDecorations(state: EditorState): DecorationSet {
  const doc = state.doc.toString();
  const statements = parseSQLStatements(doc);
  const lines = decorationsForStatements(statements, state.selection.main.head, doc);
  if (!lines.length) return Decoration.none;
  // 行装饰使用零长度 range,起点为该行行首(CM6 惯例,如 activeLine)。
  return Decoration.set(
    lines.map((line) => Decoration.line({ class: line.className }).range(line.from)),
    true,
  );
}

/** 当前语句虚线框装饰集合(导出便于测试)。 */
export const statementDecoField = StateField.define<DecorationSet>({
  // 创建即计算,保证编辑器挂载后立即高亮当前语句。
  create: (state) => computeDecorations(state),
  update: (decorations, transaction) => {
    // 光标移动或文档变化时重新计算;其余事务(如折叠、滚动)保持原装饰。
    if (!transaction.docChanged && !transaction.selection) return decorations;
    return computeDecorations(transaction.state);
  },
  // 将装饰集提供给视图渲染。
  provide: (field) => EditorView.decorations.from(field),
});

/** 当前语句虚线框插件(selection 变化 → 高亮语句所在行组)。 */
export function statementDecorator(): Extension {
  return [statementDecoField];
}
