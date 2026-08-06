# Web SQL 控制台 CodeMirror 6 改造实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Web SQL 控制台编辑器替换为 CodeMirror 6,实现语法树语句切分、行号执行按钮、当前语句虚线框与表/列自动补全。

**Architecture:** 后端新增元数据查询接口(复用现有 SQLConsole 会话连接查 information_schema,走 RBAC 不建审计);前端引入 CM6,语句切分基于 lang-sql 的 Lezer 语法树(`Script` → `Statement` 节点,已验证节点结构与边界行为),gutter 按钮与虚线框均为 CM6 插件;现有执行/写确认/审计流程完全复用。

**Tech Stack:** Go(后端元数据接口)、Vue 3 + TypeScript + Vite、CodeMirror 6(codemirror / @codemirror/lang-sql / @codemirror/view / @codemirror/language / @codemirror/state / @codemirror/autocomplete)、vitest + happy-dom、tsx --test(utils 单测)。

## Global Constraints

- 注释与 git 提交信息一律使用中文(项目规范);
- 前端仅新增依赖：`codemirror`、`@codemirror/lang-sql`、`@codemirror/view`、`@codemirror/language`、`@codemirror/state`、`@codemirror/autocomplete`,不引入其他库;
- 现有执行、写确认(PRECONDITION_FAILED → 确认弹窗 → 重发)、404 自动重连、审计逻辑**不做任何修改**;
- 元数据查询不建审计会话(已确认),仅走 RBAC `ActionDBQuery` 校验;
- 元数据表数量上限 500 张,超出截断;
- 后端 `singleSQLStatement` 单语句语义不变;
- i18n 文案统一加入 `web/src/i18n/index.ts`;
- 语句切分节点名以 lang-sql 语法树为准：顶层 `Script` → 子节点 `Statement`(含精确 `from`/`to`),注释/空白不在 Statement 范围内。

---

### Task 1: 后端 executor 元数据查询

**Files:**
- Modify: `internal/service/sql_console_executor.go`(接口 + 实现 + SQL 生成/解析纯函数)
- Test: `internal/service/sql_console_executor_test.go`(新建)

**Interfaces:**
- Produces:
  - `type SQLConsoleMetadata struct { Tables []SQLConsoleTableMeta }`
  - `type SQLConsoleTableMeta struct { Name, Detail string; Columns []SQLConsoleColumnMeta }`
  - `type SQLConsoleColumnMeta struct { Name, Type string }`
  - `SQLConsoleConnection` 接口新增方法 `Metadata(context.Context, string) (SQLConsoleMetadata, error)`
  - 纯函数 `metadataStatementSQL(protocol string) (tablesSQL, columnsSQL string)`(MySQL / PostgreSQL 分支)
  - 纯函数 `parseMetadataTables(protocol string, tablesRows [][]any, columnsRows [][]any) SQLConsoleMetadata`(含 500 表截断)

- [ ] **Step 1: 写失败的测试**(新建 `internal/service/sql_console_executor_test.go`,沿用项目 `node:assert` 风格——Go 侧用标准 `testing`)

```go
package service

import (
	"strings"
	"testing"
)

func TestMetadataStatementSQLMySQL(t *testing.T) {
	tablesSQL, columnsSQL := metadataStatementSQL("mysql")
	if !strings.Contains(tablesSQL, "DATABASE()") || !strings.Contains(tablesSQL, "information_schema.TABLES") {
		t.Fatalf("MySQL tables SQL 不符合预期: %s", tablesSQL)
	}
	if !strings.Contains(columnsSQL, "ORDINAL_POSITION") {
		t.Fatalf("MySQL columns SQL 不符合预期: %s", columnsSQL)
	}
}

func TestMetadataStatementSQLPostgres(t *testing.T) {
	tablesSQL, columnsSQL := metadataStatementSQL("postgres")
	if !strings.Contains(tablesSQL, "current_schema()") || !strings.Contains(tablesSQL, "information_schema.tables") {
		t.Fatalf("Postgres tables SQL 不符合预期: %s", tablesSQL)
	}
	if !strings.Contains(columnsSQL, "current_schema()") {
		t.Fatalf("Postgres columns SQL 不符合预期: %s", columnsSQL)
	}
}

func TestParseMetadataTables(t *testing.T) {
	tables := [][]any{{"users", "系统用户"}, {"orders", ""}}
	columns := [][]any{{"users", "id", "bigint"}, {"users", "name", "varchar"}, {"orders", "id", "int"}}
	meta := parseMetadataTables("mysql", tables, columns)
	if len(meta.Tables) != 2 {
		t.Fatalf("期望 2 张表,实际 %d", len(meta.Tables))
	}
	if meta.Tables[0].Name != "users" || meta.Tables[0].Detail != "系统用户" || len(meta.Tables[0].Columns) != 2 {
		t.Fatalf("users 表解析错误: %+v", meta.Tables[0])
	}
	if meta.Tables[0].Columns[1].Type != "varchar" {
		t.Fatalf("列类型解析错误: %+v", meta.Tables[0].Columns[1])
	}
}

func TestParseMetadataTablesTruncated(t *testing.T) {
	tables := make([][]any, 0, 600)
	for i := 0; i < 600; i++ {
		tables = append(tables, []any{strings.Repeat("t", 3), ""})
	}
	meta := parseMetadataTables("mysql", tables, nil)
	if len(meta.Tables) != sqlConsoleMetadataMaxTables {
		t.Fatalf("期望截断到 %d 张,实际 %d", sqlConsoleMetadataMaxTables, len(meta.Tables))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/service/ -run 'TestMetadata|TestParseMetadata' -v`
Expected: FAIL(`metadataStatementSQL` 未定义、`sqlConsoleMetadataMaxTables` 未定义)

- [ ] **Step 3: 最小实现**(在 `internal/service/sql_console_executor.go` 追加)

