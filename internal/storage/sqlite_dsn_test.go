package storage

import (
	"strings"
	"testing"
)

func TestSQLiteDSNForcesForeignKeysAndMemoryTempStore(t *testing.T) {
	dsn := sqliteDSNWithForeignKeys("data/bastion.db")
	for _, want := range []string{"_pragma=foreign_keys(1)", "_pragma=temp_store(memory)"} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn %q missing %q", dsn, want)
		}
	}
}

func TestSQLiteDSNReplacesConflictingPragmas(t *testing.T) {
	dsn := sqliteDSNWithForeignKeys("data/bastion.db?_pragma=temp_store(file)&cache=shared&_pragma=foreign_keys(0)")
	if strings.Contains(dsn, "temp_store(file)") || strings.Contains(dsn, "foreign_keys(0)") {
		t.Fatalf("conflicting pragmas kept: %q", dsn)
	}
	if !strings.Contains(dsn, "cache=shared") {
		t.Fatalf("unrelated parameter dropped: %q", dsn)
	}
	if strings.Count(dsn, "_pragma=temp_store(memory)") != 1 {
		t.Fatalf("temp_store pragma must appear exactly once: %q", dsn)
	}
}

func TestSQLiteDSNKeepsMemoryMode(t *testing.T) {
	dsn := sqliteDSNWithForeignKeys(":memory:")
	if !strings.Contains(dsn, "mode=memory") {
		t.Fatalf("memory dsn lost memory mode: %q", dsn)
	}
	if !strings.Contains(dsn, "_pragma=temp_store(memory)") {
		t.Fatalf("memory dsn missing temp_store pragma: %q", dsn)
	}
}
