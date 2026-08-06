import { StateEffect, StateField, RangeSetBuilder, type Extension } from '@codemirror/state';
import { GutterMarker, gutter, type EditorView } from '@codemirror/view';

import { parseSQLStatements, type SQLStatementRange } from '@/utils/sqlStatements';

/** 设置当前执行中的语句 from 偏移(null 表示清除)。 */
export const setExecutingStatement = StateEffect.define<number | null>();

/** 当前正在执行的语句 from 偏移(无执行中语句时为 null)。 */
export const executingStatement = StateField.define<number | null>({
  create: () => null,
  update: (value, transaction) => {
    const effect = transaction.effects.find((candidate) => candidate.is(setExecutingStatement));
    return effect ? effect.value : value;
  },
});

/** gutter 标记数据(纯数据,不依赖 DOM,便于测试)。 */
export interface GutterMarkerSpec {
  /** 语句起始偏移(用于定位按钮所属行)。 */
  from: number;
  /** 该语句是否正在执行(按钮显示 loading)。 */
  executing: boolean;
}

/** 计算每条语句在行号区的标记数据:每条语句起始行一个按钮。 */
export function gutterMarkersFor(
  statements: readonly SQLStatementRange[],
  executingFrom: number | null,
): GutterMarkerSpec[] {
  return statements.map((statement) => ({
    from: statement.from,
    executing: executingFrom !== null && statement.from === executingFrom,
  }));
}

/** 行号执行按钮标记。 */
class ExecuteMarker extends GutterMarker {
  constructor(
    readonly from: number,
    readonly executing: boolean,
    readonly onExecute: (statement: SQLStatementRange) => void,
  ) {
    super();
  }
  override eq(other: ExecuteMarker): boolean {
    return other.from === this.from && other.executing === this.executing;
  }
  override toDOM(view: EditorView): HTMLElement {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'sql-exec-marker';
    button.title = this.executing ? '执行中…' : '执行此语句';
    button.textContent = this.executing ? '…' : '▶';
    button.disabled = this.executing;
    button.addEventListener('click', (event) => {
      event.stopPropagation();
      // 关键:gutter 在 eq 命中时会复用既有按钮 DOM 而不再调用 toDOM,
      // 因此这里不能使用创建时的语句快照(陈旧闭包)。必须基于当前
      // view.state 重新解析,否则编辑文档后点击执行的是旧语句文本。
      const statements = parseSQLStatements(view.state.doc.toString());
      const target = statements.find((statement) => statement.from === this.from);
      if (target) this.onExecute(target);
    });
    return button;
  }
}

/**
 * 行号执行按钮:每条语句起始行一个 ▶(执行中显示 … 并禁用)。
 *
 * 返回的扩展自带 executingStatement 状态字段,消费方无需再手动挂载。
 * 注意:gutter 的 `markers` 在本版本(@codemirror/view 6.43+)要求返回
 * `RangeSet<GutterMarker>`,且 marker 的 range 起点必须恰好等于目标行的
 * 行首偏移,否则该按钮不会渲染到对应行。
 */
export function executionGutter(options: { onExecute: (statement: SQLStatementRange) => void }): Extension {
  return [
    executingStatement,
    gutter({
      class: 'sql-exec-gutter',
      markers: (view) => {
        const statements = parseSQLStatements(view.state.doc.toString());
        const executingFrom = view.state.field(executingStatement, false) ?? null;
        const builder = new RangeSetBuilder<GutterMarker>();
        for (const spec of gutterMarkersFor(statements, executingFrom)) {
          const line = view.state.doc.lineAt(spec.from);
          builder.add(line.from, line.from, new ExecuteMarker(spec.from, spec.executing, options.onExecute));
        }
        return builder.finish();
      },
    }),
  ];
}