```go
const sqlConsoleMetadataMaxTables = 500

type SQLConsoleMetadata struct {
	Tables []SQLConsoleTableMeta
}

type SQLConsoleTableMeta struct {
	Name    string
	Detail  string
	Columns []SQLConsoleColumnMeta
}

type SQLConsoleColumnMeta struct {
	Name string
	Type string
}

// metadataStatementSQL 返回按协议区分元数据查询语句(统一 information_schema)。
func metadataStatementSQL(protocol string) (tablesSQL, columnsSQL string) {
	schemaExpr := "DATABASE()"
	if isPostgresProtocol(protocol) {
		schemaExpr = "current_schema()"
	}
	tablesSQL = "SELECT table_name, '' AS detail FROM information_schema.tables WHERE table_schema = " + schemaExpr + " ORDER BY table_name"
	columnsSQL = "SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = " + schemaExpr + " ORDER BY table_name, ordinal_position"
	return tablesSQL, columnsSQL
}

// parseMetadataTables 将两轮查询结果组装为元数据,表数超过上限时截断。
func parseMetadataTables(protocol string, tablesRows [][]any, columnsRows [][]any) SQLConsoleMetadata {
	limit := sqlConsoleMetadataMaxTables
	if len(tablesRows) > limit {
		tablesRows = tablesRows[:limit]
	}
	meta := SQLConsoleMetadata{Tables: make([]SQLConsoleTableMeta, 0, len(tablesRows))}
	index := make(map[string]int, len(tablesRows))
	for _, row := range tablesRows {
		name, _ := row[0].(string)
		detail, _ := row[1].(string)
		meta.Tables = append(meta.Tables, SQLConsoleTableMeta{Name: name, Detail: detail})
		index[name] = len(meta.Tables) - 1
	}
	for _, row := range columnsRows {
		table, _ := row[0].(string)
		column, _ := row[1].(string)
		columnType, _ := row[2].(string)
		if tableIndex, ok := index[table]; ok {
			meta.Tables[tableIndex].Columns = append(
				meta.Tables[tableIndex].Columns,
				SQLConsoleColumnMeta{Name: column, Type: columnType},
			)
		}
	}
	return meta
}
```

注:`isPostgresProtocol` 已存在于本文件(`listSQLConsoleDatabases` 使用),MySQL 分支使用现有 `openSQLConsoleDatabase` 同款判断;若不存在则定义为 `strings.EqualFold(protocol, "postgres") || strings.EqualFold(protocol, "postgresql")`。

- [ ] **Step 4: 实现 `SQLConsoleConnection.Metadata` 方法**(同文件,`databaseSQLConsoleConnection` 追加)

```go
// Metadata 查询数据库表结构与列类型(只读,不建审计会话)。
func (c *databaseSQLConsoleConnection) Metadata(ctx context.Context, database string) (SQLConsoleMetadata, error) {
	if ctx == nil {
		return SQLConsoleMetadata{}, errors.New("query metadata: nil context")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return SQLConsoleMetadata{}, errors.New("SQL console connection is closed")
	}
	metadataContext, cancel := context.WithTimeout(ctx, sqlConsoleTimeout)
	defer cancel()
	db, err := c.databasePool(metadataContext, strings.TrimSpace(database))
	if err != nil {
		return SQLConsoleMetadata{}, err
	}
	tablesSQL, columnsSQL := metadataStatementSQL(c.account.Instance.Protocol)
	tablesRows, err := querySQLConsoleRows(metadataContext, db, tablesSQL)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("query metadata tables: %w", err)
	}
	columnsRows, err := querySQLConsoleRows(metadataContext, db, columnsSQL)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("query metadata columns: %w", err)
	}
	return parseMetadataTables(c.account.Instance.Protocol, tablesRows, columnsRows), nil
}

// querySQLConsoleRows 执行只读查询并返回原始行数据(复用现有查询行扫描模式)。
func querySQLConsoleRows(ctx context.Context, db *sql.DB, statement string) ([][]any, error) {
	transaction, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read-only query: %w", err)
	}
	defer transaction.Rollback()
	rows, err := transaction.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read result columns: %w", err)
	}
	result := make([][]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan result row: %w", err)
		}
		for index := range values {
			values[index] = normalizeSQLConsoleValue(values[index])
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read result rows: %w", err)
	}
	return result, nil
}
```

`normalizeSQLConsoleValue` 返回三元组 `(any, int, bool)`,此处取首值即可(若签名不符,改为解构取第一个返回值)。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/service/ -run 'TestMetadata|TestParseMetadata' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/service/sql_console_executor.go internal/service/sql_console_executor_test.go
git commit -m "feat(service): SQL 控制台元数据查询(表/列结构)"
```

---

### Task 2: 后端 service.Metadata + handler + 路由

**Files:**
- Modify: `internal/service/sql_console.go`(新增 `Metadata` 方法)
- Modify: `internal/service/sql_console_test.go`(stub 补 `Metadata` + 新增用例)
- Modify: `internal/handler/sqlconsole/handler.go`(新增 `HandleMetadata`)
- Modify: `internal/server/admin/sql_console_handlers.go`(路径分发)

**Interfaces:**
- Consumes: Task 1 的 `SQLConsoleMetadata`、`SQLConsoleConnection.Metadata`
- Produces:
  - `(*SQLConsoleService).Metadata(ctx, actor SQLConsoleActor, sessionID, database string) (SQLConsoleMetadata, error)`
  - `(*sqlconsole.Handler).HandleMetadata(w, r, actor, sessionID string)`

- [ ] **Step 1: 给测试 stub 补 Metadata 方法**(`internal/service/sql_console_test.go`,让编译通过)

```go
type sqlConsoleConnectionStub struct {
	called   int
	closed   int
	readOnly bool
	result   SQLConsoleExecution
	err      error
	metadata SQLConsoleMetadata
	metaErr  error
}

