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
