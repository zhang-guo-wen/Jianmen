package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemStoreLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := newTestFilesystemStore(t, root, "/tenant/rdp/")
	body := []byte("recording payload")
	info, err := store.Put(
		context.Background(),
		"sessions/2026/one.cast",
		bytes.NewReader(body),
		int64(len(body)),
		"application/x-asciicast",
	)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	wantETag := fmt.Sprintf("%x", sha256.Sum256(body))
	if info.Key != "sessions/2026/one.cast" ||
		info.Size != int64(len(body)) ||
		info.ContentType != "application/x-asciicast" ||
		info.ETag != wantETag ||
		info.LastModified.IsZero() {
		t.Fatalf("Put() info = %+v", info)
	}

	physicalPath := filepath.Join(root, "tenant", "rdp", "sessions", "2026", "one.cast")
	raw, err := os.ReadFile(physicalPath)
	if err != nil {
		t.Fatalf("read physical object: %v", err)
	}
	if !bytes.Equal(raw, body) {
		t.Fatalf("physical object = %q, want %q", raw, body)
	}

	reader, err := store.Open(context.Background(), "sessions/2026/one.cast")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()
	first := make([]byte, 9)
	if _, err := io.ReadFull(reader, first); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("Open() body = %q, want %q", got, body)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	stat, err := store.Stat(context.Background(), "sessions/2026/one.cast")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Key != info.Key ||
		stat.Size != info.Size ||
		stat.ContentType != info.ContentType ||
		stat.ETag != info.ETag ||
		stat.LastModified.IsZero() {
		t.Fatalf("Stat() info = %+v, want values from %+v", stat, info)
	}

	if err := store.Delete(context.Background(), "sessions/2026/one.cast"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(context.Background(), "sessions/2026/one.cast"); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := store.Open(context.Background(), "sessions/2026/one.cast"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() after delete error = %v, want ErrNotFound", err)
	}
	if _, err := store.Stat(context.Background(), "sessions/2026/one.cast"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat() after delete error = %v, want ErrNotFound", err)
	}
}

func TestFilesystemStorePutIsAtomicOnReadFailure(t *testing.T) {
	t.Parallel()

	store := newTestFilesystemStore(t, t.TempDir(), "")
	key := "sessions/one.cast"
	original := []byte("original recording")
	if _, err := store.Put(context.Background(), key, bytes.NewReader(original), int64(len(original)), "video/mp4"); err != nil {
		t.Fatalf("initial Put() error = %v", err)
	}

	injected := errors.New("injected read failure")
	if _, err := store.Put(
		context.Background(),
		key,
		&failAfterDataReader{data: []byte("replacement"), err: injected},
		-1,
		"video/mp4",
	); !errors.Is(err, injected) {
		t.Fatalf("replacement Put() error = %v, want injected failure", err)
	}
	assertFilesystemObjectBody(t, store, key, original)
}

func TestFilesystemStorePutIsAtomicOnSizeMismatch(t *testing.T) {
	t.Parallel()

	store := newTestFilesystemStore(t, t.TempDir(), "")
	key := "sessions/one.cast"
	original := []byte("original recording")
	if _, err := store.Put(context.Background(), key, bytes.NewReader(original), int64(len(original)), "video/mp4"); err != nil {
		t.Fatalf("initial Put() error = %v", err)
	}
	if _, err := store.Put(context.Background(), key, bytes.NewReader([]byte("short")), 99, "video/mp4"); err == nil {
		t.Fatal("replacement Put() error = nil, want size mismatch")
	}
	assertFilesystemObjectBody(t, store, key, original)
}

func TestFilesystemStoreSupportsUnknownSizeAndDefaultContentType(t *testing.T) {
	t.Parallel()

	store := newTestFilesystemStore(t, t.TempDir(), "")
	info, err := store.Put(
		context.Background(),
		"artifacts/empty.bin",
		bytes.NewReader(nil),
		-1,
		"",
	)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if info.Size != 0 {
		t.Fatalf("Put() size = %d, want 0", info.Size)
	}
	if info.ContentType != "application/octet-stream" {
		t.Fatalf("Put() content type = %q", info.ContentType)
	}
	assertFilesystemObjectBody(t, store, "artifacts/empty.bin", nil)
}

