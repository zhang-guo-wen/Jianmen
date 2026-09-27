package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSQLiteAtomicallyReplacesExistingBackup(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "jianmen.db")
	bakPath := srcPath + ".bak"
	current := []byte("current database contents")
	writeAtomicBackupTestFile(t, srcPath, current)
	writeAtomicBackupTestFile(t, bakPath, []byte("old backup"))

	if err := BackupSQLite(srcPath); err != nil {
		t.Fatalf("BackupSQLite() error = %v", err)
	}

	assertAtomicBackupTestContents(t, bakPath, current)
	assertNoAtomicBackupTempFiles(t, bakPath)
}

func TestBackupSQLiteKeepsExistingBackupWhenCopyFails(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "jianmen.db")
	bakPath := srcPath + ".bak"
	oldBackup := []byte("old backup must survive")
	writeAtomicBackupTestFile(t, srcPath, []byte("current database contents"))
	writeAtomicBackupTestFile(t, bakPath, oldBackup)
	copyErr := errors.New("injected copy failure")

	err := backupSQLiteFile(srcPath, bakPath, sqliteBackupFileOps{
		copy: func(dst io.Writer, _ io.Reader) (int64, error) {
			n, err := dst.Write([]byte("partial backup"))
			if err != nil {
				return int64(n), err
			}
			return int64(n), copyErr
		},
		replace: os.Rename,
	})
	if !errors.Is(err, copyErr) {
		t.Fatalf("backupSQLiteFile() error = %v, want %v", err, copyErr)
	}

	assertAtomicBackupTestContents(t, bakPath, oldBackup)
	assertNoAtomicBackupTempFiles(t, bakPath)
}

func TestBackupSQLiteKeepsExistingBackupWhenReplaceFails(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "jianmen.db")
	bakPath := srcPath + ".bak"
	current := []byte("current database contents")
	oldBackup := []byte("old backup must survive")
	writeAtomicBackupTestFile(t, srcPath, current)
	writeAtomicBackupTestFile(t, bakPath, oldBackup)
	replaceErr := errors.New("injected replace failure")

	err := backupSQLiteFile(srcPath, bakPath, sqliteBackupFileOps{
		copy: io.Copy,
		replace: func(tmpPath, targetPath string) error {
			if filepath.Dir(tmpPath) != filepath.Dir(targetPath) {
				t.Errorf("temporary file directory = %q, want %q", filepath.Dir(tmpPath), filepath.Dir(targetPath))
			}
			assertAtomicBackupTestContents(t, tmpPath, current)
			assertAtomicBackupTestContents(t, targetPath, oldBackup)
			return replaceErr
		},
	})
	if !errors.Is(err, replaceErr) {
		t.Fatalf("backupSQLiteFile() error = %v, want %v", err, replaceErr)
	}

	assertAtomicBackupTestContents(t, bakPath, oldBackup)
	assertNoAtomicBackupTempFiles(t, bakPath)
}

func writeAtomicBackupTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func assertAtomicBackupTestContents(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%q contents = %q, want %q", path, got, want)
	}
}

func assertNoAtomicBackupTempFiles(t *testing.T, bakPath string) {
	t.Helper()
	pattern := filepath.Join(filepath.Dir(bakPath), "."+filepath.Base(bakPath)+".tmp-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob temporary backup files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary backup files were not cleaned up: %v", matches)
	}
}
