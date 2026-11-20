package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// DSN 带驱动参数时，迁移前备份仍应备份真实数据库文件，而不是把参数当成文件名。
func TestBackupSQLiteUsesFilePathFromDSNWithParameters(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "jianmen.db")
	contents := []byte("database contents for backup")
	writeAtomicBackupTestFile(t, srcPath, contents)
	dsn := srcPath + "?_pragma=temp_store(memory)&cache=shared"

	if got := BackupPath(dsn); got != srcPath+".bak" {
		t.Fatalf("BackupPath(%q) = %q, want %q", dsn, got, srcPath+".bak")
	}
	if err := BackupSQLite(dsn); err != nil {
		t.Fatalf("BackupSQLite(%q) error = %v", dsn, err)
	}
	assertAtomicBackupTestContents(t, srcPath+".bak", contents)
	assertNoAtomicBackupTempFiles(t, srcPath+".bak")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "jianmen.db" && entry.Name() != "jianmen.db.bak" {
			t.Fatalf("unexpected file in database directory: %q", entry.Name())
		}
	}
}

// 没有参数时行为保持不变。
func TestBackupSQLiteWithoutParametersKeepsPlainPath(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "jianmen.db")
	contents := []byte("plain path backup")
	writeAtomicBackupTestFile(t, srcPath, contents)

	if got := BackupPath(srcPath); got != srcPath+".bak" {
		t.Fatalf("BackupPath(%q) = %q, want %q", srcPath, got, srcPath+".bak")
	}
	if err := BackupSQLite(srcPath); err != nil {
		t.Fatalf("BackupSQLite(%q) error = %v", srcPath, err)
	}
	assertAtomicBackupTestContents(t, srcPath+".bak", contents)
}
