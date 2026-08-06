import { StandardSQL } from '@codemirror/lang-sql';

/** 一条 SQL 语句在文档中的位置与文本。 */
export interface SQLStatementRange {
  from: number;
  to: number;
  startLine: number;
  endLine: number;
  text: string;
}

/** lang-sql 语法树以 Script 为顶层节点,语句为 Statement 节点。 */
const PARSER = StandardSQL.language.parser;

function lineNumber(doc: string, offset: number): number {
  let line = 0;
  for (let i = 0; i < offset; i++) {
    if (doc.charCodeAt(i) === 10) line++;
  }
  return line;
}

/** 解析文档中的全部 SQL 语句(基于 Lezer 语法树,字符串/注释内分号不会误切)。 */
export function parseSQLStatements(doc: string): SQLStatementRange[] {
  if (!doc.trim()) return [];
  const tree = PARSER.parse(doc);
  const statements: SQLStatementRange[] = [];
  tree.iterate({
    enter: (node) => {
      if (node.name !== 'Statement') return;
      statements.push({
        from: node.from,
        to: node.to,
        startLine: lineNumber(doc, node.from),
        endLine: lineNumber(doc, node.to),
        text: doc.slice(node.from, node.to),
      });
    },
  });
  return statements;
}

/** 返回光标位置所在的语句;位置在注释/空白处时返回 null。 */
export function statementAtPosition(
  statements: readonly SQLStatementRange[],
  pos: number,
): SQLStatementRange | null {
  return statements.find((stmt) => pos >= stmt.from && pos <= stmt.to) ?? null;
}