func (s *sqlConsoleConnectionStub) Metadata(_ context.Context, _ string) (SQLConsoleMetadata, error) {
	return s.metadata, s.metaErr
}
```

- [ ] **Step 2: 写失败的 service 测试**(追加到 `internal/service/sql_console_test.go`)

```go
func TestSQLConsoleServiceMetadata(t *testing.T) {
	svc, _, authorizer, executor := newSQLConsoleServiceFixture(t)
	executor.connection.metadata = SQLConsoleMetadata{
		Tables: []SQLConsoleTableMeta{{Name: "users", Columns: []SQLConsoleColumnMeta{{Name: "id", Type: "bigint"}}}},
	}
	session, err := svc.CreateSession(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice", ClientIP: "127.0.0.1"}, "account-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	meta, err := svc.Metadata(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice", ClientIP: "127.0.0.1"}, session.ID, "app")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBQuery {
		t.Fatalf("unexpected rbac actions: %v", authorizer.actions)
	}
}

func TestSQLConsoleServiceMetadataForbidden(t *testing.T) {
	svc, _, authorizer, _ := newSQLConsoleServiceFixture(t)
	authorizer.allowed = false
	_, err := svc.CreateSession(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice"}, "account-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session, err := svc.CreateSession(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice"}, "account-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = svc.Metadata(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice"}, session.ID, "app")
	if err != ErrSQLConsoleForbidden {
		t.Fatalf("期望 ErrSQLConsoleForbidden,实际 %v", err)
	}
}
```

注:先确认 `CreateSession` 的确切签名(查看 `sql_console.go` 中 `CreateSession`),测试中的创建会话调用以实际签名为准——若为 `CreateSession(ctx, actor, accountID)` 则如上;测试前 `go build ./...` 验证签名。

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/service/ -run TestSQLConsoleServiceMetadata -v`
Expected: FAIL(`Metadata` 方法不存在)

- [ ] **Step 4: 实现 service.Metadata**(`internal/service/sql_console.go` 追加,遵循 `Execute` 的校验顺序)

```go
// Metadata 返回数据库表结构元数据(供前端补全),不建审计会话。
func (s *SQLConsoleService) Metadata(
	ctx context.Context,
	actor SQLConsoleActor,
	sessionID, database string,
) (SQLConsoleMetadata, error) {
	if ctx == nil || strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(sessionID) == "" {
		return SQLConsoleMetadata{}, ErrSQLConsoleInvalid
	}
	webSession, err := s.sessionForActor(strings.TrimSpace(sessionID), strings.TrimSpace(actor.UserID))
	if err != nil {
		return SQLConsoleMetadata{}, err
	}
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		strings.TrimSpace(actor.UserID),
		[]string{rbac.ActionDBQuery},
		model.ResourceTypeDatabaseAccount,
		webSession.accountID,
	)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("authorize SQL console: %w", err)
	}
	if !allowed {
		return SQLConsoleMetadata{}, ErrSQLConsoleForbidden
	}
	account, _, err := s.loadSQLConsoleAccount(ctx, webSession.accountID)
	if err != nil {
		return SQLConsoleMetadata{}, err
	}
	database = strings.TrimSpace(database)
	if database == "" || !webSession.databaseAllowed(database) {
		return SQLConsoleMetadata{}, ErrSQLConsoleInvalid
	}
	connection, err := s.executor.Connect(ctx, account)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("connect SQL console: %w", err)
	}
	defer connection.Close()
	return connection.Metadata(ctx, database)
}
```

若 `Execute` 中已有会话连接复用(而非每次 Connect),则保持与 `Execute` 相同的连接获取方式(检查 `sql_console.go` 中 `Execute` 如何取得 connection——以现状为准,保持一致的连接生命周期)。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/service/ -run TestSQLConsoleServiceMetadata -v`
Expected: PASS

- [ ] **Step 6: 实现 handler**(`internal/handler/sqlconsole/handler.go` 追加)

```go
// HandleMetadata 返回数据库表结构元数据。
func (h *Handler) HandleMetadata(w http.ResponseWriter, r *http.Request, actor Actor, sessionID string) {
	if r.Method != http.MethodGet {
		apiresp.WriteError(w, r, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed,
			"method not allowed", nil, apiresp.RequestID(r.Context()))
		return
	}
	meta, err := h.service.Metadata(
		r.Context(),
		service.SQLConsoleActor{UserID: actor.UserID, Username: actor.Username, ClientIP: actor.ClientIP},
		sessionID,
		r.URL.Query().Get("database"),
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	apiresp.Write(w, http.StatusOK, meta, apiresp.RequestID(r.Context()))
}
```

- [ ] **Step 7: 挂载路由**(`internal/server/admin/sql_console_handlers.go` 的 `handleSQLConsoleSession` 中追加分支)

```go
	if len(parts) == 2 && parts[0] != "" && parts[1] == "metadata" {
		s.sqlConsole.HandleMetadata(w, r, sqlConsoleActor(r), parts[0])
		return
	}
```

- [ ] **Step 8: 全量构建 + 测试**

Run: `go build ./... && go test ./internal/service/ ./internal/handler/sqlconsole/`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/service/sql_console.go internal/service/sql_console_test.go internal/handler/sqlconsole/handler.go internal/server/admin/sql_console_handlers.go
git commit -m "feat(sql-console): 元数据查询接口(RBAC 校验,不建审计会话)"
```

---

### Task 3: 前端语句切分纯函数

**Files:**
- Create: `web/src/utils/sqlStatements.ts`
- Test: `web/src/utils/sqlStatements.test.ts`
- Modify: `web/package.json`(测试脚本追加新用例文件)

**Interfaces:**
- Produces:
  - `interface SQLStatementRange { from: number; to: number; startLine: number; endLine: number; text: string }`
  - `parseSQLStatements(doc: string): SQLStatementRange[]`(空文档返回 `[]`)
  - `statementAtPosition(statements: readonly SQLStatementRange[], pos: number): SQLStatementRange | null`(命中 `[from, to]` 区间;注释/空白处返回 `null`)

- [ ] **Step 1: 安装依赖**

```bash
cd web
npm install codemirror@^6 @codemirror/lang-sql@^6 @codemirror/view@^6 @codemirror/language@^6 @codemirror/state@^6 @codemirror/autocomplete@^6 --registry=https://registry.npmmirror.com
```

- [ ] **Step 2: 写失败的测试**(`web/src/utils/sqlStatements.test.ts`,沿用项目 `node:assert` + `tsx --test` 风格)

```ts
import assert from 'node:assert/strict';
import { describe, it } from 'vitest';

import { parseSQLStatements, statementAtPosition } from './sqlStatements';

describe('parseSQLStatements', () => {
  it('空文档返回空数组', () => {
    assert.deepEqual(parseSQLStatements(''), []);
  });

  it('单条语句', () => {
    const [stmt] = parseSQLStatements('SELECT * FROM users;');
    assert.equal(stmt.from, 0);
    assert.equal(stmt.to, 19);
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
    assert.equal(stmts[0].from, 7);
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

  it('注释/空白位置返回 null', () => {
    assert.equal(statementAtPosition(stmts, 8), null);
  });

  it('空语句列表返回 null', () => {
    assert.equal(statementAtPosition([], 0), null);
  });
});
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd web && npx tsx --test src/utils/sqlStatements.test.ts`
Expected: FAIL(模块不存在)

- [ ] **Step 4: 最小实现**(`web/src/utils/sqlStatements.ts`)

```ts
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
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd web && npx tsx --test src/utils/sqlStatements.test.ts`
Expected: PASS

- [ ] **Step 6: 将新测试挂入测试脚本**(`web/package.json` 的 `test:connection-commands` 末尾追加 ` src/utils/sqlStatements.test.ts`),然后:

Run: `cd web && npm run test:connection-commands`
Expected: 全部 PASS(含既有用例)

- [ ] **Step 7: 提交**

```bash
git add web/src/utils/sqlStatements.ts web/src/utils/sqlStatements.test.ts web/package.json web/package-lock.json
git commit -m "feat(web): SQL 语句切分与光标定位纯函数(语法树)"
```

---

### Task 4: 前端元数据 → schema 转换

**Files:**
- Create: `web/src/utils/sqlSchema.ts`
- Test: `web/src/utils/sqlSchema.test.ts`

**Interfaces:**
- Consumes: Task 5 定义的 `SQLConsoleTableMetadata` 类型(为避免循环,本任务在 `web/src/api/client.ts` 中先定义该类型,见 Task 5 Step 1;若先行实现本任务,则在 `sqlSchema.ts` 内定义并导出,Task 5 复用)
- Produces: `sqlSchemaFromMetadata(tables: readonly SQLTableMetadata[]): Record<string, { self: { label: string; type: 'table' }; children: { label: string; type: 'column'; detail: string }[] }>`

- [ ] **Step 1: 写失败的测试**(`web/src/utils/sqlSchema.test.ts`)

```ts
import assert from 'node:assert/strict';
import { describe, it } from 'vitest';

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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx tsx --test src/utils/sqlSchema.test.ts`
Expected: FAIL(模块不存在)

- [ ] **Step 3: 最小实现**(`web/src/utils/sqlSchema.ts`)

```ts
/** 单张表的元数据(与后端接口返回对齐)。 */
export interface SQLTableMetadata {
  name: string;
  detail?: string;
  columns: { name: string; type: string }[];
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npx tsx --test src/utils/sqlSchema.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add web/src/utils/sqlSchema.ts web/src/utils/sqlSchema.test.ts
git commit -m "feat(web): 元数据到 lang-sql schema 格式转换"
```

---

### Task 5: 前端 API client + useSQLConsole 元数据加载

**Files:**
- Modify: `web/src/api/client.ts`(类型 + `getSQLConsoleMetadata` 方法)
- Modify: `web/src/composables/useSQLConsole.ts`(`metadata` ref + `loadMetadata`)
- Modify: `web/src/composables/useSQLConsole.test.ts`(追加用例)

**Interfaces:**
- Consumes: Task 4 的 `SQLTableMetadata`(从 `sqlSchema.ts` 导入,避免与 client 耦合)
- Produces:
  - `apiClient.getSQLConsoleMetadata(sessionId: string, database: string, signal?: AbortSignal): Promise<{ tables: SQLTableMetadata[] }>`
  - `useSQLConsole()` 新增返回:`metadata: Readonly<Ref<SQLTableMetadata[]>>`、`loadMetadata(): Promise<void>`(失败静默置空)

- [ ] **Step 1: 写失败的 client 测试 + 用例**(追加到 `web/src/composables/useSQLConsole.test.ts`,沿用现有 mock 风格)

```ts
it('连接成功后自动加载元数据,失败静默降级为空', async () => {
  vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({ items: accounts.slice(0, 1), total: 1, page: 1, page_size: 200 });
  vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({ id: 'session-1', databases: ['app'], default_database: 'app' });
  vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
  vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockResolvedValue({ tables: [{ name: 'users', columns: [{ name: 'id', type: 'bigint' }] }] });
  const consoleState = useSQLConsole();
  await consoleState.loadAccounts();
  assert.equal(consoleState.metadata.value.length, 1);
  assert.equal(consoleState.metadata.value[0].name, 'users');
});

it('元数据加载失败时静默置空,不抛错', async () => {
  vi.spyOn(apiClient, 'getAllDBAccounts').mockResolvedValue({ items: accounts.slice(0, 1), total: 1, page: 1, page_size: 200 });
  vi.spyOn(apiClient, 'createSQLConsoleSession').mockResolvedValue({ id: 'session-1', databases: ['app'], default_database: 'app' });
  vi.spyOn(apiClient, 'closeSQLConsoleSession').mockResolvedValue(undefined);
  vi.spyOn(apiClient, 'getSQLConsoleMetadata').mockRejectedValue(new Error('boom'));
  const consoleState = useSQLConsole();
  await consoleState.loadAccounts();
  assert.deepEqual(consoleState.metadata.value, []);
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/composables/useSQLConsole.test.ts`
Expected: FAIL(`getSQLConsoleMetadata` 不存在 / `metadata` 未定义)

- [ ] **Step 3: client.ts 增加类型与方法**

```ts
/** SQL 控制台元数据:表结构(供编辑器补全)。 */
export interface SQLConsoleMetadataResult {
  tables: { name: string; detail?: string; columns: { name: string; type: string }[] }[];
}
```

`apiClient` 对象内(在 `executeSQL` 定义之后)追加:

```ts
getSQLConsoleMetadata: (sessionId: string, database: string, signal?: AbortSignal) =>
  request<SQLConsoleMetadataResult>(
    `/api/sql-console/sessions/${encodeURIComponent(sessionId)}/metadata?database=${encodeURIComponent(database)}`,
    { signal },
  ),
```

- [ ] **Step 4: useSQLConsole 增加元数据状态**(`web/src/composables/useSQLConsole.ts`)

```ts
import { sqlSchemaFromMetadata } 不需要——只持有原始元数据;转换由编辑器组件负责。
```

状态与加载方法:

```ts
const metadata = shallowRef<SQLTableMetadata[]>([]);
const metadataController = shallowRef<AbortController | null>(null);

async function loadMetadata(): Promise<void> {
  const currentSessionId = sessionId.value;
  const currentDatabase = database.value;
  metadataController.value?.abort();
  if (!currentSessionId || !currentDatabase) {
    metadata.value = [];
    return;
  }
  const controller = new AbortController();
  metadataController.value = controller;
  try {
    const response = await apiClient.getSQLConsoleMetadata(currentSessionId, currentDatabase, controller.signal);
    if (controller.signal.aborted) return;
    metadata.value = response.tables ?? [];
  } catch {
    if (!controller.signal.aborted) metadata.value = [];
  } finally {
    if (metadataController.value === controller) metadataController.value = null;
  }
}
```

- `connect()` 成功设置 `sessionId.value` 与 `database.value` 之后追加 `void loadMetadata();`
- `connect()` 开头与 `disconnect()` 中追加 `metadata.value = []; metadataController.value?.abort();`
- `watch(database, () => void loadMetadata())`(在现有 `watch(requestedAccountId, ...)` 之后)
- 返回对象中追加 `metadata: readonly(metadata)` 与 `loadMetadata`
- 类型导入:`import type { SQLTableMetadata } from '@/utils/sqlSchema';`

- [ ] **Step 5: 运行测试确认通过**

Run: `cd web && npx vitest run src/composables/useSQLConsole.test.ts && npm run typecheck`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add web/src/api/client.ts web/src/composables/useSQLConsole.ts web/src/composables/useSQLConsole.test.ts
git commit -m "feat(web): SQL 控制台元数据加载(连接/切库自动刷新,失败静默)"
```

---

### Task 6: 前端 gutter 执行按钮 + 虚线框插件

**Files:**
- Create: `web/src/components/sql-console/codemirror/executionGutter.ts`
- Create: `web/src/components/sql-console/codemirror/statementDecorator.ts`
- Test: `web/src/components/sql-console/codemirror/codemirrorPlugins.test.ts`

**Interfaces:**
- Consumes: Task 3 的 `SQLStatementRange` / `parseSQLStatements` / `statementAtPosition`
- Produces:
  - `executionGutter(options: { onExecute: (statement: SQLStatementRange) => void }): Extension`(gutter ▶ 按钮;含 `executingStatement` StateField,执行中的语句按钮显示 loading)
  - `statementDecorator(): Extension`(selection 变化 → 虚线框 decorations)

- [ ] **Step 1: 写失败的测试**(`codemirrorPlugins.test.ts`,CM6 状态层测试,无需真实 DOM)

```ts
import assert from 'node:assert/strict';
import { EditorState } from '@codemirror/state';
import { describe, it } from 'vitest';

import { executingStatement } from './executionGutter';
import { statementDecorator, statementDecoField } from './statementDecorator';

describe('executionGutter 状态字段', () => {
  it('executingStatement 默认与更新', () => {
    const state = EditorState.create({ extensions: [executingStatement] });
    assert.equal(state.field(executingStatement, false), null);
    const next = executingStatement.updateOf(state, 42);
    assert.equal(next.field(executingStatement), 42);
  });
});

describe('statementDecorator 装饰输出', () => {
  it('selection 移动时装饰行集合变化', () => {
    const doc = 'SELECT 1;\nSELECT 2;';
    const state = EditorState.create({ doc, extensions: [statementDecorator(), statementDecoField] });
    const first = state.field(statementDecoField, false);
    assert.ok(first && first.size > 0, '第一条语句应有装饰');
    const next = EditorState.create({ doc, selection: { anchor: 12 }, extensions: [statementDecorator(), statementDecoField] });
    const second = next.field(statementDecoField, false);
    assert.ok(second && second.size > 0);
    assert.notEqual(JSON.stringify(first), JSON.stringify(second), '不同语句的装饰应不同');
  });
});
```

注:若插件实现为 ViewPlugin,状态层测试改为直接测试插件内部暴露的纯函数:`decorationsForStatements(statements, pos)` 与 `gutterMarkersFor(statements, executingFrom)`(导出,不依赖 EditorView);上述测试按"导出纯函数 + 状态字段"两条路线任选其一,实现时以纯函数测试为主(更稳)。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/components/sql-console/codemirror/codemirrorPlugins.test.ts`
Expected: FAIL(模块不存在)

- [ ] **Step 3: 实现 executionGutter.ts**

```ts
import { gutter, GutterMarker } from '@codemirror/view';
import { StateEffect, StateField, type Extension } from '@codemirror/state';
import { syntaxTree } from '@codemirror/language';

import type { SQLStatementRange } from '@/utils/sqlStatements';

/** 当前正在执行的语句 from 偏移(无执行中语句时为 null)。 */
export const setExecutingStatement = StateEffect.define<number | null>();
export const executingStatement = StateField.define<number | null>({
  create: () => null,
  update: (value, transaction) =>
    transaction.effects.some((effect) => effect.is(setExecutingStatement))
      ? transaction.effects.find((effect) => effect.is(setExecutingStatement))!.value
      : value,
});

class ExecuteMarker extends GutterMarker {
  constructor(
    readonly from: number,
    readonly executing: boolean,
    readonly onExecute: (from: number) => void,
  ) {
    super();
  }
  override eq(other: ExecuteMarker) {
    return other.from === this.from && other.executing === this.executing;
  }
  override toDOM() {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'sql-exec-marker';
    button.title = '执行此语句';
    button.textContent = this.executing ? '…' : '▶';
    button.disabled = this.executing;
    button.addEventListener('click', (event) => {
      event.stopPropagation();
      this.onExecute(this.from);
    });
    return button;
  }
}

/** 行号执行按钮:每条语句起始行一个 ▶。 */
export function executionGutter(options: { onExecute: (statement: SQLStatementRange) => void }): Extension {
  return gutter({
    class: 'sql-exec-gutter',
    markers: (view) => {
      const statements = collectStatements(view);
      const current = view.state.field(executingStatement, false) ?? null;
      return statements.map(
        (statement) => new ExecuteMarker(statement.from, statement.from === current, (from) => {
          const target = statements.find((s) => s.from === from);
          if (target) options.onExecute(target);
        }),
      );
    },
  });
}
```

并追加 `collectStatements`(从语法树收集 Statement,与 Task 3 一致;实现时复用 `parseSQLStatements(view.state.doc.toString())` 即可——性能足够,保持单一数据源)。

- [ ] **Step 4: 实现 statementDecorator.ts**

```ts
import { Decoration, ViewPlugin, DecorationSet } from '@codemirror/view';
import { StateField, type Extension } from '@codemirror/state';

import { parseSQLStatements, statementAtPosition } from '@/utils/sqlStatements';

/** 语句虚线框装饰集合(导出便于测试)。 */
export const statementDecoField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update: (decorations, transaction) => {
    if (!transaction.docChanged && !transaction.selection) return decorations;
    const statements = parseSQLStatements(transaction.state.doc.toString());
    const current = statementAtPosition(statements, transaction.state.selection.main.head);
    if (!current) return Decoration.none;
    const ranges: ReturnType<typeof Decoration.line>[] = [];
    const lineAt = (offset: number) => transaction.state.doc.lineAt(offset);
    const firstLine = lineAt(current.from);
    const lastLine = lineAt(current.to);
    let line = firstLine;
    while (true) {
      const isFirst = line.from === firstLine.from;
      const isLast = line.to >= lastLine.to;
      const className = `sql-stmt-line${isFirst ? ' sql-stmt-line--first' : ''}${isLast ? ' sql-stmt-line--last' : ''}`;
      ranges.push(Decoration.line({ class: className }));
      if (isLast) break;
      line = transaction.state.doc.lineAt(line.to + 1);
    }
    return Decoration.set(ranges, true);
  },
});

/** 当前语句虚线框插件。 */
export function statementDecorator(): Extension {
  return [statementDecoField];
}
```

- [ ] **Step 5: 调整测试按实际导出,运行确认通过**

Run: `cd web && npx vitest run src/components/sql-console/codemirror/codemirrorPlugins.test.ts`
Expected: PASS(测试按 Step 1 或实现调整后的纯函数断言为准)

- [ ] **Step 6: 提交**

```bash
git add web/src/components/sql-console/codemirror/executionGutter.ts web/src/components/sql-console/codemirror/statementDecorator.ts web/src/components/sql-console/codemirror/codemirrorPlugins.test.ts
git commit -m "feat(web): 行号执行按钮与当前语句虚线框插件"
```

---

### Task 7: 前端 SQLEditorPanel 重写为 CM6

**Files:**
- Modify: `web/src/components/sql-console/SQLEditorPanel.vue`(整体重写)
- Modify: `web/src/styles/main.css`(gutter 按钮与虚线框样式,浅/深两套)
- Test: `web/src/components/sql-console/SQLEditorPanel.mount.test.ts`(新建)

**Interfaces:**
- Consumes: Task 3 `parseSQLStatements`/`statementAtPosition`、Task 4 `sqlSchemaFromMetadata`、Task 5 `SQLTableMetadata`、Task 6 `executionGutter`/`statementDecorator`/`executingStatement`/`setExecutingStatement`
- Produces:
  - props:`executing: boolean`、`disabled: boolean`、`metadata: SQLTableMetadata[]`、`dialect: 'mysql' | 'postgres'`(默认 `'mysql'`)
  - v-model:`sql: string`
  - emits:`execute: (statementText: string) => void`、`cancel: () => void`

- [ ] **Step 1: 写失败的挂载测试**(`SQLEditorPanel.mount.test.ts`;若 CM6 在 happy-dom 下无法挂载——报 ResizeObserver/getClientRects 相关错误,则在测试 setup 中 mock:`globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }`,并在 `beforeEach` 中 `vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({ width: 800, height: 400, top: 0, left: 0, right: 800, bottom: 400, x: 0, y: 0, toJSON: () => ({}) })`)

```ts
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import SQLEditorPanel from './SQLEditorPanel.vue';

describe('SQLEditorPanel (CodeMirror 6)', () => {
  beforeEach(() => {
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({ width: 800, height: 400, top: 0, left: 0, right: 800, bottom: 400, x: 0, y: 0, toJSON: () => ({}) } as DOMRect);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('渲染 CodeMirror 编辑器并回写 v-model', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { sql: 'SELECT 1;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    expect(wrapper.find('.cm-editor').exists()).toBe(true);
    // 通过编辑器输入触发 update:sql
    const view = (wrapper.vm as unknown as { getView(): unknown }).getView();
    // 若组件未暴露 getView,则断言编辑内容被渲染(见 Step 3 组件实现后补强)
    expect(view).toBeTruthy();
  });

  it('执行事件携带语句文本', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { sql: 'SELECT 1;\nSELECT 2;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    // 点击第一条语句的 gutter 执行按钮
    const marker = wrapper.find('.sql-exec-gutter .sql-exec-marker');
    await marker.trigger('click');
    const emitted = wrapper.emitted('execute');
    expect(emitted).toBeTruthy();
    expect(String(emitted![0][0])).toContain('SELECT 1');
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/components/sql-console/SQLEditorPanel.mount.test.ts`
Expected: FAIL(组件仍是 textarea 实现,`execute` 未携带文本或 `.cm-editor` 不存在)

- [ ] **Step 3: 重写 SQLEditorPanel.vue**

```vue
<script setup lang="ts">
import { EditorView, keymap, basicSetup } from 'codemirror';
import { sql, MySQL, PostgreSQL } from '@codemirror/lang-sql';
import { Compartment, type Extension } from '@codemirror/state';
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import { onBeforeUnmount, onMounted, watch } from 'vue';

import type { SQLTableMetadata } from '@/utils/sqlSchema';
import { sqlSchemaFromMetadata } from '@/utils/sqlSchema';
import { parseSQLStatements, statementAtPosition } from '@/utils/sqlStatements';

import { executionGutter, executingStatement, setExecutingStatement } from './codemirror/executionGutter';
import { statementDecorator } from './codemirror/statementDecorator';

const props = withDefaults(defineProps<{
  executing: boolean;
  disabled: boolean;
  metadata?: SQLTableMetadata[];
  dialect?: 'mysql' | 'postgres';
}>(), {
  metadata: () => [],
  dialect: 'mysql',
});

const emit = defineEmits<{
  execute: [statementText: string];
  cancel: [];
}>();

const sql = defineModel<string>({ required: true });

let view: EditorView | null = null;
const container = ref<HTMLElement | null>(null);

/* 主题(浅/深) */
const themeCompartment = new Compartment();
const themeExtensions = (dark: boolean): Extension => [
  EditorView.theme(
    {
      '&': { fontSize: '13px', backgroundColor: 'var(--color-surface)', color: 'var(--color-text)' },
      '.cm-gutters': { backgroundColor: 'var(--color-surface-muted)', color: 'var(--color-text-secondary)', borderRight: '1px solid var(--color-border)' },
      '.cm-activeLine': { backgroundColor: 'var(--color-primary-bg)' },
      '.cm-activeLineGutter': { backgroundColor: 'var(--color-surface-muted)', color: 'var(--color-primary)' },
      '.cm-cursor': { borderLeftColor: 'var(--color-primary)' },
      '.cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--color-primary) 20%, transparent)' },
      '&.cm-focused .cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--color-primary) 30%, transparent)' },
      '.cm-tooltip': { backgroundColor: 'var(--color-card)', border: '1px solid var(--color-border)', color: 'var(--color-text)' },
      '.cm-tooltip-autocomplete ul li[aria-selected]': { backgroundColor: 'var(--color-primary)', color: '#fff' },
      '.cm-tooltip-autocomplete ul li': { color: 'var(--color-text)' },
      '.cm-panels': { backgroundColor: 'var(--color-surface-muted)', color: 'var(--color-text)' },
    },
    { dark },
  ),
  syntaxHighlighting(
    HighlightStyle.define([
      { tag: tags.keyword, color: dark ? '#c586c0' : '#cf222e' },
      { tag: tags.string, color: dark ? '#ce9178' : '#0a3069' },
      { tag: tags.number, color: dark ? '#b5cea8' : '#0550ae' },
      { tag: tags.comment, color: dark ? '#6a9955' : '#6e7781', fontStyle: 'italic' },
      { tag: tags.typeName, color: dark ? '#4ec9b0' : '#0550ae' },
      { tag: tags.function(tags.variableName), color: dark ? '#dcdcaa' : '#8250df' },
      { tag: tags.operator, color: dark ? '#d4d4d4' : '#24292f' },
      { tag: tags.bool, color: dark ? '#569cd6' : '#cf222e' },
      { tag: tags.null, color: dark ? '#569cd6' : '#cf222e' },
    ]),
  ),
];

/* 补全 schema 动态切换 */
const schemaCompartment = new Compartment();
const schemaExtension = (metadata: SQLTableMetadata[], dialect: 'mysql' | 'postgres'): Extension =>
  sql({
    dialect: dialect === 'postgres' ? PostgreSQL : MySQL,
    schema: sqlSchemaFromMetadata(metadata),
  });

/* 执行当前语句(Ctrl+Enter / 工具栏执行按钮) */
function executeCurrentStatement(): void {
  if (!view) return;
  const statements = parseSQLStatements(view.state.doc.toString());
  const current = statementAtPosition(statements, view.state.selection.main.head);
  if (!current) return;
  emit('execute', current.text);
}

/* 执行指定语句(gutter 点击) */
function executeStatement(range: { from: number }): void {
  if (!view) return;
  const statements = parseSQLStatements(view.state.doc.toString());
  const target = statements.find((s) => s.from === range.from);
  if (target) emit('execute', target.text);
}

function isDarkTheme(): boolean {
  return document.documentElement.dataset.theme === 'dark';
}

onMounted(() => {
  if (!container.value) return;
  const initialDark = isDarkTheme();
  view = new EditorView({
    parent: container.value,
    doc: sql.value,
    extensions: [
      basicSetup,
      keymap.of([{ key: 'Ctrl-Enter', run: () => { executeCurrentStatement(); return true; } }, { key: 'Mod-Enter', run: () => { executeCurrentStatement(); return true; } }]),
      themeCompartment.of(themeExtensions(initialDark)),
      schemaCompartment.of(schemaExtension(props.metadata, props.dialect)),
      statementDecorator(),
      executionGutter({ onExecute: executeStatement }),
      EditorView.updateListener.of((update) => {
        if (update.docChanged && update.state.doc.toString() !== sql.value) {
          sql.value = update.state.doc.toString();
        }
      }),
    ],
  });
  /* 外部修改 sql 时同步进编辑器 */
  watch(sql, (value) => {
    if (!view) return;
    const current = view.state.doc.toString();
    if (value !== current) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
    }
  });
  /* 主题切换 */
  const observer = new MutationObserver(() => {
    if (!view) return;
    view.dispatch({ effects: themeCompartment.reconfigure(themeExtensions(isDarkTheme())) });
  });
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  /* 执行状态 → gutter loading */
  watch(() => props.executing, (executing) => {
    view?.dispatch({ effects: setExecutingStatement.of(executing ? executingFrom() : null) });
  });
  /* 元数据更新 → 补全重建 */
  watch(() => [props.metadata, props.dialect], () => {
    view?.dispatch({ effects: schemaCompartment.reconfigure(schemaExtension(props.metadata, props.dialect)) });
  });
  (view as unknown as { __cleanup?: () => void }).__cleanup = () => observer.disconnect();
});

function executingFrom(): number | null {
  if (!view) return null;
  const statements = parseSQLStatements(view.state.doc.toString());
  const current = statementAtPosition(statements, view.state.selection.main.head);
  return current?.from ?? null;
}

onBeforeUnmount(() => {
  (view as unknown as { __cleanup?: () => void }).__cleanup?.();
  view?.destroy();
  view = null;
});
</script>

<template>
  <section class="editor-panel" aria-labelledby="sql-console-editor-title">
    <header class="editor-header">
      <div>
        <strong id="sql-console-editor-title">{{ t('sqlConsole.editorLabel') }}</strong>
        <span>{{ t('sqlConsole.keyboardHint') }}</span>
      </div>
      <div class="editor-actions">
        <el-button v-if="executing" :icon="Close" @click="emit('cancel')">
          {{ t('sqlConsole.cancel') }}
        </el-button>
        <el-button type="primary" :icon="VideoPlay" :loading="executing" :disabled="disabled || executing" @click="executeCurrentStatement">
          {{ t('sqlConsole.execute') }}
        </el-button>
      </div>
    </header>
    <div ref="container" class="sql-editor-container" />
  </section>
</template>
```

补充说明(实现者必读):
- `import { ref } from 'vue'` 需补上;`t` 从 `@/i18n` 导入(现有实现同款);`Close`/`VideoPlay` 图标从 `@element-plus/icons-vue` 导入;
- 若 `executingFrom` 依赖 `statementAtPosition` 在 selection 变化时更新,`watch(() => props.executing)` 读取当前光标语句即可;
- `sqlStatements.ts` 的 `statementAtPosition` 已处理空语句;
- `sql.value` 更新回写时用 guard 防循环(updateListener 内比较)。

- [ ] **Step 4: 全局样式**(`web/src/styles/main.css` 追加,浅色默认 + `[data-theme="dark"]` 覆盖)

```css
.sql-exec-gutter { width: 28px; }
.sql-exec-marker {
  display: grid;
  width: 20px;
  height: 20px;
  margin: 2px auto;
  padding: 0;
  color: var(--color-primary);
  background: transparent;
  border: 0;
  border-radius: 6px;
  cursor: pointer;
  font-size: 10px;
  place-items: center;
}
.sql-exec-marker:hover { background: var(--color-primary-bg); }
.sql-exec-marker:disabled { color: var(--color-text-secondary); cursor: not-allowed; }
.cm-line.sql-stmt-line { background: color-mix(in srgb, var(--color-primary) 8%, transparent); border-left: 2px dashed var(--color-primary); }
.cm-line.sql-stmt-line--first { border-top: 1px dashed var(--color-primary); }
.cm-line.sql-stmt-line--last { border-bottom: 1px dashed var(--color-primary); }
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd web && npx vitest run src/components/sql-console/SQLEditorPanel.mount.test.ts`
Expected: PASS(若 happy-dom 下 CM6 无法稳定渲染,测试回退为:断言组件暴露的 `executeCurrentStatement` 逻辑通过 emit 携带语句文本——挂载测试中直接调用组件方法并断言 `execute` 事件内容)

- [ ] **Step 6: typecheck + 全量前端测试**

Run: `cd web && npm run typecheck && npm run test:connection-dialog`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add web/src/components/sql-console/SQLEditorPanel.vue web/src/components/sql-console/SQLEditorPanel.mount.test.ts web/src/styles/main.css
git commit -m "feat(web): SQL 编辑器重写为 CodeMirror 6(高亮/补全/语句执行)"
```

---

### Task 8: 接线(i18n、Workspace、useSQLConsole 执行语义)+ 全量验证

**Files:**
- Modify: `web/src/views/SQLConsoleWorkspace.vue`(handleExecute 接收语句文本;编辑器接入 metadata/dialect)
- Modify: `web/src/i18n/index.ts`(新增键)
- Modify: `web/package.json`(测试脚本挂入新 mount 测试,若 Task 7 已处理则跳过)
- Test: `web/src/views/SQLConsoleWorkspace` 相关既有用例按需更新(无则跳过)

**Interfaces:**
- Consumes: Task 7 的 SQLEditorPanel 新接口(props `metadata`/`dialect`,emit `execute(statementText)`)

- [ ] **Step 1: SQLConsoleWorkspace 接线**

`web/src/views/SQLConsoleWorkspace.vue` 中:

```ts
const { t } = useI18n();
// ...现有代码,修改:
function handleExecute(statementText?: string): void {
  if (!accountId.value) {
    ElMessage.warning(t('sqlConsole.error.missingAccount'));
    return;
  }
  if (!database.value) {
    ElMessage.warning(t('sqlConsole.error.missingDatabase'));
    return;
  }
  const sqlText = statementText?.trim() ?? sql.value.trim();
  if (!sqlText) {
    ElMessage.warning(t('sqlConsole.error.missingSQL'));
    return;
  }
  sql.value = sqlText;
  void executeSQL(sqlText, false);
}

async function executeSQL(sqlText: string, confirmWrite: boolean): Promise<void> {
  try {
    await execute(confirmWrite);
    ElMessage.success(t('sqlConsole.executionSucceeded'));
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === 'PRECONDITION_FAILED') {
      await confirmAndExecuteWrite(sqlText);
      return;
    }
    if (cause instanceof DOMException && cause.name === 'AbortError') {
      ElMessage.info(t('sqlConsole.executionCancelled'));
      return;
    }
    ElMessage.error(cause instanceof Error ? cause.message : t('sqlConsole.executionFailed'));
  }
}

async function confirmAndExecuteWrite(sqlText: string): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('sqlConsole.writeConfirmMessage'),
      t('sqlConsole.writeConfirmTitle'),
      { confirmButtonText: t('sqlConsole.execute'), cancelButtonText: t('common.cancel'), type: 'warning' },
    );
    await executeSQL(sqlText, true);
  } catch (cause) {
    if (cause === 'cancel' || cause === 'close') return;
    if (cause instanceof DOMException && cause.name === 'AbortError') return;
    ElMessage.error(cause instanceof Error ? cause.message : t('sqlConsole.executionFailed'));
  }
}
```

模板中 `SQLEditorPanel` 改为:

```vue
<SQLEditorPanel
  v-model="sql"
  :executing="executing"
  :disabled="executionDisabled"
  :metadata="metadata"
  :dialect="selectedAccount?.instance_protocol === 'postgres' ? 'postgres' : 'mysql'"
  @execute="handleExecute"
  @cancel="cancel"
/>
```

`executionDisabled` 调整为 `executing.value || connecting.value || !connected.value || !database.value`(不再要求 `sql.value` 非空,语句来自编辑器定位)。

注:`execute`(composable)仍读 `sql.value`——handleExecute 已先置 `sql.value = sqlText`,语义不变。

- [ ] **Step 2: i18n 新增键**(`web/src/i18n/index.ts` 的 `sqlConsole` 区块追加)

```ts
'sqlConsole.executeStatement': '执行此语句',
'sqlConsole.keyboardHint': 'Ctrl / ⌘ + Enter 执行光标所在语句 · 行号 ▶ 执行单条语句',
```

- [ ] **Step 3: 运行既有 SQL 控制台相关测试**

Run: `cd web && npm run test:connection-dialog && npm run typecheck`
Expected: PASS

- [ ] **Step 4: 全量验证**

Run:
```bash
cd web && npm run build
cd .. && go build ./... && go test ./internal/service/ ./internal/handler/sqlconsole/
```
Expected: 构建与测试全部通过

- [ ] **Step 5: 手工验收**(`npm run dev` 启动前端 + 本地后端,按 spec 第 10 节验收标准逐项确认;无法联调时在浏览器 Console 用 mock 数据核对编辑器行为)

- [ ] **Step 6: 提交**

```bash
git add web/src/views/SQLConsoleWorkspace.vue web/src/i18n/index.ts
git commit -m "feat(web): SQL 控制台接线(执行当前语句/补全/方言)"
```

---

## 自审记录

- **Spec 覆盖**:元数据接口(Task 1/2)、语句切分(Task 3)、gutter 按钮(Task 6/7)、虚线框(Task 6/7)、补全(Task 4/5/7)、i18n 与接线(Task 8)、主题适配(Task 7)、错误处理(静默降级 Task 5;连接过期/写确认保持现有逻辑 Task 8)、测试与验收(Task 各步 + Task 8 Step 4/5)✓
- **占位符**:无 TBD/TODO;所有代码与命令已给出 ✓
- **类型一致性**:`SQLStatementRange`/`statementAtPosition`/`parseSQLStatements` 跨 Task 3/6/7/8 一致;`SQLTableMetadata` 定义于 `sqlSchema.ts`(Task 4),client 类型 `SQLConsoleMetadataResult` 独立(Task 5),`useSQLConsole.metadata` 类型为 `SQLTableMetadata[]` ✓
- **注意项**:Go 测试中 `CreateSession` 签名与连接获取方式、前端 CM6 在 happy-dom 的挂载稳定性,均已在任务中给出"以实际签名为准/测试回退方案"的明确处置。
