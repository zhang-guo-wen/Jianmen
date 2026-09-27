package service

import (
	"reflect"
	"testing"
)

func TestParseMySQLGrantRowsClassifiesDatabasePrivileges(t *testing.T) {
	rows := [][]string{
		{"GRANT SELECT ON `customer`.* TO 'jm_abc'@'%'"},
		{"GRANT SELECT, INSERT, UPDATE, DELETE ON `orders`.* TO 'jm_abc'@'%'"},
		{"GRANT ALL PRIVILEGES ON `analytics`.* TO 'jm_abc'@'%'"},
		{"GRANT USAGE ON *.* TO 'jm_abc'@'%'"},
		{"GRANT SELECT ON *.* TO 'jm_abc'@'%' WITH GRANT OPTION"},
		{"GRANT SELECT ON `legacy`.`orders` TO 'jm_abc'@'%'"},
	}
	grants, warnings := parseMySQLGrantRows(rows)
	expected := []DatabaseAccountGrant{
		{Database: "customer", Privilege: "read", Privileges: []string{"SELECT"}},
		{Database: "orders", Privilege: "readwrite", Privileges: []string{"SELECT", "INSERT", "UPDATE", "DELETE"}},
		{Database: "analytics", Privilege: "readwrite", Privileges: []string{"ALL PRIVILEGES"}},
	}
	if !reflect.DeepEqual(grants, expected) {
		t.Fatalf("grants = %#v, want %#v", grants, expected)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v, want 2 entries", warnings)
	}
}

func TestParseMySQLGrantRowsMergesDuplicateDatabases(t *testing.T) {
	rows := [][]string{
		{"GRANT SELECT ON `customer`.* TO 'u'@'%'"},
		{"GRANT INSERT ON `customer`.* TO 'u'@'%'"},
	}
	grants, warnings := parseMySQLGrantRows(rows)
	if len(grants) != 1 {
		t.Fatalf("grants = %#v", grants)
	}
	if grants[0].Database != "customer" || grants[0].Privilege != "readwrite" {
		t.Fatalf("merged grant = %#v", grants[0])
	}
	if len(grants[0].Privileges) != 2 {
		t.Fatalf("merged privileges = %#v", grants[0].Privileges)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestParseMySQLGrantRowsSanitizesIdentifiedByPassword(t *testing.T) {
	rows := [][]string{
		{"GRANT SELECT ON `customer`.* TO 'u'@'%' IDENTIFIED BY PASSWORD '*HASHHASHHASH'"},
	}
	grants, warnings := parseMySQLGrantRows(rows)
	if len(grants) != 1 || grants[0].Database != "customer" || grants[0].Privilege != "read" {
		t.Fatalf("grants = %#v", grants)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestParseMySQLGrantRowsHandlesUnparseableStatements(t *testing.T) {
	rows := [][]string{
		{"GRANT SELECT ON `customer`.* TO 'u'@'%'"},
		{"not a grant statement at all"},
	}
	grants, warnings := parseMySQLGrantRows(rows)
	if len(grants) != 1 {
		t.Fatalf("grants = %#v", grants)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(warnings[0]) > 90 {
		t.Fatalf("warning may leak long statement: %q", warnings[0])
	}
}

func TestParseMySQLGrantRowsIgnoresEmptyRows(t *testing.T) {
	rows := [][]string{nil, {""}}
	grants, warnings := parseMySQLGrantRows(rows)
	if len(grants) != 0 || len(warnings) != 0 {
		t.Fatalf("grants = %#v warnings = %#v", grants, warnings)
	}
}

func TestClassifyMySQLGrantPrivileges(t *testing.T) {
	cases := []struct {
		privileges []string
		want       string
	}{
		{[]string{"SELECT"}, "read"},
		{[]string{"SELECT", "INSERT"}, "readwrite"},
		{[]string{"SELECT", "INSERT", "UPDATE", "DELETE"}, "readwrite"},
		{[]string{"SELECT", "GRANT OPTION"}, "readwrite"},
		{[]string{"ALL", "PRIVILEGES"}, "readwrite"},
		{[]string{"CREATE"}, "readwrite"},
		{[]string{"EXECUTE"}, "readwrite"},
		{[]string{""}, ""},
		{[]string{}, ""},
	}
	for _, test := range cases {
		if got := classifyMySQLGrantPrivileges(test.privileges); got != test.want {
			t.Errorf("classifyMySQLGrantPrivileges(%v) = %q, want %q", test.privileges, got, test.want)
		}
	}
}