func TestFilesystemStoreRejectsInvalidPutArguments(t *testing.T) {
	t.Parallel()

	store := newTestFilesystemStore(t, t.TempDir(), "")
	if _, err := store.Put(context.Background(), "session.cast", nil, 0, "text/plain"); err == nil {
		t.Fatal("Put() with nil source error = nil")
	}
	if _, err := store.Put(
		context.Background(),
		"session.cast",
		bytes.NewReader(nil),
		-2,
		"text/plain",
	); err == nil {
		t.Fatal("Put() with invalid size error = nil")
	}
}

func TestFilesystemStoreRejectsTraversalAndReservedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := newTestFilesystemStore(t, root, "")
	keys := []string{
		"../escape.cast",
		"a/../../escape.cast",
		`a\..\escape.cast`,
		filesystemMetadataDir,
		filesystemMetadataDir + "/collision.json",
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			_, err := store.Put(context.Background(), key, bytes.NewReader([]byte("x")), 1, "text/plain")
			if !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Put(%q) error = %v, want ErrInvalidKey", key, err)
			}
		})
	}

	escaped := filepath.Join(filepath.Dir(root), "escape.cast")
	if _, err := os.Stat(escaped); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escaped object exists or stat failed unexpectedly: %v", err)
	}
	if _, err := New(context.Background(), Config{
		Provider: ProviderFilesystem,
		LocalDir: root,
		Prefix:   filesystemMetadataDir + "/tenant",
	}); err == nil {
		t.Fatal("New() with reserved prefix error = nil")
	}
}

func TestFilesystemStoreRejectsSymlinkTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "secret.cast")
	if err := os.WriteFile(secretPath, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	store := newTestFilesystemStore(t, root, "")
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	if _, err := store.Open(context.Background(), "linked/secret.cast"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Open() through symlink error = %v, want ErrInvalidKey", err)
	}
	if _, err := store.Put(
		context.Background(),
		"linked/new.cast",
		bytes.NewReader([]byte("new")),
		3,
		"video/mp4",
	); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Put() through symlink error = %v, want ErrInvalidKey", err)
	}
	if err := store.Delete(context.Background(), "linked/secret.cast"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Delete() through symlink error = %v, want ErrInvalidKey", err)
	}
	targetLink := filepath.Join(root, "target-link.cast")
	if err := os.Symlink(secretPath, targetLink); err != nil {
		t.Fatalf("create target symlink: %v", err)
	}
	if err := store.Delete(context.Background(), "target-link.cast"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Delete() target symlink error = %v, want ErrInvalidKey", err)
	}
	raw, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
	if string(raw) != "outside" {
		t.Fatalf("outside file = %q, want unchanged", raw)
	}
}

func TestFilesystemStoreHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	store := newTestFilesystemStore(t, t.TempDir(), "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, "session.cast", bytes.NewReader([]byte("x")), 1, "text/plain"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put() error = %v, want context.Canceled", err)
	}
	if _, err := store.Open(ctx, "session.cast"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled", err)
	}
	if _, err := store.Stat(ctx, "session.cast"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stat() error = %v, want context.Canceled", err)
	}
	if err := store.Delete(ctx, "session.cast"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete() error = %v, want context.Canceled", err)
	}
}

type failAfterDataReader struct {
	data []byte
	err  error
	read bool
}

func (r *failAfterDataReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, r.data), nil
	}
	return 0, r.err
}

func newTestFilesystemStore(t *testing.T, root, prefix string) Store {
	t.Helper()
	store, err := New(context.Background(), Config{
		Provider: ProviderFilesystem,
		LocalDir: root,
		Prefix:   prefix,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

func assertFilesystemObjectBody(t *testing.T, store Store, key string, want []byte) {
	t.Helper()
	reader, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("object body = %q, want %q", got, want)
	}
}
