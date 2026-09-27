package recording

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplayStorageUsageBytes(t *testing.T) {
	root := t.TempDir()
	writeReplayFile(t, root, "ssh/a/file", "123")
	writeReplayFile(t, root, "db/b/file", "12345")
	if err := os.WriteFile(filepath.Join(root, "ignored.sock"), []byte("not special"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage, err := NewReplayStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := storage.UsageBytes(context.Background())
	if err != nil || got != 19 {
		t.Fatalf("usage = %d, err = %v; want 19", got, err)
	}
}

func TestReplayStorageDeleteSession(t *testing.T) {
	root := t.TempDir()
	writeReplayFile(t, root, "ssh/session/file", "1234")
	storage, _ := NewReplayStorage(root)
	got, err := storage.DeleteSession(context.Background(), filepath.Join(root, "ssh", "session"))
	if err != nil || got != 4 {
		t.Fatalf("deleted = %d, err = %v; want 4", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "ssh", "session")); !os.IsNotExist(err) {
		t.Fatalf("session still exists, err = %v", err)
	}

	got, err = storage.DeleteSession(context.Background(), filepath.Join(root, "db", "missing"))
	if err != nil || got != 0 {
		t.Fatalf("missing deleted = %d, err = %v; want 0", got, err)
	}
}

func TestReplayStorageRejectsInvalidPaths(t *testing.T) {
	root := t.TempDir()
	storage, _ := NewReplayStorage(root)
	paths := []string{root, filepath.Join(root, "ssh"), filepath.Join(root, "ssh", "a", "nested"), filepath.Join(root, "other", "a"), filepath.Join(root, "..", filepath.Base(root), "other")}
	for _, path := range paths {
		if _, err := storage.DeleteSession(context.Background(), path); err == nil {
			t.Errorf("DeleteSession(%q) succeeded", path)
		}
	}
}

func TestReplayStorageRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "ssh")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	storage, _ := NewReplayStorage(root)
	if _, err := storage.DeleteSession(context.Background(), filepath.Join(root, "ssh", "escape")); err == nil {
		t.Fatal("symlink escape was accepted")
	}
}

func TestReplayStorageContextCancellation(t *testing.T) {
	root := t.TempDir()
	writeReplayFile(t, root, "ssh/a/file", "123")
	storage, _ := NewReplayStorage(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := storage.UsageBytes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("usage error = %v, want context.Canceled", err)
	}
}

func TestReplayStorageMissingRootHasZeroUsage(t *testing.T) {
	storage, err := NewReplayStorage(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	usage, err := storage.UsageBytes(context.Background())
	if err != nil || usage != 0 {
		t.Fatalf("missing root usage = %d, error = %v", usage, err)
	}
}

func writeReplayFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
