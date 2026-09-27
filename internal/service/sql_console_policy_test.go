package service

import (
	"errors"
	"testing"
)

func TestInspectSQLStatementClassifiesSupportedStatements(t *testing.T) {
	tests := []struct {
		sql      string
		kind     string
		readOnly bool
	}{
		{sql: "SELECT id FROM users;", kind: "select", readOnly: true},
		{sql: "/* inspect */ SHOW TABLES", kind: "show", readOnly: true},
		{sql: "EXPLAIN SELECT * FROM users", kind: "explain", readOnly: true},
		{sql: "WITH rows AS (SELECT 1) SELECT * FROM rows", kind: "with", readOnly: true},
		{sql: "UPDATE users SET status = 'active' WHERE id = 1", kind: "update"},
		{sql: "CREATE TABLE sample (id bigint)", kind: "create"},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			policy, err := inspectSQLStatement(test.sql)
			if err != nil {
				t.Fatalf("inspectSQLStatement() error = %v", err)
			}
			if policy.QueryKind != test.kind || policy.ReadOnly != test.readOnly {
				t.Fatalf("policy = %#v", policy)
			}
		})
	}
}

func TestInspectSQLStatementRejectsUnsafeOrMultipleStatements(t *testing.T) {
	tests := []struct {
		sql  string
		want error
	}{
		{sql: "SELECT 1; DROP TABLE users", want: ErrSQLConsoleMultipleStatements},
		{sql: "SELECT * INTO copied_users FROM users", want: ErrSQLConsoleUnsupported},
		{sql: "SELECT * FROM users FOR UPDATE", want: ErrSQLConsoleUnsupported},
		{sql: "BEGIN", want: ErrSQLConsoleUnsupported},
		{sql: "SELECT 'unterminated", want: ErrSQLConsoleInvalid},
	}
	for _, test := range tests {
		t.Run(test.sql, func(t *testing.T) {
			_, err := inspectSQLStatement(test.sql)
			if !errors.Is(err, test.want) {
				t.Fatalf("inspectSQLStatement() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestInspectSQLStatementAllowsSemicolonsInsideLiterals(t *testing.T) {
	policy, err := inspectSQLStatement("SELECT 'alpha;beta' AS value; -- trailing comment")
	if err != nil {
		t.Fatalf("inspectSQLStatement() error = %v", err)
	}
	if policy.SQL != "SELECT 'alpha;beta' AS value" || !policy.ReadOnly {
		t.Fatalf("policy = %#v", policy)
	}
}
