/** 单张表的元数据(与后端接口返回对齐;字段只读,消费方仅作只读展示/补全)。 */
export interface SQLTableMetadata {
  readonly name: string;
  readonly detail?: string;
  readonly columns: readonly { readonly name: string; readonly type: string }[];
}

interface SchemaColumn {
  label: string;
  type: 'column';
  detail: string;
}

interface SchemaTable {
  self: { label: string; type: 'table' };
  children: SchemaColumn[];
}

/** 将元数据转换为 lang-sql 补全所需的 { self, children } 结构。 */
export function sqlSchemaFromMetadata(tables: readonly SQLTableMetadata[]): Record<string, SchemaTable> {
  const schema: Record<string, SchemaTable> = {};
  for (const table of tables) {
    // 按表名索引,后出现的同名表覆盖前者;数据库 schema 已由后端过滤,正常无同名。
    schema[table.name] = {
      self: { label: table.name, type: 'table' },
      children: table.columns.map((column) => ({
        label: column.name,
        type: 'column',
        detail: column.type,
      })),
    };
  }
  return schema;
}
